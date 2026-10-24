package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/team"
)

// TeamOverviewRow describes one configured member, including members without live sessions.
type TeamOverviewRow struct {
	Index       int
	Identity    InboxIdentity
	Role        string
	Status      string
	Leases      []string
	Permissions int
	Questions   int
	Policy      string
	Task        string
}

func (m *Model) teamOverviewRows(t *team.Team) []TeamOverviewRow {
	if t == nil {
		return nil
	}
	rows := make([]TeamOverviewRow, 0, len(t.Members))
	for i := range t.Members {
		member := &t.Members[i]
		row := TeamOverviewRow{
			Index: i, Role: member.Role, Status: "NOT STARTED",
			Identity: InboxIdentity{Agent: member.Name, Directory: member.SessionDir(), Session: member.Session},
			Leases:   append([]string(nil), member.Leases...),
			Policy:   member.PermissionSummary(),
			Task:     m.activeTask(member.Name),
		}
		if member.Session != "" {
			row.Status = "OFFLINE"
		}
		for j := range m.panes {
			p := &m.panes[j]
			if t.Server == m.deck.Server && p.Agent.Session == member.Session && p.Agent.Dir == member.SessionDir() &&
				member.Session != "" {
				row.Status = p.State.DisplayStatus()
				row.Permissions, row.Questions = len(p.State.Permissions), len(p.State.Questions)
				break
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func RenderTeamOverviewRow(row *TeamOverviewRow, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = "› "
	}
	alert := row.Status
	if row.Permissions > 0 || row.Questions > 0 {
		alert = fmt.Sprintf("! NEEDS PERMISSION / INPUT · %d permission · %d question", row.Permissions, row.Questions)
	}
	leases := strings.Join(row.Leases, ", ")
	if leases == "" {
		leases = "none"
	}
	lines := []string{
		fmt.Sprintf("%s%s · %s · %s", marker, alert,
			lipgloss.NewStyle().Foreground(MemberColor(row.Identity.Agent)).Render(row.Identity.Agent), row.Role),
		"  leases: " + leases + overviewPolicy(row.Policy) + overviewTask(row.Task),
		"  " + row.Identity.Directory + " · " + row.Identity.Session,
	}
	for i := range lines {
		if width <= 0 {
			lines[i] = ""
		} else {
			lines[i] = ansi.Truncate(inboxLine(lines[i]), width, "…")
		}
	}
	return strings.Join(lines, "\n")
}

func overviewTask(task string) string {
	if task == "" {
		return ""
	}
	return " · task: " + task
}

func overviewPolicy(policy string) string {
	if policy == "" {
		return ""
	}
	return " · " + policy
}

// TeamOverviewRects uses the same fixed three-line row height as RenderTeamOverviewRow.
func TeamOverviewRects(x, y, width, height, first, count int) []Rect {
	if width <= 0 || height < 3 || first < 0 || first >= count {
		return nil
	}
	visible := min(height/3, count-first)
	rows := make([]Rect, visible)
	for i := range rows {
		rows[i] = Rect{X: x, Y: y + i*3, W: width, H: 3}
	}
	return rows
}

// TeamOverviewViewport renders rows and returns hit targets using identical geometry.
func TeamOverviewViewport(rows []TeamOverviewRow, selected, first int, bounds Rect) (string, []zone) {
	first = max(0, first)
	rects := TeamOverviewRects(bounds.X, bounds.Y, bounds.W, bounds.H, first, len(rows))
	lines := make([]string, 0, len(rects))
	zones := make([]zone, 0, len(rects))
	for i, rect := range rects {
		row := &rows[first+i]
		lines = append(lines, RenderTeamOverviewRow(row, row.Index == selected, rect.W))
		zones = append(zones, zone{Rect: rect, Action: fmt.Sprintf("member-row:%d", row.Index), Pane: -1})
	}
	return strings.Join(lines, "\n"), zones
}

// TeamOverviewRequests counts outstanding requests, including off-screen members.
func TeamOverviewRequests(rows []TeamOverviewRow) (permissions, questions int) {
	for i := range rows {
		permissions += rows[i].Permissions
		questions += rows[i].Questions
	}
	return permissions, questions
}
