package ui

import (
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
