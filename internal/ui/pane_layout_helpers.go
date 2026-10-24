package ui

import (
	"github.com/charmbracelet/lipgloss"

	api "github.com/qivs/agent-deck/internal/opencode"
)

// PaneChrome is cell geometry within an outer pane rectangle, including its border.
type PaneChrome struct {
	Controls   [][]int
	ControlY   []int
	Transcript Rect
	Waiting    Rect
	FooterY    int
}

// PlanPaneChrome wraps complete button labels and reserves pinned requests before transcript rows.
func PlanPaneChrome(bounds Rect, labels []string, waitingLines int) PaneChrome {
	inner := max(0, bounds.W-2)
	plan := PaneChrome{FooterY: bounds.Y + max(0, bounds.H-2)}
	if inner == 0 || bounds.H < 3 {
		return plan
	}
	available := max(0, bounds.H-5)
	used := 0
	for i, label := range labels {
		width := lipgloss.Width("[" + label + "]")
		if width > inner {
			continue
		}
		if len(plan.Controls) == 0 || used+width > inner {
			if len(plan.Controls) >= available {
				break
			}
			plan.Controls = append(plan.Controls, nil)
			plan.ControlY = append(plan.ControlY, bounds.Y+2+len(plan.Controls)-1)
			used = 0
		}
		row := len(plan.Controls) - 1
		plan.Controls[row] = append(plan.Controls[row], i)
		used += width + 1
	}
	bodyY := bounds.Y + 3 + len(plan.Controls)
	bodyHeight := max(0, plan.FooterY-bodyY)
	waiting := min(max(0, waitingLines), bodyHeight)
	plan.Waiting = Rect{X: bounds.X + 1, Y: bodyY, W: inner, H: waiting}
	plan.Transcript = Rect{X: bounds.X + 1, Y: bodyY + waiting, W: inner, H: bodyHeight - waiting}
	return plan
}

// PaneRequestSnapshot contains requests from one server/directory/session scope.
type PaneRequestSnapshot struct {
	Server      string
	Directory   string
	Session     string
	Permissions []api.Permission
	Questions   []api.Question
}

// PaneRequestCounts separates waiting sessions from individual permission and question requests.
type PaneRequestCounts struct {
	Sessions    int
	Permissions int
	Questions   int
}

func CountPaneRequests(snapshots []PaneRequestSnapshot) PaneRequestCounts {
	requests := map[[5]string]bool{}
	sessions := map[[3]string]bool{}
	counts := PaneRequestCounts{}
	for i := range snapshots {
		s := &snapshots[i]
		if s.Session == "" {
			continue
		}
		add := func(kind, id, session string) {
			key := [5]string{s.Server, s.Directory, s.Session, kind, id}
			if id == "" || session != s.Session || requests[key] {
				return
			}
			requests[key] = true
			sessions[[3]string{s.Server, s.Directory, s.Session}] = true
			if kind == "permission" {
				counts.Permissions++
			} else {
				counts.Questions++
			}
		}
		for _, permission := range s.Permissions {
			add("permission", permission.ID, permission.SessionID)
		}
		for _, question := range s.Questions {
			add("question", question.ID, question.SessionID)
		}
	}
	counts.Sessions = len(sessions)
	return counts
}
