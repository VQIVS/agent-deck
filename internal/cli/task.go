package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

func taskCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: agent-deck task ls|show|workflow|pending-release|add|edit|move|assign|comment|review " +
			"--team T [flags]")
	}
	command := args[0]
	options, err := parseTaskFlags(command, args[1:])
	if err != nil {
		return err
	}
	store := team.Store{Root: team.DefaultRoot(), ReadOnly: options.ReadOnly}
	current, revision, err := store.Load(ctx, options.Team)
	if err != nil {
		return fmt.Errorf("load task team: %w", err)
	}
	current.Server = resolveServer(options.Server, current.Server)
	manager := &team.Manager{Store: store, Team: current, Version: revision, ReadOnly: options.ReadOnly}
	root, err := server.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve task board root: %w", err)
	}
	boardStore := team.BoardStore{
		Root: filepath.Join(root, "boards"), Team: current.Name,
		ReadOnly: options.ReadOnly || taskObservation(command),
	}
	if taskObservation(command) {
		if command == "ls" || command == "show" {
			return readTasks(ctx, boardStore, &options, command, out)
		}
		return readTaskWorkflow(ctx, boardStore, &current, &options, command, out)
	}
	if options.ReadOnly {
		return fmt.Errorf("read-only: task mutations disabled")
	}
	return applyTaskCommand(ctx, manager, boardStore, &options, command, out)
}

func taskObservation(command string) bool {
	return command == "ls" || command == "show" || command == "workflow" || command == "pending-release"
}

func applyTaskCommand(ctx context.Context, manager *team.Manager, boardStore team.BoardStore,
	options *taskFlags, command string, out io.Writer,
) error {
	current := manager.Team
	options.Call.Version, options.Call.ReleaseURL = options.Version, options.ReleaseURL
	change, err := taskChange(command, &options.Call, &current)
	if err != nil {
		return err
	}
	board, notices, err := team.ChangeTasks(ctx, boardStore, &current, team.TaskActor{User: true}, time.Now(), change)
	if err != nil {
		return fmt.Errorf("change task: %w", err)
	}
	warning := notifyTaskNotices(ctx, manager, notices)
	if options.JSON {
		return writeJSON(out, struct {
			Board   team.TaskBoard `json:"board"`
			Warning string         `json:"warning,omitempty"`
		}{Board: board, Warning: warning})
	}
	if err := writeText(out, "Task change saved"); err != nil {
		return err
	}
	if warning != "" {
		return writeText(out, "Warning: "+warning)
	}
	return nil
}

func taskChange(command string, call *team.WorkflowCall,
	current *team.Team,
) (func(*team.TaskBoard, team.TaskMutation) error, error) {
	if command == "review" {
		return func(board *team.TaskBoard, m team.TaskMutation) error {
			return team.ReviewTaskForTeam(board, current, call.ID, "", call.Decision, call.Comment, true, m.Now, m.ReadOnly)
		}, nil
	}
	if command == "add" && call.ID != "" {
		return func(board *team.TaskBoard, m team.TaskMutation) error {
			return planNamedTask(board, &call.TaskSpec, &m)
		}, nil
	}
	names := map[string]string{
		"add": "create_task", "edit": "update_task", "move": "move_task",
		"assign": "set_assignee", "comment": "comment_task",
	}
	name, ok := names[command]
	if !ok {
		return nil, fmt.Errorf("unsupported task mutation %q", command)
	}
	change, err := team.WorkflowChange(name, call)
	if err != nil {
		return nil, fmt.Errorf("select task mutation: %w", err)
	}
	return change, nil
}

func planNamedTask(board *team.TaskBoard, spec *team.TaskSpec, m *team.TaskMutation) error {
	for i := range board.Tasks {
		if board.Tasks[i].ID == spec.ID {
			return fmt.Errorf("task ID already exists")
		}
	}
	id, err := board.PlanTask(spec, *m)
	if err != nil {
		return fmt.Errorf("plan named task: %w", err)
	}
	for i := range board.Tasks {
		if board.Tasks[i].ID == id {
			board.Tasks[i].ID = spec.ID
			renameTaskEvents(board.Tasks[i].History, id, spec.ID)
		}
	}
	renameTaskEvents(board.History, id, spec.ID)
	if err := board.Validate(); err != nil {
		return fmt.Errorf("validate named task: %w", err)
	}
	return nil
}

func renameTaskEvents(events []team.TaskChange, from, to string) {
	for i := range events {
		if events[i].Task == from {
			events[i].Task = to
		}
	}
}

func readTasks(ctx context.Context, store team.BoardStore, options *taskFlags, command string, out io.Writer) error {
	state, err := store.Load(ctx)
	if errors.Is(err, os.ErrNotExist) {
		state.Board = team.TaskBoard{Version: 2}
	} else if err != nil {
		return fmt.Errorf("read task board: %w", err)
	}
	if command == "ls" {
		if options.JSON {
			if state.Board.Tasks == nil {
				state.Board.Tasks = []team.Task{}
			}
			return writeJSON(out, state.Board.Tasks)
		}
		for i := range state.Board.Tasks {
			task := &state.Board.Tasks[i]
			if err := writeText(out, fmt.Sprintf("%s\t%s\t%s\t%s", task.ID, task.State, task.Assignee, task.Title)); err != nil {
				return err
			}
		}
		return nil
	}
	for i := range state.Board.Tasks {
		if state.Board.Tasks[i].ID == options.Call.ID {
			if options.JSON {
				return writeJSON(out, &state.Board.Tasks[i])
			}
			return showTaskText(out, &state.Board.Tasks[i])
		}
	}
	return fmt.Errorf("unknown task %q", options.Call.ID)
}

func showTaskText(out io.Writer, task *team.Task) error {
	var text strings.Builder
	fmt.Fprintf(&text, "%s · %s\nState: %s\nAssignee: %s\nReviewer: %s\nDescription: %s\n",
		task.ID, task.Title, task.State, task.Assignee, task.Reviewer, task.Description)
	fmt.Fprintf(&text, "Paths: %s\nDependencies: %s\nAcceptance criteria:\n",
		strings.Join(task.Paths, ", "), strings.Join(task.Dependencies, ", "))
	for _, criterion := range task.Criteria {
		mark := "[ ]"
		if criterion.Checked {
			mark = "[x]"
		}
		fmt.Fprintf(&text, "%s %s: %s\n", mark, criterion.ID, criterion.Text)
	}
	text.WriteString("History:\n")
	for i := range task.History {
		event := &task.History[i]
		fmt.Fprintf(&text, "%s %s %s: %s -> %s\n",
			event.At.Format(time.RFC3339), event.Actor, event.Field, event.From, event.To)
	}
	return writeText(out, strings.TrimSuffix(text.String(), "\n"))
}
