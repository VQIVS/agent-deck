package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

// KanbanEdit is a captured user mutation paired with the board revision shown to the user.
type KanbanEdit struct {
	Intent   KanbanIntent
	Revision uint64
	Task     *team.Task
	Comment  string
}

// KanbanSaved distinguishes durable task state from optional notification errors.
type KanbanSaved struct {
	Board     team.TaskBoard
	TaskID    string
	NotifyErr error
	Notices   []team.TaskNotice
}

// KanbanExecutor validates trusted interactive mutations and persists optimistic revisions.
type KanbanExecutor struct {
	Store    team.BoardStore
	Team     *team.Team
	ReadOnly bool
	Notify   func(context.Context, []team.TaskNotice) error
}

func (e *KanbanExecutor) Apply(ctx context.Context, edit *KanbanEdit, now time.Time) (KanbanSaved, error) {
	if edit == nil || e.Team == nil || e.ReadOnly || e.Store.ReadOnly {
		return KanbanSaved{}, fmt.Errorf("read-only or missing Kanban edit")
	}
	state, err := e.Store.Load(ctx)
	if err != nil && (!errors.Is(err, os.ErrNotExist) || edit.Revision != 0) {
		return KanbanSaved{}, fmt.Errorf("load Kanban board: %w", err)
	}
	if state.Revision != edit.Revision {
		return KanbanSaved{}, fmt.Errorf("board changed; reload task before saving")
	}
	baseline, err := kanbanEditBaseline(&state.Board, edit)
	if err != nil {
		return KanbanSaved{}, err
	}
	board, notices, err := team.ChangeTasks(ctx, e.Store, e.Team, team.TaskActor{User: true}, now,
		e.change(edit, baseline))
	if err != nil {
		return KanbanSaved{}, fmt.Errorf("change Kanban task: %w", err)
	}
	id := edit.Intent.TaskID
	if edit.Intent.Action == "create" && edit.Task != nil {
		id = edit.Task.ID
	}
	result := KanbanSaved{Board: board, TaskID: id, Notices: notices}
	if e.Notify != nil {
		result.NotifyErr = e.Notify(ctx, notices)
	}
	return result, nil
}

func (e *KanbanExecutor) change(edit *KanbanEdit, baseline string) func(*team.TaskBoard, team.TaskMutation) error {
	return func(board *team.TaskBoard, mutation team.TaskMutation) error {
		current, err := kanbanEditBaseline(board, edit)
		if err != nil {
			return err
		}
		if current != baseline {
			return fmt.Errorf("task changed; reload before saving")
		}
		return e.mutate(board, edit, mutation)
	}
}

func kanbanEditBaseline(board *team.TaskBoard, edit *KanbanEdit) (string, error) {
	if edit.Intent.Action == "create" {
		return "", nil
	}
	task := kanbanTask(board.Tasks, edit.Intent.TaskID)
	if task == nil {
		return "", fmt.Errorf("task no longer exists")
	}
	data, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("capture task snapshot: %w", err)
	}
	return string(data), nil
}

func (e *KanbanExecutor) mutate(board *team.TaskBoard, edit *KanbanEdit, m team.TaskMutation) error {
	id := edit.Intent.TaskID
	switch edit.Intent.Action {
	case "review":
		return kanbanMutationError("review", board.ReviewTask(id, edit.Intent.State, edit.Comment, m))
	case "create":
		if edit.Task == nil {
			return fmt.Errorf("create requires task form")
		}
		if err := e.validateMembers(edit.Task.Assignee, edit.Task.Reviewer); err != nil {
			return err
		}
		return kanbanMutationError("create", board.CreateTask(edit.Task, m))
	case "edit":
		return e.update(board, edit, m)
	case "move":
		return kanbanMutationError("move", board.MoveTask(id, edit.Intent.State, m))
	case "comment":
		return kanbanMutationError("comment", board.CommentTask(id, edit.Comment, m))
	case "assign":
		if err := e.validateMembers(edit.Intent.Assignee, ""); err != nil {
			return err
		}
		return kanbanMutationError("assign", board.AssignTask(id, edit.Intent.Assignee, "", m))
	default:
		return fmt.Errorf("unsupported Kanban mutation %q", edit.Intent.Action)
	}
}

func (e *KanbanExecutor) update(board *team.TaskBoard, edit *KanbanEdit, m team.TaskMutation) error {
	current := kanbanTask(board.Tasks, edit.Intent.TaskID)
	if current == nil || edit.Task == nil {
		return fmt.Errorf("edited task no longer exists")
	}
	if err := e.validateMembers(edit.Task.Assignee, edit.Task.Reviewer); err != nil {
		return err
	}
	replacement := *edit.Task
	desired, previous := replacement.Assignee, current.Assignee
	replacement.Assignee, replacement.Worker = current.Assignee, current.Worker
	if err := board.UpdateTask(current.ID, &replacement, m); err != nil {
		return fmt.Errorf("update Kanban task: %w", err)
	}
	if desired != previous {
		return kanbanMutationError("assign", board.AssignTask(edit.Intent.TaskID, desired, "", m))
	}
	return nil
}

func (e *KanbanExecutor) validateMembers(assignee, reviewer string) error {
	for _, name := range []string{assignee, reviewer} {
		found := name == ""
		for i := range e.Team.Members {
			if e.Team.Members[i].Name == name {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("unknown task member %q", name)
		}
	}
	return nil
}

func (m *Model) KanbanWorkerPane(task *team.Task, members []team.Member) (int, error) {
	if task == nil {
		return -1, fmt.Errorf("task no longer exists")
	}
	for i := range members {
		member := &members[i]
		if member.Name != kanbanAssignee(task) || member.Session == "" {
			continue
		}
		for j := range m.panes {
			a := m.panes[j].Agent
			if a.Name == member.Name && a.Session == member.Session && a.Dir == member.SessionDir() {
				return j, nil
			}
		}
	}
	return -1, fmt.Errorf("assignee has no matching current session pane")
}

func kanbanMutationError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s Kanban task: %w", action, err)
}
