package team

import "fmt"

// WorkflowView is the shared read-only CLI/MCP workflow response.
type WorkflowView struct {
	Definition             WorkflowDefinition `json:"workflow"`
	Task                   string             `json:"task,omitempty"`
	ValidMoves             []string           `json:"valid_moves"`
	ReleaseVersionRequired bool               `json:"release_version_required"`
}

func ReadWorkflowView(board *TaskBoard, id string, mutation TaskMutation) (WorkflowView, error) {
	view := WorkflowView{
		Definition: board.Workflow.Resolved(), Task: id,
		ValidMoves: []string{}, ReleaseVersionRequired: true,
	}
	if id == "" {
		return view, nil
	}
	if _, err := taskByID(board, id); err != nil {
		return WorkflowView{}, fmt.Errorf("unknown task %q", id)
	}
	for _, stage := range board.ValidMoves(id, mutation) {
		view.ValidMoves = append(view.ValidMoves, stage.Name)
	}
	return view, nil
}
