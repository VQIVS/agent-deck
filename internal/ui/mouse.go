package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type zone struct {
	Rect
	Action string
	Pane   int
}
type button struct {
	Label, Action string
	Disabled      bool
}

func hit(zones []zone, x, y int) (zone, bool) {
	// Buttons registered after their encompassing pane take precedence.
	for i := len(zones) - 1; i >= 0; i-- {
		z := zones[i]
		if x >= z.X && x < z.X+z.W && y >= z.Y && y < z.Y+z.H {
			return z, true
		}
	}
	return zone{}, false
}

func paneButtons(permission, readOnly, zoom bool) []button {
	z := button{"⤢", "enter", false}
	if zoom {
		z = button{"×", "esc", false}
	}
	out := []button{
		z,
		{"Message", "m", readOnly},
		{"Nudge", "n", readOnly},
		{"Stop", "s", readOnly},
		{"Attach", "a", readOnly},
	}
	out = append(out, button{"Compact", "C", readOnly})
	if permission {
		out = append(
			out,
			button{"Approve once", "y", readOnly},
			button{"Always", "Y", readOnly},
			button{"Reject", "x", readOnly},
		)
	}
	return out
}

func (m *Model) headerButtons() []button {
	notify := "Notify off"
	if m.opts.Notify {
		notify = "Notify on"
	}
	out := []button{
		{"Sessions", "p", false},
		{"Refresh", "r", false},
		{notify, "N", false},
		{"Help", "?", false},
		{"<", "page-prev", false},
		{">", "page-next", false},
	}
	if m.teams != nil {
		if m.teams.Manager != nil {
			out = append([]button{{"Team", "team-members", false}, {"+ Worker", "team-add", m.opts.ReadOnly}}, out...)
		} else {
			out = append(out, button{"Teams", "team-picker", false})
		}
	}
	return out
}

func (m *Model) buttonRow(buttons []button, x, y, width, pane int) string {
	var out strings.Builder
	used := 0
	for _, b := range buttons {
		text := "[" + b.Label + "]"
		w := lipgloss.Width(text)
		if used+w > width {
			break
		}
		style := lipgloss.NewStyle().Foreground(m.theme.Accent)
		r := Rect{x + used, y, w, 1}
		if b.Disabled {
			style = style.Foreground(m.theme.Muted)
		} else {
			m.zones = append(m.zones, zone{r, b.Action, pane})
			if m.hoverAction == b.Action {
				style = style.Background(m.theme.Focus).Bold(true)
			}
		}
		out.WriteString(style.Render(text))
		out.WriteByte(' ')
		used += w + 1
	}
	return strings.TrimSuffix(out.String(), " ")
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	m.mouseX, m.mouseY = msg.X, msg.Y
	z, ok := hit(m.zones, msg.X, msg.Y)
	m.hoverAction = ""
	if ok {
		m.hoverAction = z.Action
	}
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		return m.mouseWheel(msg, z, ok)
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	if m.teams != nil && (m.teams.Screen != "" || m.teams.Confirm != "") {
		return m.teamClick(z, ok)
	}
	return m.dashboardClick(msg, z, ok)
}

func (m *Model) mouseWheel(msg tea.MouseMsg, z zone, ok bool) tea.Cmd {
	if m.teams != nil && m.teams.Screen != "" {
		key := tea.KeyDown
		if msg.Button == tea.MouseButtonWheelUp {
			key = tea.KeyUp
		}
		return m.teamKey(tea.KeyMsg{Type: key})
	}
	if m.picker {
		if msg.Button == tea.MouseButtonWheelUp {
			m.pickIndex = max(0, m.pickIndex-3)
		} else {
			m.pickIndex = min(max(0, len(m.matches())-1), m.pickIndex+3)
		}
		return nil
	}
	if ok && z.Pane >= 0 {
		v := &m.panes[z.Pane].View
		if msg.Button == tea.MouseButtonWheelUp {
			v.ScrollUp(3)
		} else {
			v.ScrollDown(3)
		}
	}
	return nil
}

func (m *Model) teamClick(z zone, ok bool) tea.Cmd {
	if !ok {
		if m.teams.Confirm != "" {
			return m.teamAction("team-no")
		}
		return nil
	}
	if z.Action == "input" {
		m.input.Focus()
		return nil
	}
	return m.teamAction(z.Action)
}

func (m *Model) dashboardClick(msg tea.MouseMsg, z zone, ok bool) tea.Cmd {
	modal := m.modalOpen()
	if !ok {
		if modal {
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			return cmd
		}
		return nil
	}
	if z.Action == "input" {
		m.input.Focus()
		return nil
	}
	if strings.HasPrefix(z.Action, "team-") {
		return m.teamAction(z.Action)
	}
	if strings.HasPrefix(z.Action, "pick:") {
		var i int
		_, _ = fmt.Sscanf(z.Action, "pick:%d", &i)
		m.pickIndex = i
		return m.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if z.Pane >= 0 {
		m.focus = z.Pane
	}
	if strings.HasPrefix(z.Action, "card:") {
		id := strings.TrimPrefix(z.Action, "card:")
		m.panes[z.Pane].Selected = id
		if msg.Ctrl {
			m.toggleRaw()
		} else {
			m.toggleCard(z.Pane, id)
		}
		return nil
	}
	if z.Action == "focus" {
		return nil
	}
	return m.clickAction(z.Action)
}

func (m *Model) clickAction(action string) tea.Cmd {
	if action == "page-prev" || action == "page-next" {
		return m.movePage(action == "page-next")
	}
	var key tea.KeyMsg
	switch action {
	case "enter":
		m.zoom = true
		return nil
	case "esc":
		key.Type = tea.KeyEsc
	case "send":
		key.Type = tea.KeyCtrlS
	default:
		key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(action)}
	}
	_, cmd := m.Update(key)
	return cmd
}

func (m *Model) movePage(next bool) tea.Cmd {
	if len(m.panes) == 0 {
		return nil
	}
	indices, _, _, _ := m.page(max(1, m.h-4))
	if len(indices) == 0 {
		return nil
	}
	if next {
		m.focus = indices[len(indices)-1]
		m.cycle(1)
	} else {
		m.focus = indices[0]
		m.cycle(-1)
	}
	return nil
}

func (m *Model) dialogView() string {
	// Full-screen modal: background clicks dismiss; only explicit Yes confirms.
	if m.confirm || m.permissionConfirm || m.compactConfirm {
		text := "Stop " + m.panes[m.focus].Agent.Name + "?"
		if m.permissionConfirm {
			text = "Always approve " + m.permissionTarget.Description + "?"
		}
		if m.compactConfirm {
			text = "Compact " + m.panes[m.focus].Agent.Name + "? Requires idle; abort or resolve waits first."
		}
		return m.theme.title(
			fit(text, m.w),
		) + "\n\n" + m.buttonRow(
			[]button{{"Yes", "y", m.opts.ReadOnly}, {"No", "esc", false}},
			0,
			2,
			m.w,
			-1,
		) + "\n" + m.theme.dim(
			"Default: No. Click outside or press Esc to cancel.",
		)
	}
	title := "Message to " + m.panes[m.focus].Agent.Name
	if m.rejecting {
		title = "Reject permission · optional message"
	}
	m.input.SetWidth(max(10, m.w-2))
	m.input.SetHeight(min(4, max(1, m.h-5)))
	input := m.input.View()
	height := lipgloss.Height(input)
	m.zones = append(m.zones, zone{Rect: Rect{0, 2, m.w, height}, Action: "input", Pane: -1})
	return m.theme.title(
		fit(title, m.w),
	) + "\n\n" + input + "\n" + m.buttonRow(
		[]button{{"Send", "send", m.opts.ReadOnly}, {"Cancel", "esc", false}},
		0,
		2+height,
		m.w,
		-1,
	)
}

func (m *Model) mousePickerView() string {
	m.input.SetWidth(max(10, m.w-2))
	m.input.SetHeight(1)
	m.input.Placeholder = "Filter sessions by title, ID or path"
	input := m.input.View()
	height := lipgloss.Height(input)
	m.zones = append(m.zones, zone{Rect: Rect{0, 2, m.w, height}, Action: "input", Pane: -1})
	var b strings.Builder
	b.WriteString("Sessions · click a row to add/remove a pane\n" + input + "\n")
	list := m.matches()
	limit := max(1, m.h-height-5)
	m.pickIndex = min(m.pickIndex, max(0, len(list)-1))
	start := max(0, m.pickIndex-limit+1)
	for i := start; i < min(len(list), start+limit); i++ {
		s := list[i]
		mark := "[ ]"
		for j := range m.panes {
			p := &m.panes[j]
			if p.Agent.Session == s.ID && !m.hidden[s.ID] {
				mark = "[x]"
			}
		}
		text := fit(mark+" "+s.Title+" · "+s.Directory, m.w)
		y := 2 + height + i - start
		m.zones = append(m.zones, zone{Rect: Rect{0, y, m.w, 1}, Action: fmt.Sprintf("pick:%d", i), Pane: -1})
		if m.mouseY == y || i == m.pickIndex {
			text = lipgloss.NewStyle().Background(lipgloss.Color("24")).Render(text)
		}
		b.WriteString(text + "\n")
	}
	y := 2 + height + min(len(list)-start, limit)
	b.WriteString(m.buttonRow([]button{{"Cancel", "esc", false}}, 0, y, m.w, -1))
	return b.String()
}
