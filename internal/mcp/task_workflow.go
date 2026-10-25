package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/vqivs/agent-deck/internal/team"
)

type workflowView struct {
	Definition             team.WorkflowDefinition `json:"workflow"`
	Task                   string                  `json:"task,omitempty"`
	ValidMoves             []string                `json:"valid_moves"`
	ReleaseVersionRequired bool                    `json:"release_version_required"`
}

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
	view := workflowView{
		Definition: state.Board.Workflow.Resolved(), Task: call.ID,
		ValidMoves: []string{}, ReleaseVersionRequired: true,
	}
	if call.ID == "" {
		return view, nil
	}
	found := false
	for i := range state.Board.Tasks {
		if state.Board.Tasks[i].ID == call.ID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown task %q", call.ID)
	}
	// Observation remains read-only; hypothetical moves evaluate normal actor guards.
	mutation := team.TaskMutation{Actor: actor, Now: t.now()}
	for _, stage := range state.Board.ValidMoves(call.ID, mutation) {
		view.ValidMoves = append(view.ValidMoves, stage.Name)
	}
	return view, nil
}
