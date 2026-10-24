package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

// RenderKanbanReview includes the current team's merged DoD only when task detail is open.
func RenderKanbanReview(tasks []team.Task, screen *KanbanScreen, opts *KanbanOptions,
	current *team.Team,
) (string, []KanbanTarget) {
	task := screen.reconcile(tasks)
	if !screen.Detail || task == nil {
		return RenderKanban(tasks, screen, opts)
	}
	definition, err := team.TaskReviewDefinition(current, task)
	requirements := "Merged definition of done unavailable"
	if err != nil {
		requirements += ": " + err.Error()
	} else {
		requirements = TaskDefinitionDetail(definition, max(1, min(110, opts.Width-4)))
	}
	return renderKanbanDetailRequirements(task, screen, opts, requirements)
}

// ApplyTaskReview applies a trusted UI review through the common workflow persistence and notice boundary.
func ApplyTaskReview(ctx context.Context, store team.BoardStore, current *team.Team,
	taskID, decision, comment string, now time.Time,
) (team.TaskBoard, []team.TaskNotice, error) {
	board, notices, err := team.ChangeTasks(ctx, store, current, team.TaskActor{User: true}, now,
		func(board *team.TaskBoard, m team.TaskMutation) error {
			return team.ReviewTaskForTeam(board, current, taskID, "", decision, comment, true, m.Now, m.ReadOnly)
		})
	if err != nil {
		return team.TaskBoard{}, nil, fmt.Errorf("review task %q: %w", taskID, err)
	}
	return board, notices, nil
}
