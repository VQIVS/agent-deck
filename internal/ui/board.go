package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

type (
	boardUI struct {
		Open      bool
		State     team.BoardState
		Actions   []team.ReconcileAction
		Index     int
		Confirm   *team.ReconcileAction
		Kanban    KanbanScreen
		Targets   []KanbanTarget
		Form      *KanbanForm
		Approvals bool
		Saving    bool
		Loading   bool
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
	if m.teams == nil || m.teams.Manager == nil || m.board.Loading {
		return nil
	}
	m.board.Loading = true
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
		if snapshot.opts.ReadOnly {
			return boardResult{State: state}
		}
		actor := team.ReconcileActor{User: true}
		actions, err := pool.Pending(snapshot.context(), actor)
		return boardResult{State: state, Actions: actions, Err: err}
	}
}

func (m *Model) boardView() string {
	if m.board.Confirm != nil {
		lines := append([]string{"Confirm pool approval · y apply · any other key cancel"},
			boardApprovalDetails(m.board.Confirm)...)
		return boardLines(lines, m.w, m.h)
	}
	if !m.board.Approvals {
		return m.kanbanBoardView()
	}
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
	if m.board.Index >= 0 && m.board.Index < len(m.board.Actions) {
		lines = append(lines, boardApprovalDetails(&m.board.Actions[m.board.Index])...)
	}
	return boardLines(lines, m.w, m.h)
}

func boardLines(lines []string, width, height int) string {
	for i := range lines {
		lines[i] = RenderInboxLine(lines[i], width)
	}
	return strings.Join(lines[:min(len(lines), max(0, height))], "\n")
}

func boardApprovalDetails(action *team.ReconcileAction) []string {
	task, role, model, paths := action.Task, "unspecified", "team default", "none"
	reason := "Apply desired " + action.Kind + " operation"
	if p := action.Proposal; p != nil {
		task, role, paths = p.Task, p.Role, strings.Join(p.Paths, ", ")
		if p.Model != "" {
			model = p.Model
		}
		reason = "Add capacity for ready task"
		if p.Task == "" {
			reason = "Satisfy minimum worker count"
		}
	}
	return []string{
		"Action: " + action.ID, "Worker: " + action.Worker, "Task: " + task, "Paths: " + paths,
		"Role: " + role + " · Model: " + model, "Reason: " + reason,
	}
}

func (m *Model) boardKey(v tea.KeyMsg) tea.Cmd {
	if m.board.Confirm != nil {
		action := m.board.Confirm
		m.board.Confirm = nil
		if v.String() == "y" {
			return m.confirmBoard(action)
		}
		return nil
	}
	if v.String() == "y" && len(m.board.Actions) > 0 && m.board.Form == nil && !m.board.Kanban.searching {
		return m.approveBoard()
	}
	if !m.board.Approvals {
		return m.kanbanBoardKey(v)
	}
	switch v.String() {
	case "p":
		m.board.Approvals = false
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
	action := m.board.Actions[max(0, min(m.board.Index, len(m.board.Actions)-1))]
	if action.Proposal != nil {
		proposal := *action.Proposal
		proposal.Paths = append(team.Leases(nil), proposal.Paths...)
		proposal.Skills = append([]string(nil), proposal.Skills...)
		action.Proposal = &proposal
	}
	m.board.Confirm = &action
	return nil
}

func (m *Model) confirmBoard(action *team.ReconcileAction) tea.Cmd {
	if m.opts.ReadOnly {
		return nil
	}
	pending := false
	for i := range m.board.Actions {
		if m.board.Actions[i].ID == action.ID {
			pending = true
		}
	}
	if !pending {
		m.note = "Pool action is no longer pending"
		return nil
	}
	id := action.ID
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
