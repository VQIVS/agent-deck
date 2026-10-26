package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	mutation := team.TaskMutation{Actor: team.TaskActor{User: true}, Now: time.Now()}
	view, err := team.ReadWorkflowView(&state.Board, options.Call.ID, mutation)
	if err != nil {
		return fmt.Errorf("read workflow view: %w", err)
	}
	return writeJSON(out, view)
}
