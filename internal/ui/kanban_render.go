package ui

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/team"
)

func RenderKanban(tasks []team.Task, screen *KanbanScreen, opts *KanbanOptions) (string, []KanbanTarget) {
	if opts.Width <= 0 || opts.Height <= 0 {
		return "", nil
	}
	task := screen.reconcile(tasks)
	if screen.Detail && task != nil {
		return renderKanbanDetail(task, screen, opts)
	}
	states := KanbanStates()
	first, widths := kanbanColumnWidths(tasks, screen, opts.Width)
	views := make([]string, 0, len(widths))
	var targets []KanbanTarget
	height := max(0, opts.Height-3)
	x := 0
	for i, width := range widths {
		view, zones := renderKanbanColumn(tasks, screen, opts, states[first+i], Rect{X: x, Y: 1, W: width, H: height})
		views = append(views, view)
		targets = append(targets, zones...)
		x += width
	}
	header := "Tasks · " + screen.Column + " · / filter · s swimlanes"
	if screen.Filter.Query != "" || screen.searching {
		header += " · search: " + inboxLine(screen.Filter.Query)
	}
	if screen.Filter.Assignee != "" {
		header += " · @" + inboxLine(screen.Filter.Assignee)
	}
	if screen.Filter.Label != "" {
		header += " · #" + inboxLine(screen.Filter.Label)
	}
	if screen.Filter.State != "" {
		header += " · state:" + screen.Filter.State
	}
	footer := "←→ columns · ↑↓ cards · Enter detail · m move · n new · e edit · w worker"
	if screen.Move {
		footer = "Move: 1 backlog · 2 ready · 3 in_progress · 4 review · 5 done · 6 blocked · 7 cancel"
	}
	filterHelp := "f assignee · t label · z state · 0 clear"
	if opts.ReadOnly {
		footer = "READ ONLY · ←→ columns · ↑↓ cards · Enter detail · w worker"
	}
	lines := []string{
		kanbanFit(header, opts.Width), lipgloss.JoinHorizontal(lipgloss.Top, views...),
		kanbanFit(footer, opts.Width), kanbanFit(filterHelp, opts.Width),
	}
	return kanbanBound(strings.Join(lines, "\n"), opts.Width, opts.Height), targets
}

func renderKanbanColumn(tasks []team.Task, screen *KanbanScreen, opts *KanbanOptions,
	state string, bounds Rect,
) (string, []KanbanTarget) {
	items := kanbanTasks(tasks, screen.Filter, state, screen.Swimlanes)
	width := max(1, bounds.W-1)
	color := kanbanStateColor(state)
	label := state
	if len(items) == 0 && bounds.W <= 12 {
		label = map[string]string{
			"backlog": "backlog", "ready": "ready", "in_progress": "progress",
			"review": "review", "done": "done", "blocked": "blocked", team.TaskCancelled: "cancel",
		}[state]
	}
	pill := lipgloss.NewStyle().Foreground(lipgloss.Color("#141619")).Background(color).Bold(true).
		Render(" " + fmt.Sprintf("%s %d", label, len(items)) + " ")
	lines := []string{kanbanFit(pill, width)}
	var targets []KanbanTarget
	start := 0
	capacity := max(1, (bounds.H-1)/8)
	for i, task := range items {
		if task.ID == screen.Selected {
			start = max(0, i-capacity+1)
		}
	}
	lane := ""
	for _, task := range items[start:] {
		if screen.Swimlanes && lane != kanbanAssignee(task) {
			lane = kanbanAssignee(task)
			lines = append(lines, kanbanFit("── "+inboxLine(kanbanOwner(lane))+" ──", width))
		}
		if len(lines)+8 > bounds.H {
			break
		}
		y := bounds.Y + len(lines)
		lines = append(lines, kanbanCard(task, screen.Selected == task.ID, opts, width)...)
		targets = append(targets, KanbanTarget{
			Rect: Rect{X: bounds.X, Y: y, W: width, H: 8}, TaskID: task.ID, Action: "select", State: task.State,
		})
		if !opts.ReadOnly {
			for n, action := range []string{"previous", "next"} {
				targets = append(targets, KanbanTarget{
					Rect:   Rect{X: bounds.X + 1 + n*4, Y: y + 6, W: 3, H: 1},
					TaskID: task.ID, Action: action, State: task.State,
				})
			}
		}
	}
	for len(lines) < bounds.H {
		lines = append(lines, "")
	}
	if bounds.H == 0 {
		return "", nil
	}
	for i := range lines {
		lines[i] = lipgloss.NewStyle().Width(bounds.W).Render(kanbanFit(lines[i], width))
	}
	return strings.Join(lines[:min(len(lines), bounds.H)], "\n"), targets
}

func kanbanCard(task *team.Task, selected bool, opts *KanbanOptions, width int) []string {
	checked := 0
	for _, criterion := range task.Criteria {
		if criterion.Checked {
			checked++
		}
	}
	deps := ""
	if len(task.Dependencies) > 0 {
		deps = fmt.Sprintf("deps %d", len(task.Dependencies))
	}
	if task.State == "blocked" {
		deps += " BLOCKED"
	}
	priority := task.Priority
	if priority == "" {
		priority = "normal"
	}
	owner := kanbanAssignee(task)
	ownerLabel := kanbanOwner(owner)
	if task.State == "ready" && owner != "" {
		ownerLabel = "waiting for " + owner
	}
	badge := lipgloss.NewStyle().Foreground(kanbanMemberColor(owner, opts)).
		Render("[" + inboxLine(ownerLabel) + "]")
	bar := kanbanProgress(checked, len(task.Criteria))
	priority = map[string]string{"urgent": "▲ urgent", "high": "▲ high", "normal": "• normal", "low": "▽ low"}[priority]
	alert := lipgloss.NewStyle().Foreground(opts.Theme.Danger).Render(strings.TrimSpace(deps))
	inner := max(1, width-2)
	lines := []string{
		inboxLine(task.ID + " " + task.Title), badge + " · " + priority,
		bar + " · " + kanbanAge(task.CreatedAt, opts.Now), inboxLine(strings.Join(task.Labels, " · ")), alert, "[<] [>]",
	}
	for i := range lines {
		lines[i] = kanbanFit(lines[i], inner)
	}
	border, color := lipgloss.RoundedBorder(), opts.Theme.Border
	if selected {
		border, color = lipgloss.ThickBorder(), opts.Theme.Accent
	}
	box := lipgloss.NewStyle().Width(inner).Border(border).BorderForeground(color).
		Background(opts.Theme.Surface).Render(strings.Join(lines, "\n"))
	return strings.Split(box, "\n")
}

func kanbanProgress(checked, total int) string {
	filled := 0
	if total > 0 {
		filled = checked * 3 / total
	}
	return strings.Repeat("■", filled) + strings.Repeat("□", 3-filled) + fmt.Sprintf(" %d/%d", checked, total)
}

func kanbanStateColor(state string) lipgloss.Color {
	return map[string]lipgloss.Color{
		"backlog": "#929BAA", "ready": "#7DB6F5", "in_progress": "#E5BB79",
		"review": "#B7A3F5", "done": "#89C9A0", "blocked": "#EF9393", team.TaskCancelled: "#657080",
	}[state]
}

func kanbanOwner(owner string) string {
	if owner == "" {
		return "unassigned"
	}
	return owner
}

func kanbanMemberColor(owner string, opts *KanbanOptions) lipgloss.Color {
	if color, ok := opts.MemberColors[owner]; ok {
		return color
	}
	return MemberColor(owner)
}

// MemberColor is a stable identity color shared by cards, member rows and session panes.
func MemberColor(identity string) lipgloss.Color {
	colors := []lipgloss.Color{"#B7A3F5", "#89C9A0", "#E5BB79", "#DDA2CD", "#7DB6F5", "#EF9393"}
	h := fnv.New32a()
	_, _ = h.Write([]byte(identity))
	return colors[int(h.Sum32())%len(colors)]
}

func kanbanAge(created, now time.Time) string {
	if created.IsZero() || now.IsZero() {
		return "age unknown"
	}
	d := max(time.Duration(0), now.Sub(created))
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

func kanbanFit(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(strings.ReplaceAll(text, "\n", " "), width, "…")
}

func kanbanBound(text string, width, height int) string {
	lines := strings.Split(text, "\n")
	lines = lines[:min(len(lines), max(0, height))]
	for i := range lines {
		lines[i] = kanbanFit(lines[i], width)
	}
	return strings.Join(lines, "\n")
}
