package team

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

const briefDeliveryField = "brief_delivered"

// BriefDeliveryKey binds the latest assignment/start to immutable membership and session.
// Delivery history itself does not change this key, so crash retries retain the same journal ID.
func BriefDeliveryKey(task *Task, current *Team) string {
	var identity string
	for i := range current.Members {
		member := &current.Members[i]
		if member.Name == task.Assignee && !member.Muted && member.Session != "" {
			identity = member.ID + "\x00" + member.Session
		}
	}
	if identity == "" || !briefable(task.Assignee, task.State) {
		return ""
	}
	assignment := task.CreatedAt.UTC().String()
	for _, change := range task.History {
		if change.Field == "assignee" || change.Field == "state" && change.To == "in_progress" {
			assignment = fmt.Sprintf("%s:%s:%s:%d", change.Field, change.From, change.To, change.At.UnixNano())
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(current.Name+"\x00"+task.ID+"\x00"+identity+"\x00"+assignment)))
}

// BriefDelivered means that the private journal append was durably acknowledged on the task.
func BriefDelivered(task *Task, key string) bool {
	for _, change := range task.History {
		if key != "" && change.Field == briefDeliveryField && change.To == key {
			return true
		}
	}
	return false
}

// MissingBriefs retries preexisting in-progress assignments only when the observed worker is idle.
func MissingBriefs(board *TaskBoard, current *Team, workers []SchedulerWorker) []TaskNotice {
	idle := map[string]bool{}
	for i := range workers {
		worker := &workers[i]
		idle[worker.Name] = worker.State == "idle"
	}
	var notices []TaskNotice
	for i := range board.Tasks {
		task := &board.Tasks[i]
		key := BriefDeliveryKey(task, current)
		if key != "" && idle[task.Assignee] && !BriefDelivered(task, key) {
			notices = append(notices, TaskNotice{To: task.Assignee, Task: task.ID, Text: TaskBrief(task), Brief: key})
		}
	}
	return notices
}

// RecordBriefDelivery is called only after successful (or already persisted) journal append.
func RecordBriefDelivery(ctx context.Context, store BoardStore, current *Team,
	notice *TaskNotice, now time.Time,
) error {
	_, _, err := ChangeTasksState(ctx, store, current, AutomationActor, now,
		func(board *TaskBoard, mutation TaskMutation) error {
			for i := range board.Tasks {
				task := &board.Tasks[i]
				if task.ID != notice.Task {
					continue
				}
				if task.Assignee != notice.To || BriefDeliveryKey(task, current) != notice.Brief || notice.Brief == "" {
					return fmt.Errorf("brief assignment changed before delivery record")
				}
				if !BriefDelivered(task, notice.Brief) {
					taskEvent(board, task, mutation, briefDeliveryField, "", notice.Brief)
				}
				return nil
			}
			return fmt.Errorf("brief task no longer exists")
		})
	return err
}
