package ui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

type reloadIdentity struct{ server, directory, session string }

func unwrapReload(msg tea.Msg) tea.Msg {
	if result, ok := msg.(TeamReloadResult); ok {
		if !result.Current() {
			return nil
		}
		return result.Message
	}
	return msg
}

func paneReloadIdentity(server string, p *pane) reloadIdentity {
	return reloadIdentity{server, p.Agent.Dir, p.Agent.Session}
}

// ReconcileTeamReload copies display state only between identical server/directory/session bindings.
func ReconcileTeamReload(previous, next *Model) {
	if previous == nil || next == nil {
		return
	}
	old := make(map[reloadIdentity]int, len(previous.panes))
	for i := range previous.panes {
		identity := paneReloadIdentity(previous.deck.Server, &previous.panes[i])
		if identity.session != "" {
			old[identity] = i
		}
	}
	selected := reloadIdentity{}
	if previous.focus >= 0 && previous.focus < len(previous.panes) {
		selected = paneReloadIdentity(previous.deck.Server, &previous.panes[previous.focus])
	}
	for i := range next.panes {
		identity := paneReloadIdentity(next.deck.Server, &next.panes[i])
		if index, ok := old[identity]; ok {
			agent := next.panes[i].Agent
			next.panes[i] = previous.panes[index]
			next.panes[i].Agent = agent
			next.panes[i].Loading = false
			next.panes[i].Pending = nil
		}
		if identity == selected && identity.session != "" {
			next.focus, next.zoom = i, previous.zoom
		}
	}
	next.w, next.h = previous.w, previous.h
	next.showReasoning = previous.showReasoning
}

// TeamReloadResult associates asynchronous application results with their originating context.
type TeamReloadResult struct {
	Message tea.Msg
	active  func() bool
}

// GuardTeamReload wraps leaf I/O commands, not Bubble Tea runtime commands or batches.
func GuardTeamReload(ctx context.Context, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		msg := cmd()
		if ctx.Err() != nil || msg == nil {
			return nil
		}
		if _, guarded := msg.(TeamReloadResult); guarded {
			return msg
		}
		return TeamReloadResult{Message: msg, active: func() bool { return ctx.Err() == nil }}
	}
}

func (r TeamReloadResult) Current() bool {
	return r.active != nil && r.active()
}
