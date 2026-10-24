package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// TaskNotice is a message agent-deck delivers to one member after a board change.
type TaskNotice struct {
	To   string
	Task string
	Text string
}

const taskChangeAttempts = 3

// TaskActorFor derives workflow authority from the current roster, never from caller-supplied roles.
func TaskActorFor(t *Team, name string) (TaskActor, error) {
	for i := range t.Members {
		member := &t.Members[i]
		if member.Name == name {
			if member.Role != "master" && member.Role != "worker" {
				return TaskActor{}, fmt.Errorf("member %q has no workflow role", name)
			}
			return TaskActor{Member: member.Name, Role: member.Role}, nil
		}
	}
	return TaskActor{}, fmt.Errorf("member %q is not on the current roster", name)
}

// ChangeTasks applies one workflow change to the persisted board with optimistic retries
// and returns the saved board plus the notices the change implies.
func ChangeTasks(ctx context.Context, store BoardStore, t *Team, actor TaskActor, now time.Time,
	change func(*TaskBoard, TaskMutation) error,
) (TaskBoard, []TaskNotice, error) {
	mutation := TaskMutation{Actor: actor, Now: now.UTC(), ReadOnly: store.ReadOnly}
	var err error
	for range taskChangeAttempts {
		var state BoardState
		state, err = loadWorkflowBoard(ctx, store)
		if err != nil {
			return TaskBoard{}, nil, err
		}
		before := cloneBoard(&state.Board)
		if err := change(&state.Board, mutation); err != nil {
			return TaskBoard{}, nil, err
		}
		err = store.Save(ctx, &state, state.Revision)
		if err == nil {
			return state.Board, TaskNotices(&before, &state.Board, t, actor), nil
		}
		if !errors.Is(err, ErrBoardChanged) {
			return TaskBoard{}, nil, err
		}
	}
	return TaskBoard{}, nil, fmt.Errorf("save task change: %w", err)
}

func loadWorkflowBoard(ctx context.Context, store BoardStore) (BoardState, error) {
	state, err := store.Load(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return BoardState{
			Version: 2, Board: TaskBoard{Version: 2},
			Policy: SchedulingPolicy{Approval: "off"}, Approved: map[string]bool{},
		}, nil
	}
	if err != nil {
		return state, err
	}
	if state.Board.Version != 2 {
		return state, fmt.Errorf("workflow tools need a version 2 board; migrate it with agent-deck first")
	}
	return state, nil
}

func cloneBoard(board *TaskBoard) TaskBoard {
	clone := TaskBoard{Version: board.Version, History: slices.Clone(board.History), Tasks: make([]Task, len(board.Tasks))}
	for i := range board.Tasks {
		clone.Tasks[i] = cloneTask(&board.Tasks[i])
	}
	return clone
}

// TaskNotices compares two boards and tells assignees, reviewers and the master what changed.
func TaskNotices(before, after *TaskBoard, t *Team, actor TaskActor) []TaskNotice {
	previous := map[string]*Task{}
	for i := range before.Tasks {
		previous[before.Tasks[i].ID] = &before.Tasks[i]
	}
	master := teamMaster(t)
	notices := make([]TaskNotice, 0, len(after.Tasks))
	for i := range after.Tasks {
		task := &after.Tasks[i]
		notices = append(notices, taskNotices(previous[task.ID], task, master)...)
	}
	return slices.DeleteFunc(notices, func(n TaskNotice) bool {
		return n.To == "" || n.To == actor.Member || !onRoster(t, n.To)
	})
}

func taskNotices(old, task *Task, master string) []TaskNotice {
	var notices []TaskNotice
	notify := func(to, text string) { notices = append(notices, TaskNotice{To: to, Task: task.ID, Text: text}) }
	oldState, oldAssignee := "", ""
	if old != nil {
		oldState, oldAssignee = old.State, old.Assignee
	}
	alreadyBriefed := old != nil && oldAssignee == task.Assignee && briefed(oldState)
	if briefable(task.Assignee, task.State) && !alreadyBriefed {
		notify(task.Assignee, TaskBrief(task))
	}
	if task.State != oldState {
		switch task.State {
		case "review":
			notify(task.Reviewer, fmt.Sprintf("Task %s %q is ready for your review. Check the acceptance "+
				"criteria, then call review_task with decision accept or changes and a comment.", task.ID, task.Title))
			notify(master, fmt.Sprintf("Task %s %q moved to review (reviewer %s).", task.ID, task.Title, task.Reviewer))
		case "done", "blocked", TaskCancelled:
			notify(master, fmt.Sprintf("Task %s %q is now %s.", task.ID, task.Title, task.State))
		case "in_progress":
			if oldState == "review" {
				notify(task.Assignee, fmt.Sprintf("Review requested changes on task %s %q. Read the latest "+
					"comment with my_tasks, fix it, then move the task to review again.", task.ID, task.Title))
			}
		}
	}
	if old != nil && len(task.Comments) > len(old.Comments) {
		comment := task.Comments[len(task.Comments)-1]
		for _, to := range []string{task.Assignee, master} {
			if to != comment.Actor {
				notify(to, fmt.Sprintf("New comment on task %s %q from %s: %s", task.ID, task.Title,
					comment.Actor, truncateNotice(comment.Text)))
			}
		}
	}
	return notices
}

func briefable(assignee, state string) bool {
	return assignee != "" && (state == "ready" || state == "in_progress")
}

func briefed(state string) bool {
	return state == "ready" || state == "in_progress" || state == "review" || state == "blocked"
}

func teamMaster(t *Team) string {
	for i := range t.Members {
		if t.Members[i].Role == "master" {
			return t.Members[i].Name
		}
	}
	return ""
}

func onRoster(t *Team, name string) bool {
	return slices.ContainsFunc(t.Members, func(m Member) bool { return m.Name == name })
}

func truncateNotice(text string) string {
	const limit = 600
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

// TaskBrief is the prompt an assignee receives; it is generated from the task, not free text.
func TaskBrief(task *Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are assigned task %s: %s\n", task.ID, task.Title)
	if task.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", task.Description)
	}
	b.WriteString("\nAcceptance criteria:\n")
	for _, criterion := range task.Criteria {
		fmt.Fprintf(&b, "- [%s] %s\n", criterion.ID, criterion.Text)
	}
	fmt.Fprintf(&b, "\nOwned paths: %s\n", strings.Join(task.Paths, ", "))
	if len(task.Dependencies) > 0 {
		fmt.Fprintf(&b, "Depends on: %s\n", strings.Join(task.Dependencies, ", "))
	}
	if task.Reviewer != "" {
		fmt.Fprintf(&b, "Reviewer: %s\n", task.Reviewer)
	}
	b.WriteString("\nWork only inside the owned paths. Use update_my_task to tick criteria, add progress " +
		"comments or mark the task blocked with a reason. When every criterion is met and package tests " +
		"pass, move it to review with update_my_task and submit_report with the evidence.")
	return b.String()
}

// WorkflowManual is the board operating manual added to a member's system prompt.
func WorkflowManual(role string) string {
	if role == "master" {
		return masterManual
	}
	return workerManual
}

const masterManual = `Board operating manual (you are the master; the task board is the source of truth):
- When the user gives you a goal, plan it as tasks with create_task (or split_task for a large one): a
  clear title, a description, 2-6 checkable acceptance criteria, owned paths that do not overlap other
  open tasks, dependencies, priority and an estimate. Keep tasks small enough for one worker session.
- Assign with set_assignee (and a reviewer other than the assignee), then move_task to in_progress.
  agent-deck sends the assignee a brief generated from the task; do not re-send it.
- Use list_tasks to see the board. Workers report through update_my_task and submit_report; you are
  notified on review, blocked, done and comments. Answer blocked tasks quickly, re-plan when work fails.
- Review with review_task (accept or changes with a concrete comment). Never mark your own work accepted.
- Use propose_workers when ready tasks outnumber idle workers; the user approves new workers.
- Keep the board current: cancel obsolete tasks, comment decisions on the task they affect.`

const workerManual = `Board operating manual (you are a worker; the task board is the source of truth):
- Call my_tasks to see your assigned tasks. Work only inside a task's owned paths.
- Use update_my_task to tick criteria as you meet them, comment progress or questions, or move the task
  to blocked with a reason. Move it to review when every criterion is met and your package tests pass,
  then call submit_report with the evidence.
- If a reviewer requests changes you will be notified; fix them and move the task to review again.`

// workflowInstructions adds the manual for members that can reach the team tools.
func workflowInstructions(t *Team, member *Member) string {
	if member.ControlDir == "" && !t.LeasePool {
		return ""
	}
	return "\n\n" + WorkflowManual(member.Role)
}
