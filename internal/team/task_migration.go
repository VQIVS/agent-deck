package team

import (
	"fmt"
)

// UpdateTaskProgress lets an assigned worker record checklist claims without changing acceptance text.
func (b *TaskBoard) UpdateTaskProgress(id string, checked map[string]bool, m TaskMutation) error {
	return b.mutate(id, m, func(board *TaskBoard, task *Task) error {
		if TaskShipped(task) {
			return fmt.Errorf("shipped task progress is immutable")
		}
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
	total := 0
	if len(task.Comments) > 500 || len(task.History) > 2000 {
		return fmt.Errorf("task discussion/history exceeds limits")
	}
	if !task.CreatedAt.IsZero() && !task.UpdatedAt.IsZero() && task.UpdatedAt.Before(task.CreatedAt) {
		return fmt.Errorf("task updated time precedes creation")
	}
	return validateTaskDiscussion(task, total)
}

func validateTaskDiscussion(task *Task, total int) error {
	for i := range task.History {
		change := &task.History[i]
		total += len(change.From) + len(change.To) + len(change.Actor) + len(change.Field)
		if len(change.From) > 4000 || len(change.To) > 4000 {
			return fmt.Errorf("task history entry too large")
		}
		if change.Actor == "" || change.Field == "" || (change.At.IsZero() && change.Actor != "migration") {
			return fmt.Errorf("task history requires actor, field and timestamp")
		}
	}
	for _, comment := range task.Comments {
		total += len(comment.Text)
		if comment.Actor == "" || comment.At.IsZero() || !boundedTaskText(comment.Text) {
			return fmt.Errorf("invalid task comment")
		}
	}
	if total > 1<<20 {
		return fmt.Errorf("task discussion/history exceeds 1 MiB")
	}
	return nil
}
