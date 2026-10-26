package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/vqivs/agent-deck/internal/team"
)

func (t *TaskTools) workflowRead(ctx context.Context, store team.BoardStore, manager *team.Manager,
	actor team.TaskActor, name string, call *team.WorkflowCall,
) (any, error) {
	state, err := store.Load(ctx)
	if errors.Is(err, os.ErrNotExist) {
		state.Board = team.TaskBoard{Version: 2, Workflow: manager.Team.Workflow}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read workflow: %w", err)
	}
	if name == "pending_release" {
		out := []taskSummary{}
		for i := range state.Board.Tasks {
			task := &state.Board.Tasks[i]
			if state.Board.Category(task.State) == "done" {
				summary := summarize(task, true)
				summary.Category = "done"
				out = append(out, summary)
			}
		}
		return out, nil
	}
	mutation := team.TaskMutation{Actor: actor, Now: t.now()}
	view, err := team.ReadWorkflowView(&state.Board, call.ID, mutation)
	if err != nil {
		return nil, fmt.Errorf("read workflow view: %w", err)
	}
	return view, nil
}
