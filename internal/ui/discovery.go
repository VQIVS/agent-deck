package ui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/qivs/agent-deck/internal/config"
	api "github.com/qivs/agent-deck/internal/opencode"
	"github.com/qivs/agent-deck/internal/state"
)

type inventoryMsg struct {
	Inventory api.Inventory
	Err       error
}

func (m *Model) discover() tea.Cmd {
	if m.discovering {
		return nil
	}
	m.discovering = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.context(), 90*time.Second)
		defer cancel()
		v, e := m.client.Discover(ctx)
		return inventoryMsg{v, e}
	}
}

func (m *Model) startStream(dir string) {
	go m.client.Stream(
		m.context(),
		dir,
		func(e api.Event) { m.emit(streamMsg{Dir: dir, Event: e}) },
		func(e error) { m.emit(streamMsg{Dir: dir, Err: e, Connection: true}) },
	)
}

func agent(s api.Session) config.Agent {
	name := s.Title
	if name == "" {
		name = s.ID
	}
	return config.Agent{Name: name, Session: s.ID, Dir: s.Directory, Parent: s.ParentID}
}

func activeAgents(v *api.Inventory) []config.Agent {
	byID := map[string]api.Session{}
	selected := map[string]bool{}
	for _, s := range v.Sessions {
		byID[s.ID] = s
		if v.Active[s.Directory+"\x00"+s.ID] {
			selected[s.ID] = true
		}
	}
	for id := range selected {
		seen := map[string]bool{}
		for p := byID[id].ParentID; p != "" && !seen[p]; p = byID[p].ParentID {
			seen[p] = true
			selected[p] = true
		}
	}
	var out []config.Agent
	for _, s := range v.Sessions {
		if selected[s.ID] {
			out = append(out, agent(s))
		}
	}
	return grouped(out)
}

// Stable preorder keeps children next to parents, tolerates missing parents/cycles.
func grouped(in []config.Agent) []config.Agent {
	var out []config.Agent
	seen := map[string]bool{}
	present := map[string]bool{}
	for _, a := range in {
		present[a.Session] = true
	}
	var walk func(config.Agent)
	walk = func(a config.Agent) {
		if seen[a.Session] {
			return
		}
		seen[a.Session] = true
		out = append(out, a)
		for _, b := range in {
			if b.Parent == a.Session {
				walk(b)
			}
		}
	}
	for _, a := range in {
		if a.Parent == "" || !present[a.Parent] {
			walk(a)
		}
	}
	for _, a := range in {
		walk(a)
	}
	return out
}

func (m *Model) add(a *config.Agent) []tea.Cmd {
	if m.hidden[a.Session] {
		return nil
	}
	for i := range m.panes {
		p := &m.panes[i]
		if p.Agent.Session == a.Session && p.Agent.Dir == a.Dir {
			return nil
		}
	}
	p := pane{Agent: *a, State: state.New(), View: viewport.New(40, 10)}
	if m.opts.NoGit {
		p.Git = "git disabled"
	}
	m.panes = append(m.panes, p)
	i := len(m.panes) - 1
	cmds := []tea.Cmd{m.snapshot(i)}
	if !m.opts.NoGit {
		cmds = append(cmds, m.git(i))
	}
	return cmds
}

func (m *Model) page(h int) (visible []int, rectangles []Rect, pageIndex, pageCount int) {
	var agents []config.Agent
	for i := range m.panes {
		p := &m.panes[i]
		if !m.hidden[p.Agent.Session] {
			agents = append(agents, p.Agent)
		}
	}
	order := grouped(agents)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Role != "" && order[j].Role == "" })
	var indices []int
	for _, a := range order {
		for i := range m.panes {
			p := &m.panes[i]
			if a.Session == p.Agent.Session && a.Dir == p.Agent.Dir {
				indices = append(indices, i)
				break
			}
		}
	}
	if len(indices) == 0 {
		return nil, nil, 0, 0
	}
	lead := len(order) > 0 && order[0].Role != ""
	capacity := Capacity(m.w, h, lead)
	pos := 0
	found := false
	for i, n := range indices {
		if n == m.focus {
			pos = i
			found = true
		}
	}
	if !found {
		m.focus = indices[0]
	}
	page := pos / capacity
	pages := (len(indices) + capacity - 1) / capacity
	start := page * capacity
	end := min(len(indices), start+capacity)
	indices = indices[start:end]
	return indices, Grid(m.w, h, len(indices), lead && page == 0), page, pages
}

func fuzzy(query, text string) bool {
	q := []rune(strings.ToLower(query))
	i := 0
	for _, r := range strings.ToLower(text) {
		if i < len(q) && r == q[i] {
			i++
		}
	}
	return i == len(q)
}

func (m *Model) matches() []api.Session {
	var out []api.Session
	for _, s := range m.inventory.Sessions {
		if fuzzy(m.input.Value(), s.Title+" "+s.ID+" "+s.Directory) {
			out = append(out, s)
		}
	}
	return out
}

func (m *Model) updatePicker(v tea.KeyMsg) tea.Cmd {
	list := m.matches()
	switch v.String() {
	case "esc", "ctrl+c":
		m.picker = false
		return nil
	case "up":
		m.pickIndex = max(0, m.pickIndex-1)
		return nil
	case "down":
		m.pickIndex = min(max(0, len(list)-1), m.pickIndex+1)
		return nil
	case " ", "enter":
		if len(list) == 0 {
			return nil
		}
		s := list[min(m.pickIndex, len(list)-1)]
		for i := range m.panes {
			p := &m.panes[i]
			if p.Agent.Session == s.ID { // Keep indices stable while async commands are running.
				m.hidden[s.ID] = !m.hidden[s.ID]
				m.note = "Pane visibility updated"
				return nil
			}
		}
		delete(m.hidden, s.ID)
		a := agent(s)
		return tea.Batch(m.add(&a)...)
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(v)
		m.pickIndex = 0
		return cmd
	}
}

func (m *Model) cycle(delta int) {
	var agents []config.Agent
	for i := range m.panes {
		p := &m.panes[i]
		if !m.hidden[p.Agent.Session] {
			agents = append(agents, p.Agent)
		}
	}
	ordered := grouped(agents)
	if len(ordered) == 0 {
		return
	}
	pos := 0
	for i, a := range ordered {
		if a.Session == m.panes[m.focus].Agent.Session {
			pos = i
		}
	}
	target := ordered[(pos+delta+len(ordered))%len(ordered)].Session
	for i := range m.panes {
		p := &m.panes[i]
		if p.Agent.Session == target {
			m.focus = i
			return
		}
	}
}
