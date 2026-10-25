package team

import "fmt"

const (
	TaskKindTask = "task"
	TaskKindEpic = "epic"
)

// TaskSchedulable treats omitted kind as implementation work for compatibility.
// Planning parents must be explicitly classified; titles never grant scheduling semantics.
func TaskSchedulable(task *Task) bool { return task.Kind == "" || task.Kind == TaskKindTask }

func validateTaskKind(task *Task) error {
	if task.Kind != "" && task.Kind != TaskKindTask && task.Kind != TaskKindEpic {
		return fmt.Errorf("task kind must be task or epic")
	}
	return nil
}

func (b *TaskBoard) checkAutomaticTaskStart(id, to string, automation bool) error {
	if !automation || b.Category(to) != "active" {
		return nil
	}
	task, err := taskByID(b, id)
	if err != nil {
		return err
	}
	if !TaskSchedulable(task) {
		return fmt.Errorf("epic is planning-only and cannot start automatically")
	}
	return nil
}
