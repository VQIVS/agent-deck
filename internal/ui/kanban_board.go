package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

func (m *Model) kanbanBoardView() string {
	if form := m.board.Form; form != nil && form.Open {
		return m.kanbanFormView(form)
	}
	current := &team.Team{}
	if m.teams != nil && m.teams.Manager != nil {
		current = &m.teams.Manager.Team
	}
	view, targets := RenderKanbanReview(m.board.State.Board.Tasks, &m.board.Kanban, &KanbanOptions{
		Width: m.w, Height: max(1, m.h-1), Now: time.Now(), Theme: m.theme, ReadOnly: m.opts.ReadOnly,
		GitFacts:   m.board.Git,
		GitTargets: m.taskEvidenceTargets(current),
	}, current)
	m.board.Targets = targets
	m.zones = nil
	for i := range targets {
		m.zones = append(m.zones, zone{Rect: targets[i].Rect, Action: fmt.Sprintf("kanban:%d", i), Pane: -1})
	}
	return view + "\n" + fit("v review · p pool approvals · "+m.note, m.w)
}

func (m *Model) activeTask(member string) string {
	for i := range m.board.State.Board.Tasks {
		task := &m.board.State.Board.Tasks[i]
		if task.Assignee == member && (team.TaskActive(task) || task.State == "blocked") {
			return task.ID + " " + task.Title
		}
	}
	return ""
}

func (m *Model) kanbanBoardKey(v tea.KeyMsg) tea.Cmd {
	if form := m.board.Form; form != nil && form.Open {
		if edit := KanbanFormKey(form, v, m.opts.ReadOnly); edit != nil {
			return m.saveKanbanEdit(edit)
		}
		return nil
	}
	if m.board.Kanban.searching || m.board.Kanban.Move {
		return m.kanbanIntent(KanbanKey(m.board.State.Board.Tasks, &m.board.Kanban, v, m.opts.ReadOnly))
	}
	if v.String() == "p" {
		m.board.Approvals = true
		return nil
	}
	if v.String() == "v" && !m.opts.ReadOnly {
		task := kanbanTask(m.board.State.Board.Tasks, m.board.Kanban.Selected)
		if task != nil {
			m.board.Form = NewKanbanForm("review", task, m.board.State.Revision)
		}
		return nil
	}
	intent := KanbanKey(m.board.State.Board.Tasks, &m.board.Kanban, v, m.opts.ReadOnly)
	if v.String() == "enter" && m.board.Kanban.Detail {
		return m.readTaskGit()
	}
	return m.kanbanIntent(intent)
}

func (m *Model) kanbanIntent(intent *KanbanIntent) tea.Cmd {
	if intent == nil {
		return nil
	}
	switch intent.Action {
	case "close":
		m.board.Open = false
	case "refresh":
		return m.readBoard()
	case "create", "edit", "assign", "comment":
		if !m.opts.ReadOnly {
			task := kanbanTask(m.board.State.Board.Tasks, intent.TaskID)
			m.board.Form = NewKanbanForm(intent.Action, task, m.board.State.Revision)
		}
	case "move":
		return m.saveKanbanEdit(&KanbanEdit{Intent: *intent, Revision: m.board.State.Revision})
	case "worker":
		if m.teams == nil || m.teams.Manager == nil {
			return nil
		}
		index, err := m.KanbanWorkerPane(kanbanTask(m.board.State.Board.Tasks, intent.TaskID), m.teams.Manager.Team.Members)
		if err != nil {
			m.note = err.Error()
		} else {
			m.focus, m.zoom, m.board.Open = index, true, false
		}
	}
	return nil
}

func (m *Model) kanbanFormView(form *KanbanForm) string {
	lines := make([]string, 0, len(form.Fields)+2)
	lines = append(lines, "Task "+form.Action+" · Tab field · Ctrl+S save · Esc cancel")
	for i := range form.Fields {
		field := &form.Fields[i]
		prefix := "  "
		if i == form.Index {
			prefix = "> "
		}
		value := strings.ReplaceAll(field.Value, "\n", " ↵ ")
		lines = append(lines, prefix+field.Label+": "+value)
	}
	lines = append(lines, form.Error)
	for i := range lines {
		lines[i] = fit(lines[i], m.w)
	}
	return strings.Join(lines[:min(len(lines), m.h)], "\n")
}

func (m *Model) saveKanbanEdit(edit *KanbanEdit) tea.Cmd {
	if m.opts.ReadOnly || m.teams == nil || m.teams.Manager == nil || m.board.Saving {
		return nil
	}
	m.board.Saving = true
	t := m.teams.Manager.Team
	t.Members = append([]team.Member(nil), t.Members...)
	readOnly := m.opts.ReadOnly
	root, err := server.StateRoot()
	if err != nil {
		m.note = err.Error()
		m.board.Saving = false
		return nil
	}
	runtime := m.channel.Runtime
	ctx := m.context()
	return func() tea.Msg {
		executor := KanbanExecutor{
			Store: team.BoardStore{Root: filepath.Join(root, "boards"), Team: t.Name, ReadOnly: readOnly},
			Team:  &t, ReadOnly: readOnly,
			Notify: func(ctx context.Context, notices []team.TaskNotice) error {
				if len(notices) == 0 {
					return nil
				}
				notifier, ok := runtime.(interface {
					Notify(context.Context, []team.TaskNotice) error
				})
				if !ok {
					return fmt.Errorf("task saved; runtime notice delivery unavailable")
				}
				return notifier.Notify(ctx, notices)
			},
		}
		result, err := executor.Apply(ctx, edit, time.Now())
		return kanbanResult{Saved: result, Err: err}
	}
}

type kanbanResult struct {
	Saved KanbanSaved
	Err   error
}

func (m *Model) shouldReadBoard() bool {
	return m.teams != nil && m.teams.Manager != nil && m.frame%3 == 0 && !m.board.Saving
}

func (m *Model) applyKanbanResult(result *kanbanResult) tea.Cmd {
	m.board.Saving = false
	if result.Err != nil {
		m.note = result.Err.Error()
		if m.board.Form != nil {
			m.board.Form.Error = m.note
		}
		return nil
	}
	m.board.Form = nil
	if result.Saved.State.Revision >= m.board.State.Revision {
		m.board.State = result.Saved.State
	}
	m.board.Kanban.Selected = result.Saved.TaskID
	m.note = "Task saved"
	if result.Saved.NotifyErr != nil {
		m.note = "Task saved; notice error: " + result.Saved.NotifyErr.Error()
	}
	return m.readBoard()
}

func (m *Model) kanbanBoardClick(z zone, ok bool) tea.Cmd {
	if !ok || m.board.Approvals {
		return nil
	}
	index := -1
	_, _ = fmt.Sscanf(z.Action, "kanban:%d", &index)
	if index < 0 || index >= len(m.board.Targets) {
		return nil
	}
	intent := KanbanClickAt(m.board.State.Board.Tasks, &m.board.Kanban,
		&m.board.Targets[index], m.opts.ReadOnly, time.Now())
	if m.board.Kanban.Detail && intent == nil {
		return m.readTaskGit()
	}
	return m.kanbanIntent(intent)
}
