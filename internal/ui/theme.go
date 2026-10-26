package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Theme is the single source for chrome colors, hierarchy and interaction states.
// Transcript syntax colors are supplied by Glamour using the same Dark setting.
type Theme struct {
	Dark                                                                                       bool
	Background, Surface, Text, Muted, Border, Accent, Focus, Success, Warning, Waiting, Danger lipgloss.Color
}

func ResolveTheme(name string) (Theme, error) {
	dark := lipgloss.HasDarkBackground()
	switch name {
	case "", "auto":
	case "dark":
		dark = true
	case "light":
		dark = false
	default:
		return Theme{}, fmt.Errorf("theme must be auto, dark or light")
	}
	if dark {
		return Theme{
			true,
			"#141619",
			"#1C1F24",
			"#E4E7EC",
			"#929BAA",
			"#363C46",
			"#B7A3F5",
			"#303044",
			"#89C9A0",
			"#E5BB79",
			"#DDA2CD",
			"#EF9393",
		}, nil
	}
	return Theme{
		false,
		"#F5F5F3",
		"#FFFFFF",
		"#242832",
		"#657080",
		"#D4D8DE",
		"#6850A5",
		"#EAE4F5",
		"#28754D",
		"#916312",
		"#984583",
		"#B33E47",
	}, nil
}

func (t *Theme) title(s string) string {
	return lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(s)
}
func (t *Theme) dim(s string) string { return lipgloss.NewStyle().Foreground(t.Muted).Render(s) }
func (t *Theme) chip(s string, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Background(t.Surface).Render(" " + s + " ")
}

func (t *Theme) panel(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Border).
		Background(t.Surface).
		Foreground(t.Text)
}

// frame centers a surface and moves all its hit targets by the same cell offset.
func (m *Model) frameView(body, footer string, width int) string {
	width = max(1, min(width, m.w-4))
	panel := m.theme.panel(width).Render(body)
	x := max(0, (m.w-lipgloss.Width(panel))/2)
	y := max(0, (m.h-lipgloss.Height(panel)-3)/2)
	for i := range m.zones {
		m.zones[i].X += x + 1
		m.zones[i].Y += y + 1
	}
	lines := strings.Split(panel, "\n")
	for i := range lines {
		lines[i] = strings.Repeat(" ", x) + lines[i]
	}
	result := strings.Repeat("\n", y) + strings.Join(lines, "\n")
	remaining := max(1, m.h-lipgloss.Height(result)-1)
	result += strings.Repeat("\n", remaining)
	result += lipgloss.NewStyle().
		Width(m.w).
		Foreground(m.theme.Muted).
		Background(m.theme.Surface).
		Render(ansi.Truncate(footer, m.w, "…"))
	return lipgloss.NewStyle().Foreground(m.theme.Text).Background(m.theme.Background).Width(m.w).Render(result)
}

func (m *Model) teamView() string {
	if m.w < 40 || m.h < 20 {
		m.zones = nil
		return m.theme.dim("Resize terminal to 40×20 for team controls · Ctrl+C quits")
	}
	if m.teams.Screen == "picker" && m.teams.Confirm == "" {
		return m.teamPickerView()
	}
	width := max(20, min(88, m.w-6))
	// Content views use local coordinates; their frame translates hit targets once.
	w := m.w
	m.w = width
	body := m.teamContent()
	m.w = w
	return m.frameView(body, "  Tab next field  ·  Ctrl+S save  ·  Esc back  ·  Click to focus", width)
}

func (m *Model) teamPickerView() string {
	u := m.teams
	m.zones = nil
	width := max(20, min(94, m.w-6))
	center := func(s string) string { return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(s) }
	var b strings.Builder
	b.WriteString(
		"\n" + center(lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render("◈  agent-deck")) + "\n",
	)
	b.WriteString(center(m.theme.dim("A workspace for your agents")) + "\n\n")
	b.WriteString("  " + m.theme.title("Teams") + m.theme.dim(fmt.Sprintf("  %d saved", len(u.Teams))) + "\n")
	b.WriteString("  " + m.theme.dim("Choose a team to pick up where you left off.") + "\n\n")
	y := 7
	rows := max(1, (m.h-20)/3)
	start := max(0, u.Index-rows+1)
	if len(u.Teams) == 0 {
		b.WriteString("  " + m.theme.title("Your next team starts here") + "\n")
		b.WriteString("  " + m.theme.dim("Create a team or view all live sessions.") + "\n")
		b.WriteString("  " + m.theme.dim("Use agent-deck import/export in the terminal to transfer teams.") + "\n")
		y += 3
	}
	for i := start; i < min(len(u.Teams), start+rows); i++ {
		b.WriteString(m.teamPickerRow(i, width, y))
		y += 3
	}
	b.WriteString("\n")
	y++
	b.WriteString(m.teamPickerActions(width, y))
	if m.note != "" {
		b.WriteString("\n  " + m.theme.dim(fit(m.note, width-4)))
	}
	footer := "  ↑↓ select  ·  Enter open  ·  n new  ·  c duplicate  ·  a all sessions  ·  q quit"
	if m.opts.ReadOnly {
		footer = "  READ ONLY  ·  ↑↓ select  ·  Enter open  ·  a all sessions  ·  q quit"
	}
	return m.frameView(b.String(), footer, width)
}

func (m *Model) teamPickerRow(i, width, y int) string {
	u := m.teams
	var b strings.Builder
	t := &u.Teams[i]
	marker := "  "
	style := lipgloss.NewStyle().Width(width).Foreground(m.theme.Text)
	if m.hoverAction == fmt.Sprintf("team-row:%d", i) {
		style = style.Background(m.theme.Surface).Foreground(m.theme.Accent)
	}
	if i == u.Index {
		marker = "› "
		style = style.Background(m.theme.Focus)
	}
	// Hover highlights the whole row while preserving the keyboard selection.
	name := m.theme.title(fit(t.Name, width-20))
	b.WriteString(style.Render("  "+marker+name+m.theme.dim(fmt.Sprintf("  %d members", len(t.Members)))) + "\n")
	s, loaded := u.Status[t.Name]
	chips := m.theme.chip(
		fmt.Sprintf("%d busy", s.Busy),
		m.theme.Warning,
	) + " " + m.theme.chip(
		fmt.Sprintf("%d idle", s.Idle),
		m.theme.Success,
	) + " " + m.theme.chip(
		fmt.Sprintf("%d waiting", s.Waiting),
		m.theme.Waiting,
	)
	activity := "activity unavailable"
	if !loaded {
		activity = "Loading live status…"
	}
	if !s.Last.IsZero() {
		activity = "active " + relativeTime(s.Last)
	}
	if s.Offline > 0 {
		activity = fmt.Sprintf("%d offline · %s", s.Offline, activity)
	}
	b.WriteString(style.Render("    "+chips) + "\n")
	b.WriteString(style.Render("    "+m.theme.dim(fit(activity, width-6))) + "\n")
	m.zones = append(m.zones, zone{Rect: Rect{0, y, width, 3}, Action: fmt.Sprintf("team-row:%d", i), Pane: -1})
	return b.String()
}

func (m *Model) teamPickerActions(width, y int) string {
	u := m.teams
	var b strings.Builder
	b.WriteString(
		"  " + m.buttonRow(
			[]button{
				{"New team", "team-new", m.opts.ReadOnly},
				{"Open", "team-open", len(u.Teams) == 0},
			},
			2,
			y,
			width-4,
			-1,
		) + "\n",
	)
	y++
	b.WriteString(
		"  " + m.buttonRow(
			[]button{
				{"Edit", "team-edit", m.opts.ReadOnly || len(u.Teams) == 0},
				{"Duplicate", "team-duplicate", m.opts.ReadOnly || len(u.Teams) == 0},
				{"Delete", "team-delete", m.opts.ReadOnly || len(u.Teams) == 0},
				{"All sessions", "team-all", false},
				{"Refresh", "team-list", false},
			},
			2,
			y,
			width-4,
			-1,
		) + "\n",
	)
	return b.String()
}

func relativeTime(t interface{ Unix() int64 }) string {
	// Kept in seconds so remote millisecond timestamps don't leak into UI labels.
	d := time.Now().Unix() - t.Unix()
	if d < 60 {
		return "just now"
	}
	if d < 3600 {
		return fmt.Sprintf("%dm ago", d/60)
	}
	if d < 86400 {
		return fmt.Sprintf("%dh ago", d/3600)
	}
	return fmt.Sprintf("%dd ago", d/86400)
}
