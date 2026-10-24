package ui

import "strings"

func panePlan(bounds Rect, controls []button) PaneChrome {
	labels := make([]string, len(controls))
	for i := range controls {
		labels[i] = controls[i].Label
	}
	return PlanPaneChrome(bounds, labels, 0)
}

func (m *Model) paneControlRows(controls []button, plan *PaneChrome, x, width, index int) string {
	rows := make([]string, 0, len(plan.Controls))
	for i, indices := range plan.Controls {
		buttons := make([]button, 0, len(indices))
		for _, n := range indices {
			buttons = append(buttons, controls[n])
		}
		rows = append(rows, m.buttonRow(buttons, x+1, plan.ControlY[i], width, index))
	}
	return strings.Join(rows, "\n")
}

func (m *Model) requestCounts() PaneRequestCounts {
	snapshots := make([]PaneRequestSnapshot, 0, len(m.panes))
	for i := range m.panes {
		p := &m.panes[i]
		snapshots = append(snapshots, PaneRequestSnapshot{
			Server: m.deck.Server, Directory: p.Agent.Dir, Session: p.Agent.Session,
			Permissions: p.State.Permissions, Questions: p.State.Questions,
		})
	}
	return CountPaneRequests(snapshots)
}
