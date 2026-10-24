package team

import (
	"fmt"
)

// MigrateTaskBoard converts legacy state without treating unstructured reports as verified completion.
func MigrateTaskBoard(board *TaskBoard) error {
	if board.Version == 2 {
		return board.Validate()
	}
	if err := board.Validate(); err != nil {
		return err
	}
	for i := range board.Tasks {
		task := &board.Tasks[i]
		before := task.State
		switch before {
		case "pending":
			task.State = "ready"
		case "assigned":
			task.State = "in_progress"
		case "done", "failed":
			task.State = "blocked"
		}
		task.Assignee = task.Worker
		if len(task.Criteria) == 0 {
			task.Criteria = []TaskCriterion{{ID: "legacy-verification", Text: "Review and verify the legacy task outcome"}}
		}
		event := TaskChange{Actor: "migration", Task: task.ID, Field: "state", From: before, To: task.State}
		task.History = append(task.History, event)
		board.History = append(board.History, event)
	}
	board.Version = 2
	return board.Validate()
}

func migrateBoardState(state *BoardState) error {
	if state.Version != 1 && state.Version != 2 {
		return fmt.Errorf("board state requires version 1 or 2")
	}
	if err := MigrateTaskBoard(&state.Board); err != nil {
		return err
	}
	state.Version = 2
	return nil
}

// TaskProgress counts claimed checklist progress without implying independent verification.
func TaskProgress(task *Task) (checked, total int) {
	for _, criterion := range task.Criteria {
		if criterion.Checked {
			checked++
		}
	}
	return checked, len(task.Criteria)
}

// UpdateTaskProgress lets an assigned worker record checklist claims without changing acceptance text.
func (b *TaskBoard) UpdateTaskProgress(id string, checked map[string]bool, m TaskMutation) error {
	return b.mutate(id, m, func(board *TaskBoard, task *Task) error {
		if !m.manages() && task.Assignee != m.Actor.Member {
			return fmt.Errorf("progress requires assigned worker")
		}
		known := map[string]bool{}
		for i := range task.Criteria {
			criterion := &task.Criteria[i]
			known[criterion.ID] = true
			if value, ok := checked[criterion.ID]; ok {
				criterion.Checked = value
			}
		}
		for id := range checked {
			if !known[id] {
				return fmt.Errorf("unknown acceptance criterion %q", id)
			}
		}
		taskEvent(board, task, m, "progress", "", "")
		return nil
	})
}

func validateTaskTimes(task *Task) error {
	if !task.CreatedAt.IsZero() && !task.UpdatedAt.IsZero() && task.UpdatedAt.Before(task.CreatedAt) {
		return fmt.Errorf("task updated time precedes creation")
	}
	for _, change := range task.History {
		if change.Actor == "" || change.Field == "" || (change.At.IsZero() && change.Actor != "migration") {
			return fmt.Errorf("task history requires actor, field and timestamp")
		}
	}
	for _, comment := range task.Comments {
		if comment.Actor == "" || comment.At.IsZero() || !boundedTaskText(comment.Text) {
			return fmt.Errorf("invalid task comment")
		}
	}
	return nil
}

// TaskStateAfterAssignment preserves legacy board callers while writing the canonical workflow state.
func TaskStateAfterAssignment(version int) string {
	if version == 1 {
		return "assigned"
	}
	return "in_progress"
}
