package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// gitFrame builds the launcher body row by row so mouse zones match the rendered lines.
type gitFrame struct {
	m     *Model
	width int
	rows  []string
}

func (f *gitFrame) line(s string) {
	f.rows = append(f.rows, ansi.Truncate(s, f.width, "…"))
}

func (f *gitFrame) box(title string, lines []string) {
	t := &f.m.theme
	border := lipgloss.NewStyle().Foreground(t.Border)
	inner := max(1, f.width-4)
	title = fit(title, max(1, f.width-6))
	fill := max(0, f.width-5-lipgloss.Width(title))
	f.line(border.Render("╭─ ") + lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(title) +
		border.Render(" "+strings.Repeat("─", fill)+"╮"))
	for _, l := range lines {
		l = ansi.Truncate(l, inner, "…")
		f.line(border.Render("│ ") + l + strings.Repeat(" ", max(0, inner-lipgloss.Width(l))) + border.Render(" │"))
	}
	f.line(border.Render("╰" + strings.Repeat("─", max(0, f.width-2)) + "╯"))
}

func (m *Model) gitView() string {
	if m.gitTUI.Open {
		return m.gitTUIView()
	}
	return m.changesView()
}

// gitTUIFullHeight is the body height the framed launcher needs: header, four boxes and the footer.
const gitTUIFullHeight = 23

func (m *Model) gitTUIView() string {
	if m.w < 45 || m.h < 11 {
		return fit("Git TUI launcher · resize to 45×12 · Esc back", m.w)
	}
	g := &m.gitTUI
	f := &gitFrame{m: m, width: m.w}
	header := "Git / " + valueOr(g.Target.Member, "team checkout")
	f.line(fit(header, m.w-22) + strings.Repeat(" ", max(1, m.w-22-lipgloss.Width(fit(header, m.w-22)))) +
		m.buttonRow([]button{{Label: "Esc Built-in diff", Action: "gittui:cancel"}}, max(0, m.w-21), 0, 21, -1))
	help := []string{
		"j/k choose  Space remember  Enter open  i checkout  r detect again  Esc cancel",
		"Dashboard resumes when the client exits; Esc inside a client may only go back.",
	}
	switch {
	case g.Details:
		m.gitTUIDetails(f)
		help = []string{"Esc back to launcher", ""}
	case m.h < gitTUIFullHeight:
		m.gitTUICompact(f)
		help = help[:1]
	default:
		m.gitTUISections(f)
	}
	for len(f.rows) < m.h-1-len(help) {
		f.rows = append(f.rows, "")
	}
	status := g.Status
	if status == "" {
		status = "Target is fixed while this launcher is open; rechecked before starting."
	}
	f.line(m.theme.chip("GIT TUI", m.theme.Accent) + " " + m.gitStatusStyle(status).Render(fit(status, m.w-11)))
	for _, h := range help {
		f.line(m.theme.dim(fit(h, m.w)))
	}
	return strings.Join(f.rows, "\n")
}

func (m *Model) gitStatusStyle(status string) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(m.theme.Muted)
	if strings.HasPrefix(status, "Could not") || strings.HasPrefix(status, "Detection failed") {
		return style.Foreground(m.theme.Danger)
	}
	if strings.Contains(status, "unavailable") || strings.Contains(status, "changed") ||
		strings.Contains(status, "no longer") {
		return style.Foreground(m.theme.Warning)
	}
	return style
}

func (m *Model) gitTUISections(f *gitFrame) {
	g := &m.gitTUI
	f.box("Launch target", []string{
		"Checkout: " + fit(valueOr(g.Target.Root, valueOr(g.Target.Dir, "none")), f.width-16),
		"Member: " + valueOr(g.Target.Member, "team") + "  |  " + lipgloss.NewStyle().Foreground(m.theme.Accent).
			Render("[i Checkout details]"),
	})
	member := lipgloss.Width("Member: " + valueOr(g.Target.Member, "team") + "  |  ")
	m.zones = append(m.zones, zone{Rect: Rect{2 + member, len(f.rows) - 2, 20, 1}, Action: "gittui:details", Pane: -1})
	warn := lipgloss.NewStyle().Foreground(m.theme.Warning)
	notice := []string{
		warn.Render("Dashboard pauses while Git TUI is open."),
		"Pending requests are visible again after you return.",
		"External client uses its own keys, settings and Git actions.",
	}
	if g.Blocked != nil && m.opts.ReadOnly && !m.opts.NoGit {
		notice = []string{warn.Render(g.Blocked[0]), g.Blocked[1], g.Blocked[2]}
	}
	f.box("Full-screen handoff", notice)
	f.box("Installed clients / choose one", m.gitClientLines(f, 2, len(f.rows)+1))
	m.gitTUIOptions(f)
}

func (m *Model) gitClientLines(f *gitFrame, x, start int) []string {
	g := &m.gitTUI
	switch {
	case g.Blocked != nil && m.opts.ReadOnly && !m.opts.NoGit:
		return []string{"Launch unavailable in --read-only", "", "", ""}
	case g.Blocked != nil:
		return append(append([]string{}, g.Blocked...), "", "")[:4]
	case g.Detecting:
		return []string{"Checking PATH for lazygit, gitui, tig…", "", "", ""}
	case len(g.Clients) == 0:
		return []string{
			"No supported Git TUI found in PATH.", "Built-in diff remains available.",
			"Optional clients: lazygit, gitui, tig.", "[r Check again]",
		}
	}
	lines := make([]string, 0, 4)
	for i := range g.Clients {
		c := &g.Clients[i]
		text := fmt.Sprintf("  %-8s %s · %s", c.Name, c.Hint, c.Path)
		if i == g.Index {
			text = lipgloss.NewStyle().Background(m.theme.Focus).Bold(true).
				Render(fit("> "+strings.TrimPrefix(text, "  "), f.width-4))
		}
		lines = append(lines, text)
		m.zones = append(m.zones, zone{
			Rect:   Rect{x, start + i, f.width - 2*x, 1},
			Action: "gittui:client:" + strconv.Itoa(i), Pane: -1,
		})
	}
	for len(lines) < 3 {
		lines = append(lines, "")
	}
	return append(lines, m.theme.dim("Checked PATH only. No installation, setup or login performed."))
}

func (m *Model) gitTUIOptions(f *gitFrame) {
	remember, actions := m.gitLaunchControls(2, len(f.rows)+1, f.width-4)
	f.box("Launch options", []string{remember, actions})
}

// gitLaunchControls renders the remember toggle (row y) and the action buttons (row y+1) with their zones.
func (m *Model) gitLaunchControls(x, y, width int) (remember, actions string) {
	g := &m.gitTUI
	canOpen := g.Blocked == nil && !g.Detecting && !g.Starting && len(g.Clients) > 0
	buttons := []button{{Label: "Esc Keep built-in diff", Action: "gittui:cancel"}}
	if canOpen {
		mark := "[ ]"
		if g.Remember {
			mark = "[x]"
		}
		remember = mark + " Remember this choice for this agent-deck run"
		name := g.Clients[max(0, min(g.Index, len(g.Clients)-1))].Name
		buttons = []button{{Label: "Enter Open " + name, Action: "gittui:launch"}, buttons[0]}
	}
	if g.Starting {
		remember = "Starting " + g.Clients[g.Index].Name + "…"
	}
	actions = m.buttonRow(buttons, x, y+1, width, -1)
	if canOpen {
		m.zones = append(m.zones, zone{
			Rect:   Rect{x, y, lipgloss.Width(remember), 1},
			Action: "gittui:remember", Pane: -1,
		})
	}
	return remember, actions
}

// gitTUICompact keeps the target, pause cue, choices and actions on screen when the framed layout
// does not fit the terminal height.
func (m *Model) gitTUICompact(f *gitFrame) {
	g := &m.gitTUI
	f.line(fit("Checkout: "+valueOr(g.Target.Root, valueOr(g.Target.Dir, "none"))+" · "+
		valueOr(g.Target.Member, "team"), f.width))
	notice := "Dashboard pauses while Git TUI is open."
	if g.Blocked != nil && m.opts.ReadOnly && !m.opts.NoGit {
		notice = g.Blocked[0]
	}
	f.line(lipgloss.NewStyle().Foreground(m.theme.Warning).Render(fit(notice, f.width)))
	lines := m.gitClientLines(f, 0, len(f.rows))
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	budget := max(1, m.h-len(f.rows)-4)
	for _, line := range lines[:min(len(lines), budget)] {
		f.line(line)
	}
	remember, actions := m.gitLaunchControls(0, len(f.rows), f.width)
	f.line(remember)
	f.line(actions)
}

func (m *Model) gitTUIDetails(f *gitFrame) {
	g := &m.gitTUI
	lines := []string{
		"Member: " + valueOr(g.Target.Member, "team checkout"),
		"Code directory: " + valueOr(g.Target.Dir, "none"),
		"Git root: " + valueOr(g.Target.Root, "unresolved"),
		"",
	}
	for i := range g.Clients {
		lines = append(lines, g.Clients[i].Name+": "+g.Clients[i].Path)
	}
	if m.gitTarget() != g.Target {
		lines = append(lines, "", "Checkout changed since the launcher opened; reopen it.")
	}
	f.box("Checkout details", lines)
}

func (m *Model) gitTUIClick(z zone, ok bool) (bool, tea.Cmd) {
	if m.workspace.View != "git" || !ok || !strings.HasPrefix(z.Action, "gittui:") {
		return false, nil
	}
	g := &m.gitTUI
	switch action := strings.TrimPrefix(z.Action, "gittui:"); {
	case action == "open":
		return true, m.openGitTUI()
	case action == "cancel":
		g.Open = false
	case action == "launch":
		return true, m.launchGitClient()
	case action == "remember":
		g.Remember = !g.Remember
	case action == "details":
		g.Details = true
	case strings.HasPrefix(action, "client:"):
		if i, err := strconv.Atoi(strings.TrimPrefix(action, "client:")); err == nil && i < len(g.Clients) {
			g.Index = i
		}
	}
	return true, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (m *Model) gitLaunchBar(y, width int) string {
	disabled := m.opts.NoGit
	open := m.buttonRow([]button{{Label: "t Open Git TUI", Action: "gittui:open", Disabled: disabled}}, 0, y, width, -1)
	text := " Use an installed client for commits, staging and history; built-in diff needs nothing installed."
	if m.opts.ReadOnly {
		text = " Read-only: external clients are disabled; built-in diff stays available."
	}
	return open + m.theme.dim(fit(text, max(1, width-lipgloss.Width(open))))
}
