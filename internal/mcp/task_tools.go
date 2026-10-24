package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

// TaskTools exposes the workflow board to authenticated members; authority comes from the roster.
type TaskTools struct {
	Provider PoolProvider
	Notify   func(context.Context, []team.TaskNotice) error
	Now      func() time.Time
}

var taskToolNames = []string{
	"list_tasks", "create_task", "update_task", "split_task", "set_assignee", "move_task",
	"comment_task", "review_task", "my_tasks", "update_my_task", "answer_request",
}

// IsTaskTool reports whether a tool name belongs to the workflow board.
func IsTaskTool(name string) bool { return slices.Contains(taskToolNames, name) }

func (t *TaskTools) Definitions() []map[string]any {
	text := map[string]any{"type": "string"}
	list := map[string]any{"type": "array", "items": text}
	task := map[string]any{
		"title": text, "description": text, "criteria": list, "paths": list, "dependencies": list,
		"priority": text, "labels": list, "estimate": text, "tokens": map[string]any{"type": "integer"},
		"assignee": text, "reviewer": text, "ready": map[string]any{"type": "boolean"},
		"parallel": map[string]any{"type": "boolean"},
	}
	withID := map[string]any{"id": text}
	for key, value := range task {
		withID[key] = value
	}
	parts := map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": task}}
	return []map[string]any{
		tool("list_tasks", "List board tasks, optionally filtered by state or assignee",
			map[string]any{"state": text, "assignee": text}, nil),
		tool("create_task", "Master: create a task (backlog, or ready when ready=true); criteria are "+
			"checkable acceptance items, paths are repo-relative owned globs", task, []string{"title", "criteria", "paths"}),
		tool("update_task", "Master: edit a task's details (not state or assignee)", withID, []string{"id"}),
		tool("split_task", "Master: replace a task with smaller tasks; the original becomes "+team.TaskCancelled,
			map[string]any{"id": text, "parts": parts}, []string{"id", "parts"}),
		tool("set_assignee", "Master: assign a worker and a reviewer; the assignee receives a generated brief",
			map[string]any{"id": text, "assignee": text, "reviewer": text}, []string{"id", "assignee"}),
		tool("move_task", "Move a task to backlog, ready, in_progress, review, blocked, done or "+team.TaskCancelled,
			map[string]any{"id": text, "state": text}, []string{"id", "state"}),
		tool("comment_task", "Comment on a task you manage, work on or review",
			map[string]any{"id": text, "text": text}, []string{"id", "text"}),
		tool("review_task", "Reviewer: accept a task or request changes, with a comment",
			map[string]any{"id": text, "decision": text, "comment": text}, []string{"id", "decision", "comment"}),
		tool("my_tasks", "Tasks assigned to you or waiting for your review", nil, nil),
		tool("answer_request", "Master: answer a worker's permission request (reply once, always or reject)",
			map[string]any{"id": text, "reply": text, "message": text}, []string{"id", "reply"}),
		tool("update_my_task", "Worker: tick criteria (checked = criterion IDs), comment, or move your task "+
			"to review, blocked or in_progress", map[string]any{"id": text, "checked": list, "comment": text, "state": text},
			[]string{"id"}),
	}
}

func (t *TaskTools) Call(ctx context.Context, identity Identity, name string, arguments json.RawMessage) (any, error) {
	if name == "answer_request" {
		return t.answerRequest(ctx, identity, arguments)
	}
	var call team.WorkflowCall
	if err := decode(bytes.NewReader(arguments), &call); err != nil {
		return nil, err
	}
	manager, store, err := t.Provider(ctx)
	if err != nil {
		return nil, fmt.Errorf("open task board: %w", err)
	}
	store.ReadOnly = store.ReadOnly || manager.ReadOnly
	actor, err := manager.Team.ResolveTaskActor(identity.Member)
	if err != nil {
		return nil, fmt.Errorf("resolve task actor: %w", err)
	}
	if name == "list_tasks" || name == "my_tasks" {
		return readTasks(ctx, store, name, identity.Member, &call)
	}
	change, err := team.WorkflowChange(name, &call)
	if err != nil {
		return nil, fmt.Errorf("select task change: %w", err)
	}
	if name == "review_task" {
		change = func(board *team.TaskBoard, m team.TaskMutation) error {
			return team.ReviewTaskForTeam(board, &manager.Team, call.ID, identity.Member, call.Decision, call.Comment,
				false, m.Now, m.ReadOnly)
		}
	}
	board, notices, err := team.ChangeTasks(ctx, store, &manager.Team, actor, t.now(), change)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	result := taskResult{Tasks: changedTasks(&board, &call)}
	if t.Notify != nil && len(notices) > 0 {
		if err := t.Notify(ctx, notices); err != nil {
			result.Warning = "saved, but notifying members failed: " + err.Error()
		}
	}
	return result, nil
}

func (t *TaskTools) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func readTasks(ctx context.Context, store team.BoardStore, name, member string, call *team.WorkflowCall) (any, error) {
	state, err := store.Load(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return []taskSummary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task board: %w", err)
	}
	return listTasks(&state.Board, name, member, call), nil
}
