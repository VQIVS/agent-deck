package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qivs/agent-deck/internal/server"
	"github.com/qivs/agent-deck/internal/team"
)

type (
	boardUI struct {
		Open    bool
		State   team.BoardState
		Actions []team.ReconcileAction
		Index   int
	}
	boardResult struct {
		State   team.BoardState
		Actions []team.ReconcileAction
		Err     error
	}
)

func (m *Model) pool(ctx context.Context) (*team.PoolRuntime, error) {
	root, err := server.StateRoot()
	if err != nil {
		return nil, fmt.Errorf("pool state: %w", err)
	}
	if m.teams == nil || m.teams.Manager == nil {
		return nil, fmt.Errorf("select a team first")
	}
	pool, err := team.OpenPool(ctx, m.teams.Store, m.teams.Manager.Team.Name,
		team.BoardStore{Root: filepath.Join(root, "boards"), Team: m.teams.Manager.Team.Name, ReadOnly: m.opts.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("open board: %w", err)
	}
	return pool, nil
}

func (m *Model) poolInboxEntries() []InboxEntry {
	entries := make([]InboxEntry, 0, len(m.board.Actions))
	for i := range m.board.Actions {
		action := &m.board.Actions[i]
		entries = append(entries, InboxEntry{
			ID: "pool:" + action.ID, Kind: "pool", RequestID: action.ID,
			Title: "Pool approval: " + action.Kind + " " + action.Worker, Detail: "Open work board to approve or reject",
		})
	}
	return entries
}

func (m *Model) readBoard() tea.Cmd {
	if m.teams == nil || m.teams.Manager == nil {
		return nil
	}
	snapshot := *m
	return func() tea.Msg {
		pool, err := snapshot.pool(snapshot.context())
		if err != nil {
			return boardResult{Err: err}
		}
		state, err := pool.Status(snapshot.context())
		if err != nil {
			return boardResult{Err: err}
		}
		actor := team.ReconcileActor{User: true}
		actions, err := pool.Pending(snapshot.context(), actor)
		return boardResult{State: state, Actions: actions, Err: err}
	}
}

func (m *Model) boardView() string {
	lines := make([]string, 0, 1+len(m.board.State.Board.Tasks)+len(m.board.Actions))
	lines = append(lines, "Work board · Esc back · y approve · x reject · r refresh")
	for i := range m.board.State.Board.Tasks {
		task := &m.board.State.Board.Tasks[i]
		lines = append(lines, fmt.Sprintf("%s · %s · %s · %s", task.ID, task.State, task.Worker, task.Title))
	}
	for i := range m.board.Actions {
		action := &m.board.Actions[i]
		mark := "  "
		if i == m.board.Index {
			mark = "> "
		}
		lines = append(lines, mark+action.Kind+" "+action.Worker+" · "+action.ID)
	}
	for i := range lines {
		lines[i] = fit(lines[i], m.w)
	}
	return strings.Join(lines[:min(len(lines), m.h)], "\n")
}

func (m *Model) boardKey(v tea.KeyMsg) tea.Cmd {
	switch v.String() {
	case "esc", "b":
		m.board.Open = false
	case "up":
		m.board.Index = max(0, m.board.Index-1)
	case "down":
		m.board.Index = min(max(0, len(m.board.Actions)-1), m.board.Index+1)
	case "r":
		return m.readBoard()
	case "y":
		return m.approveBoard()
	case "x":
		return m.rejectBoard()
	}
	return nil
}

func (m *Model) rejectBoard() tea.Cmd {
	if m.opts.ReadOnly || len(m.board.Actions) == 0 {
		return nil
	}
	id := m.board.Actions[min(m.board.Index, len(m.board.Actions)-1)].ID
	snapshot := *m
	return func() tea.Msg {
		pool, err := snapshot.pool(snapshot.context())
		if err != nil {
			return actionMsg{Err: err}
		}
		err = pool.Reject(snapshot.context(), team.ReconcileActor{User: true}, id)
		return actionMsg{Err: err, Text: "Pool action rejected"}
	}
}

func (m *Model) approveBoard() tea.Cmd {
	if m.opts.ReadOnly || len(m.board.Actions) == 0 {
		return nil
	}
	id := m.board.Actions[min(m.board.Index, len(m.board.Actions)-1)].ID
	snapshot := *m
	return func() tea.Msg {
		pool, err := snapshot.pool(snapshot.context())
		if err != nil {
			return actionMsg{Err: err}
		}
		actor := team.ReconcileActor{User: true}
		if err := pool.Approve(snapshot.context(), actor, id); err != nil {
			return actionMsg{Err: err}
		}
		_, err = pool.Controller(actor).Apply(snapshot.context(), actor, id)
		return actionMsg{Err: err, Text: "Pool action applied"}
	}
}
