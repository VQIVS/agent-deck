package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	keybind "github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/config"
	"github.com/vqivs/agent-deck/internal/gitinfo"
	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/state"
	"github.com/vqivs/agent-deck/internal/team"
	"github.com/vqivs/agent-deck/internal/transcript"
)

// Options selects dashboard behavior, local integrations, and color theme.
type Options struct {
	ReadOnly, NoGit, Notify, Auto bool
	Theme                         string
}
type pane struct {
	Agent         config.Agent
	State         state.State
	Git           string
	View          viewport.Model
	Loading       bool
	Pending       []api.Event
	Expanded, Raw map[string]bool
	Selected      string
	Content       string
	ContextLimit  int64
	Messages      []api.Message
	History       bool
	Ranges        []transcript.Range
	HistoryParts  int
	NewParts      int
}
type streamMsg struct {
	Dir        string
	Event      api.Event
	Err        error
	Connection bool
}
type snapshotMsg struct {
	Index        int
	Messages     []api.Message
	Status       api.Status
	Err          error
	ContextLimit int64
}
type gitMsg struct {
	Index int
	Info  gitinfo.Info
}
type (
	tickMsg   time.Time
	actionMsg struct {
		Err  error
		Text string
	}
)

// Model owns terminal state and dispatches asynchronous results through Bubble Tea.
type Model struct {
	workspace                    workspaceUI
	changes                      changesUI
	gitTUI                       gitTUIUI
	composer                     composerUI
	summary                      SummaryState
	palette                      PaletteScreen
	board                        boardUI
	channel                      channelUI
	deck                         config.Deck
	client                       *api.Client
	context                      func() context.Context
	opts                         Options
	panes                        []pane
	events                       chan tea.Msg
	connections                  map[string]string
	w, h, focus                  int
	zoom, help, editing, confirm bool
	input                        textarea.Model
	note                         string
	inventory                    api.Inventory
	discovering, picker          bool
	pickIndex                    int
	hidden                       map[string]bool
	requestEvents                map[string][]api.Event
	permissionTarget             permissionTarget
	permissionConfirm, rejecting bool
	compactConfirm               bool
	safeReplies                  map[string]bool
	inbox                        InboxScreen
	zones                        []zone
	mouseX, mouseY               int
	hoverAction                  string
	transcript                   *transcript.Renderer
	showReasoning, searching     bool
	frame                        int
	teams                        *teamUI
	skipTick                     bool
	theme                        Theme
}

func New(ctx context.Context, d *config.Deck, opts Options) *Model {
	m := &Model{
		deck:        *d,
		client:      api.New(d.Server, opts.ReadOnly),
		context:     func() context.Context { return ctx },
		opts:        opts,
		events:      make(chan tea.Msg, 256),
		connections: map[string]string{},
		hidden:      map[string]bool{},
		w:           120,
		h:           40,
	}
	m.requestEvents = map[string][]api.Event{}
	m.safeReplies = map[string]bool{}
	m.theme, _ = ResolveTheme(opts.Theme)
	m.transcript = transcript.New()
	m.input = textarea.New()
	m.input.KeyMap.InsertNewline = keybind.NewBinding(keybind.WithKeys("ctrl+j"))
	inputStyle := textarea.Style{
		Text:        lipgloss.NewStyle().Foreground(m.theme.Text),
		Prompt:      lipgloss.NewStyle().Foreground(m.theme.Accent),
		Placeholder: lipgloss.NewStyle().Foreground(m.theme.Muted),
		LineNumber:  lipgloss.NewStyle().Foreground(m.theme.Muted),
		EndOfBuffer: lipgloss.NewStyle().Foreground(m.theme.Border),
		CursorLine:  lipgloss.NewStyle().Background(m.theme.Focus),
	}
	m.input.FocusedStyle = inputStyle
	m.input.BlurredStyle = inputStyle
	m.input.CharLimit = 16000
	m.input.SetHeight(4)
	agents := d.Agents()
	for i := range agents {
		a := &agents[i]
		p := pane{Agent: *a, State: state.New(), View: viewport.New(40, 10), Git: "git loading…"}
		if opts.NoGit {
			p.Git = "git disabled · configured branch: " + a.Branch
		}
		m.panes = append(m.panes, p)
		m.connections[a.Dir] = "connecting"
	}
	return m
}

func (m *Model) wait() tea.Cmd {
	ctx, events := m.context(), m.events
	return GuardTeamReload(ctx, func() tea.Msg {
		select {
		case v := <-events:
			return v
		case <-ctx.Done():
			return nil
		}
	})
}
func tick() tea.Cmd { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func (m *Model) Init() tea.Cmd {
	if m.teams != nil && m.teams.Screen == "picker" {
		return tea.Batch(tick(), m.listTeams())
	}
	for dir := range m.connections {
		m.startStream(dir)
	}
	cmds := []tea.Cmd{m.wait(), m.discover(), m.startChannel()}
	if !m.skipTick {
		cmds = append(cmds, tick())
	}
	for dir := range m.connections {
		cmds = append(cmds, m.requests(dir))
	}
	for i := range m.panes {
		cmds = append(cmds, m.snapshot(i))
		if !m.opts.NoGit {
			cmds = append(cmds, m.git(i))
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) snapshot(i int) tea.Cmd {
	if m.panes[i].Loading {
		return nil
	}
	m.panes[i].Loading = true
	m.panes[i].Pending = nil
	a := m.panes[i].Agent
	ctx, client := m.context(), m.client
	return GuardTeamReload(ctx, func() tea.Msg {
		ms, e := client.Messages(ctx, a.Dir, a.Session)
		if e != nil {
			return snapshotMsg{Index: i, Err: e}
		}
		ss, e := client.Status(ctx, a.Dir)
		var limit int64
		if latest := team.LatestAssistant(ms); latest != nil {
			limit, _ = client.ContextLimit(ctx, a.Dir,
				api.ModelRef{ProviderID: latest.Info.ProviderID, ModelID: latest.Info.ModelID})
		}
		return snapshotMsg{Index: i, Messages: ms, Status: ss[a.Session], Err: e, ContextLimit: limit}
	})
}

func (m *Model) git(i int) tea.Cmd {
	a := m.panes[i].Agent
	if a.CodeDir != "" {
		a.Dir = a.CodeDir
	}
	return func() tea.Msg { return gitMsg{i, gitinfo.Read(m.context(), a.Dir, a.Base)} }
}

func (m *Model) notify(i int) tea.Cmd {
	if !m.opts.Notify || runtime.GOOS != "darwin" {
		return nil
	}
	p := m.panes[i]
	text := p.Agent.Name + " is " + p.State.DisplayStatus()
	if p.State.Waiting() {
		text += " · " + p.State.WaitingText()
	}
	return func() tea.Msg {
		script := "display notification " + strconv.Quote(text) + " with title \"agent-deck\""
		err := exec.CommandContext(m.context(), "osascript", "-e", script).
			Run()
		if err != nil {
			return actionMsg{Err: err}
		}
		return nil
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	msg = unwrapReload(msg)
	if handled, cmd := m.workspaceResult(msg); handled {
		return m, cmd
	}
	if m.paneResult(msg) {
		return m, nil
	}
	if handled, cmd := m.handleTeams(msg); handled {
		return m, cmd
	}
	var cmds []tea.Cmd
	switch v := msg.(type) {
	case tea.MouseMsg:
		cmd := m.mouse(v)
		return m, cmd
	case requestsMsg:
		cmds = append(cmds, m.applyRequests(&v)...)
	case permissionReplyMsg:
		cmds = append(cmds, m.replied(v))
	case inventoryMsg:
		cmds = append(cmds, m.applyInventory(&v)...)
	case tea.WindowSizeMsg:
		m.w = v.Width
		m.h = v.Height
	case tickMsg:
		cmds = append(cmds, m.updateTick(v)...)
	case streamMsg:
		cmds = append(cmds, m.updateStream(&v)...)
	case messageSent:
		m.applyMessageSent(v)
	case actionMsg:
		if v.Err != nil {
			m.note = v.Err.Error()
		} else {
			m.note = v.Text
		}
	case tea.KeyMsg:
		return m.updateKey(v)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) paneResult(msg tea.Msg) bool {
	switch v := msg.(type) {
	case snapshotMsg:
		if v.Err == nil && v.Index >= 0 && v.Index < len(m.panes) {
			m.panes[v.Index].Messages = v.Messages
		}
		m.applySnapshot(&v)
	case gitMsg:
		if v.Index >= 0 && v.Index < len(m.panes) {
			m.panes[v.Index].Git = v.Info.String()
		}
	default:
		return false
	}
	return true
}

func (m *Model) applyInventory(v *inventoryMsg) []tea.Cmd {
	m.discovering = false
	if v.Err != nil {
		m.note = "Discovery: " + v.Err.Error()
		return nil
	}
	m.inventory = v.Inventory
	var cmds []tea.Cmd
	for _, dir := range v.Inventory.Dirs {
		if _, ok := m.connections[dir]; !ok {
			m.connections[dir] = "connecting"
			m.startStream(dir)
			cmds = append(cmds, m.requests(dir))
		}
	}
	if len(v.Inventory.Warnings) > 0 {
		m.note = v.Inventory.Warnings[0]
	}
	if m.opts.Auto {
		agents := activeAgents(&v.Inventory)
		for i := range agents {
			cmds = append(cmds, m.add(&agents[i])...)
		}
	}
	return cmds
}

func (m *Model) updateTick(v tickMsg) []tea.Cmd {
	m.frame++
	cmds := []tea.Cmd{tick()}
	cmds = append(cmds, m.workspaceRefresh()...)
	if m.shouldReadBoard() {
		cmds = append(cmds, m.readBoard())
	}
	cmds = append(cmds, m.safeCommandReplies()...)
	cmds = append(cmds, m.healthTick()...)
	second := time.Time(v).Unix()
	if m.teams != nil && second%3 == 0 {
		cmds = append(cmds, m.watchTeam())
	}
	if m.teams != nil && m.teams.Screen == "picker" {
		if second%30 == 0 {
			cmds = append(cmds, m.listTeams())
		}
		return cmds
	}
	if second%3 == 0 {
		cmds = append(cmds, m.readReports())
		for dir := range m.connections {
			cmds = append(cmds, m.requests(dir))
		}
	}
	if second%30 == 0 {
		cmds = append(cmds, m.discover())
	}
	if second%10 == 0 && !m.opts.NoGit {
		for i := range m.panes {
			cmds = append(cmds, m.git(i))
		}
	}
	return cmds
}

func (m *Model) updateStream(v *streamMsg) []tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, m.wait())
	if v.Connection {
		if v.Err != nil {
			m.connections[v.Dir] = "offline: " + v.Err.Error()
		} else {
			m.connections[v.Dir] = "live"
			cmds = append(cmds, m.requests(v.Dir))
			for i := range m.panes {
				if m.panes[i].Agent.Dir == v.Dir {
					cmds = append(cmds, m.snapshot(i))
				}
			}
		}
		return cmds
	}
	if strings.HasPrefix(v.Event.Type, "permission.") || strings.HasPrefix(v.Event.Type, "question.") {
		if _, busy := m.requestEvents[v.Dir]; busy {
			m.requestEvents[v.Dir] = append(m.requestEvents[v.Dir], v.Event)
		}
	}
	if v.Event.Type == "session.created" || v.Event.Type == "session.deleted" {
		cmds = append(cmds, m.discover())
	}
	cmds = append(cmds, m.discoverActivity(v)...)
	for i := range m.panes {
		if m.applyPaneEvent(i, v) {
			cmds = append(cmds, m.notify(i))
		}
	}
	return cmds
}

func (m *Model) discoverActivity(v *streamMsg) []tea.Cmd {
	var cmds []tea.Cmd
	if m.opts.Auto && v.Event.Type == "session.status" {
		var p struct {
			SessionID string     `json:"sessionID"`
			Status    api.Status `json:"status"`
		}
		if json.Unmarshal(v.Event.Properties, &p) == nil && p.Status.Type != "idle" {
			for _, s := range m.inventory.Sessions {
				if s.ID == p.SessionID && s.Directory == v.Dir {
					a := agent(s)
					cmds = append(cmds, m.add(&a)...)
				}
			}
		}
	}
	return cmds
}

func (m *Model) applyPaneEvent(i int, v *streamMsg) bool {
	p := &m.panes[i]
	if p.Agent.Dir != v.Dir {
		return false
	}
	requestEvent := strings.HasPrefix(v.Event.Type, "permission.") || strings.HasPrefix(v.Event.Type, "question.")
	if p.Loading && !requestEvent && len(p.Pending) < 4096 {
		p.Pending = append(p.Pending, v.Event)
	}
	before := p.State.DisplayStatus()
	changed := p.State.Apply(v.Event, p.Agent.Session, time.Now())
	terminal := p.State.Waiting() || p.State.Status == "IDLE" || p.State.Status == "ERROR"
	return changed && before != p.State.DisplayStatus() && terminal
}

func (m *Model) applySnapshot(v *snapshotMsg) {
	if v.Index >= len(m.panes) {
		return
	}
	p := &m.panes[v.Index]
	p.Loading = false
	if v.Err != nil {
		m.note = v.Err.Error()
	} else {
		p.State.Snapshot(v.Messages)
		p.ContextLimit = v.ContextLimit
		if p.State.Status != "ERROR" {
			p.State.SetStatus(v.Status.Type, time.Now())
		}
		p.State.Seen = map[string]bool{}
		for _, e := range p.Pending {
			p.State.Apply(e, p.Agent.Session, time.Now())
		}
	}
	p.Pending = nil
}

func (m *Model) updateKey(v tea.KeyMsg) (tea.Model, tea.Cmd) {
	if v.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if handled, cmd := m.workspaceKey(v); handled {
		return m, cmd
	}
	if m.palette.Open {
		cmd := m.paletteKey(&m.palette, v)
		return m, cmd
	}
	if handled, cmd := m.workspaceGlobalKey(v); handled {
		return m, cmd
	}
	if m.workspace.View == "git" {
		cmd := m.gitViewKey(v)
		return m, cmd
	}
	if m.workspace.View == "summary" {
		cmd := m.summaryKey(v)
		return m, cmd
	}
	if m.board.Open {
		cmd := m.boardKey(v)
		return m, cmd
	}
	if m.channel.Open {
		cmd := m.channelKey(v)
		return m, cmd
	}
	if m.inbox.Open {
		cmd := m.inboxKey(&m.inbox, v)
		return m, cmd
	}
	if m.picker {
		cmd := m.updatePicker(v)
		return m, cmd
	}
	if handled, cmd := m.dialogKey(v); handled {
		return m, cmd
	}
	return m.dashboardKey(v)
}

func (m *Model) dashboardKey(v tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := v.String()
	var cmds []tea.Cmd
	if key == "p" {
		m.picker = true
		m.pickIndex = 0
		m.input.Reset()
		m.input.SetHeight(1)
		m.input.Focus()
		return m, textarea.Blink
	}
	if len(m.panes) == 0 {
		cmd := m.emptyKey(key)
		return m, cmd
	}
	if handled := m.paneNavigation(key); handled {
		return m, nil
	}
	switch key {
	case "/":
		m.searching = true
		m.input.Reset()
		m.input.SetHeight(1)
		m.input.Focus()
		return m, textarea.Blink
	case "R":
		m.showReasoning = !m.showReasoning
	case "j", "k":
		m.selectCard(key == "j")
	case "v":
		m.toggleRaw()
	case "y", "Y", "x":
		cmd := m.permissionKey(key)
		return m, cmd
	case "q":
		return m, tea.Quit
	case "?":
		m.help = true
	case "N":
		m.opts.Notify = !m.opts.Notify
		m.note = fmt.Sprintf("Notifications: %t", m.opts.Notify)
	case "m", "n", "s", "a":
		cmd := m.sessionKey(key)
		return m, cmd
	case "r":
		cmds = m.refreshCommands()
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) emptyKey(key string) tea.Cmd {
	switch key {
	case "q":
		return tea.Quit
	case "r":
		return m.discover()
	case "?":
		m.help = true
	}
	return nil
}

func (m *Model) paneNavigation(key string) bool {
	if m.historyNavigation(key) {
		return true
	}
	switch key {
	case "c":
		m.channel.Open = true
	case "i":
		m.inbox.Open = true
	case "C":
		m.compactConfirm = !m.opts.ReadOnly
	case "tab":
		m.cycle(1)
	case "shift+tab":
		m.cycle(-1)
	case "left", "right", "up", "down":
		m.navigate(key)
	case "enter":
		if m.zoom && m.panes[m.focus].Selected != "" {
			m.toggleCard(m.focus, m.panes[m.focus].Selected)
		} else {
			m.zoom = !m.zoom
		}
	case "esc":
		m.zoom = false
	case "pgup":
		m.panes[m.focus].History = true
		m.panes[m.focus].View.HalfPageUp()
	case "pgdown":
		m.panes[m.focus].View.HalfPageDown()
	default:
		return false
	}
	return true
}

func (m *Model) sessionKey(key string) tea.Cmd {
	if m.opts.ReadOnly {
		m.note = "Read-only: messaging, nudge, abort, and attach disabled"
		return nil
	}
	switch key {
	case "m":
		return m.openComposer(m.focus)
	case "n":
		return m.prompt("continue your task")
	case "s":
		m.confirm = true
	case "a":
		a := m.panes[m.focus].Agent
		return tea.ExecProcess(
			exec.CommandContext(m.context(), "opencode", "attach", m.deck.Server, "--dir", a.Dir, "-s", a.Session),
			func(err error) tea.Msg { return actionMsg{Err: err, Text: "Returned from attach; press r to resync"} },
		)
	}
	return nil
}

func (m *Model) refreshCommands() []tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, m.discover())
	for dir := range m.connections {
		cmds = append(cmds, m.requests(dir))
	}
	for i := range m.panes {
		cmds = append(cmds, m.snapshot(i))
		if !m.opts.NoGit {
			cmds = append(cmds, m.git(i))
		}
	}
	return cmds
}

func (m *Model) prompt(text string) tea.Cmd {
	send, name := m.promptTo(m.focus, text), m.panes[m.focus].Agent.Name
	return func() tea.Msg { return actionMsg{Err: send(), Text: "Message sent to " + name} }
}

// promptTo binds the send to pane i's session now, so later focus or order changes cannot retarget it.
func (m *Model) promptTo(i int, text string) func() error {
	a, ctx := m.panes[i].Agent, m.context()
	if m.teams != nil && m.teams.Manager != nil {
		manager := m.teams.Manager
		for n := range manager.Team.Members {
			if manager.Team.Members[n].Session == a.Session {
				return func() error { return manager.Send(ctx, n, text) }
			}
		}
	}
	client := m.client
	return func() error { return client.Prompt(ctx, a.Dir, a.Session, text) }
}

func (m *Model) navigate(key string) {
	indices, r, _, _ := m.page(max(1, m.h-3))
	if len(indices) == 0 {
		return
	}
	local := 0
	for i, n := range indices {
		if n == m.focus {
			local = i
		}
	}
	cur := r[local]
	cx, cy := cur.X+cur.W/2, cur.Y+cur.H/2
	best, score := local, int(^uint(0)>>1)
	for i, p := range r {
		if i == local {
			continue
		}
		dx, dy := p.X+p.W/2-cx, p.Y+p.H/2-cy
		valid := directionMatches(key, dx, dy)
		if !valid {
			continue
		}
		s := dx*dx + dy*dy
		if s < score {
			best, score = i, s
		}
	}
	m.focus = indices[best]
	if best == local {
		if key == "right" || key == "down" {
			m.cycle(1)
		} else {
			m.cycle(-1)
		}
	}
}

func directionMatches(key string, dx, dy int) bool {
	switch key {
	case "left":
		return dx < 0
	case "right":
		return dx > 0
	case "up":
		return dy < 0
	case "down":
		return dy > 0
	default:
		return false
	}
}

// Strip all terminal control sequences from remote content before styling it.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}

func fit(s string, w int) string {
	return ansi.Truncate(strings.ReplaceAll(clean(s), "\n", " "), max(1, w), "…")
}

func (m *Model) renderPane(i, x, y, w, h int) string {
	p := &m.panes[i]
	inner := max(1, w-2)
	controls := paneButtons(p.State.Waiting() && len(p.State.Permissions) > 0, m.opts.ReadOnly, m.zoom)
	plan := panePlan(Rect{x, y, w, h}, controls)
	buttonRows := len(plan.Controls)
	body := max(1, h-5-buttonRows)
	name := m.leasedName(i)
	if p.Agent.Role != "" {
		name = "★ " + name
	}
	if p.Agent.Parent != "" {
		name = "↳ " + name
	}
	m.zones = append(m.zones, zone{Rect: Rect{x, y, w, h}, Action: "focus", Pane: i})
	header := m.theme.StateBadge(p.State.DisplayStatus()) + " " +
		lipgloss.NewStyle().Foreground(MemberColor(p.Agent.Name)).Render(fit(name, max(1, inner-12)))
	buttons := m.paneControlRows(controls, &plan, x, inner, i)
	atBottom := p.View.AtBottom()
	p.observeHistoryParts()
	anchorID, anchorLine := transcriptAnchor(p.Ranges, p.View.YOffset)
	p.View.Width = inner
	p.View.Height = body
	text, ranges := m.transcript.Render(
		p.State.Parts,
		transcript.Options{
			Width:     inner,
			Dir:       p.Agent.Dir,
			Dark:      m.theme.Dark,
			Full:      p.History,
			Reasoning: m.showReasoning,
			Expanded:  p.Expanded,
			Raw:       p.Raw,
			Selected:  p.Selected,
			Frame:     m.frame,
		},
	)
	p.View.SetContent(text)
	p.Content = text
	if p.History {
		if offset, ok := transcriptAnchorOffset(ranges, anchorID, anchorLine); ok {
			p.View.SetYOffset(offset)
		}
	} else if atBottom {
		p.View.GotoBottom()
	}
	p.Ranges = ranges
	detail := m.paneHealth(i) + " · tool: " + p.State.Tool()
	detail += p.historyNotice()
	content := header + "\n" + buttons + "\n" + fit(detail, inner) + "\n" + p.View.View() + "\n" + fit(p.Git, inner)
	if p.State.Waiting() {
		lines := strings.Split(ansi.Hardwrap(clean(p.State.WaitingText()), inner, true), "\n")
		lines = lines[:min(len(lines), body)]
		waiting := lipgloss.NewStyle().Foreground(m.theme.Waiting).Bold(true).Render(strings.Join(lines, "\n"))
		// Keep pending requests pinned above the transcript even while its tail follows.
		p.View.Height = max(0, body-len(lines))
		tail := ""
		if p.View.Height > 0 {
			tail = "\n" + p.View.View()
		}
		content = header + "\n" + buttons + "\n" + fit(
			"! permission/question needs attention",
			inner,
		) + "\n" + waiting + tail + "\n" + fit(
			p.Git,
			inner,
		)
	}
	bodyY := y + 3 + buttonRows
	if p.State.Waiting() {
		bodyY += min(body, len(strings.Split(ansi.Hardwrap(clean(p.State.WaitingText()), inner, true), "\n")))
	}
	m.cardZones(i, x, bodyY, inner, ranges)
	border := m.theme.Border
	if i == m.focus {
		border = m.theme.Accent
	}
	if p.State.Waiting() {
		border = m.theme.Waiting
	}
	return m.theme.panel(inner).Height(max(1, h-2)).BorderForeground(border).Render(content)
}

func (m *Model) cardZones(i, x, bodyY, inner int, ranges []transcript.Range) {
	p := &m.panes[i]
	for _, r := range ranges {
		top := max(r.Start, p.View.YOffset)
		bottom := min(r.End, p.View.YOffset+p.View.Height)
		if r.ID != "" && bottom > top {
			m.zones = append(
				m.zones,
				zone{
					Rect:   Rect{x + 1, bodyY + top - p.View.YOffset, inner, bottom - top},
					Action: "card:" + r.ID,
					Pane:   i,
				},
			)
		}
	}
}

func (m *Model) View() string {
	m.zones = nil
	if m.palette.Open {
		return m.paletteView(&m.palette)
	}
	return m.workspaceView()
}

func (m *Model) mainView() string {
	if m.board.Open {
		return m.boardView()
	}
	if m.channel.Open {
		return m.channelView()
	}
	if m.inbox.Open {
		return m.inboxView(&m.inbox)
	}
	if m.teams != nil && (m.teams.Screen != "" || m.teams.Confirm != "") {
		return m.teamView()
	}
	if m.w < 20 || m.h < 8 {
		return "Resize terminal (20×8 minimum)"
	}
	header, live, waiting := m.dashboardHeader()
	bar := m.buttonRow(m.headerButtons(), 0, 1, m.w, -1)
	if m.searching {
		bar = "Find: " + m.input.View()
	}
	if m.h < 15 {
		return fit(fmt.Sprintf("WAITING %d · agent-deck", waiting), m.w) + "\n" + bar +
			"\n" + fit("Resize for panes (15+ rows)", m.w)
	}
	if view, ok := m.overlayView(header); ok {
		return view
	}
	if len(m.panes) == 0 {
		return header + "\n" + bar + "\nDiscovering active sessions…\n" +
			"No active sessions yet. Use Sessions to select panes.\n" + fit(m.note, m.w)
	}
	return m.dashboardView(header, bar, live)
}

func (m *Model) dashboardHeader() (header string, live, waiting int) {
	mode := ""
	counts := m.requestCounts()
	if m.opts.ReadOnly {
		mode = " · READ ONLY"
	}
	for _, s := range m.connections {
		if s == "live" {
			live++
		}
	}
	for i := range m.panes {
		p := &m.panes[i]
		if p.State.Waiting() {
			waiting++
		}
	}
	header = fit(
		fmt.Sprintf(
			"agent-deck%s · WAITING %d · requests %dP/%dQ · streams %d/%d · %s",
			mode,
			waiting,
			counts.Permissions, counts.Questions,
			live,
			len(m.connections),
			m.deck.Server,
		),
		m.w,
	)
	return header, live, waiting
}

func (m *Model) overlayView(header string) (string, bool) {
	if m.help {
		m.zones = nil
		return m.helpView(header), true
	}
	if m.picker {
		m.zones = nil
		w := m.w
		m.w = max(20, min(88, w-6))
		body := m.mousePickerView()
		width := m.w
		m.w = w
		return m.frameView(body, "  ↑↓ select  ·  Space add/remove  ·  Esc back  ·  Click a session", width), true
	}
	if m.editing && m.composer.Open {
		m.zones = nil
		return m.composerWorkspaceView(), true
	}
	if m.editing || m.rejecting || m.confirm || m.permissionConfirm || m.compactConfirm {
		m.zones = nil
		w := m.w
		m.w = max(20, min(88, w-6))
		body := m.dialogView()
		width := m.w
		m.w = w
		footer := "  Enter send  ·  Ctrl+J newline  ·  Esc keep draft  ·  Ctrl+X discard  ·  Confirmation defaults to No"
		return m.frameView(body, footer, width), true
	}
	return "", false
}

func (m *Model) helpView(header string) string {
	return header + "\n" + m.buttonRow([]button{{"Close", "esc", false}}, 0, 1, m.w, -1) + "\n" +
		"1–6 / Alt+1–6 workspace tabs · 2 Git diff · 3 Summary · 4 Channel\n" +
		"Ctrl+P command palette · Tab/arrows focus · Enter zoom · Esc back\n" +
		PaletteHelp(PaletteCommands(), m.paletteContext(), m.w) + "\nEnter sends · Ctrl+J newline · PgUp/PgDn scroll"
}

func (m *Model) dashboardView(header, bar string, live int) string {
	available := m.h - 4
	if m.editing || m.rejecting {
		available -= 5
	}
	indices, r, page, pages := m.page(available)
	header = fit(header+fmt.Sprintf(" · page %d/%d · %d sessions", page+1, pages, len(m.panes)), m.w)
	compact := false
	for _, p := range r {
		if p.H < 11 {
			compact = true
		}
	}
	if len(indices) == 0 {
		return header + "\nAll panes hidden. Press p to select sessions.\n"
	}
	canvas := m.paneCanvas(indices, r, available, compact)
	footer := m.dashboardFooter(live)
	return m.theme.title(header) + "\n" + bar + "\n" + canvas + "\n" + m.theme.dim(fit(footer, m.w))
}

func (m *Model) paneCanvas(indices []int, r []Rect, available int, compact bool) string {
	if m.zoom || compact {
		return m.renderPane(m.focus, 0, 2, m.w, max(5, available))
	}
	var rows []string
	for start := 0; start < len(r); {
		end := start + 1
		for end < len(r) && r[end].Y == r[start].Y {
			end++
		}
		var row []string
		for i := start; i < end; i++ {
			row = append(row, m.renderPane(indices[i], r[i].X, r[i].Y+2, r[i].W, r[i].H))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
		start = end
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *Model) dashboardFooter(live int) string {
	footer := "Tab/arrows focus · Enter zoom · p picker · m message · n nudge · s stop · a attach · ? help · q quit"
	switch {
	case m.confirm:
		footer = "Abort " + m.panes[m.focus].Agent.Name + "? [y/N]"
	case m.permissionConfirm:
		footer = "ALWAYS approve " + m.permissionTarget.Description + "? [y/N]"
	case m.note != "":
		footer = m.note
	case m.panes[m.focus].State.Waiting():
		footer = "WAITING · y approve once · Y approve always (confirm) · x reject · a attach for questions"
	case live < len(m.connections):
		for _, status := range m.connections {
			if status != "live" {
				footer = status
				break
			}
		}
	}
	return footer
}
