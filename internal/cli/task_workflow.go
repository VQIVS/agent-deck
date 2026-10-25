package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func readTaskWorkflow(ctx context.Context, store team.BoardStore, current *team.Team, options *taskFlags,
	command string, out io.Writer,
) error {
	state, err := store.Load(ctx)
	if errors.Is(err, os.ErrNotExist) {
		state.Board = team.TaskBoard{Version: 2, Workflow: current.Workflow}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read workflow: %w", err)
	}
	if command == "pending-release" {
		pending := []team.Task{}
		for i := range state.Board.Tasks {
			if state.Board.Category(state.Board.Tasks[i].State) == "done" {
				pending = append(pending, state.Board.Tasks[i])
			}
		}
		return writeJSON(out, pending)
	}
	view := struct {
		Workflow               team.WorkflowDefinition `json:"workflow"`
		Task                   string                  `json:"task,omitempty"`
		ValidMoves             []string                `json:"valid_moves"`
		ReleaseVersionRequired bool                    `json:"release_version_required"`
	}{
		Workflow: state.Board.Workflow.Resolved(), Task: options.Call.ID,
		ValidMoves: []string{}, ReleaseVersionRequired: true,
	}
	if view.Task != "" {
		found := slices.ContainsFunc(state.Board.Tasks, func(task team.Task) bool { return task.ID == view.Task })
		if !found {
			return fmt.Errorf("unknown task %q", view.Task)
		}
		mutation := team.TaskMutation{Actor: team.TaskActor{User: true}, Now: time.Now()}
		for _, stage := range state.Board.ValidMoves(view.Task, mutation) {
			view.ValidMoves = append(view.ValidMoves, stage.Name)
		}
	}
	return writeJSON(out, view)
}
