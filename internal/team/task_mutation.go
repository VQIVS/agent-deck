package team

import (
	"fmt"
	"slices"
)

func (b *TaskBoard) mutate(id string, m TaskMutation, fn func(*TaskBoard, *Task) error) error {
	if err := m.validate(); err != nil {
		return err
	}
	if b.Version != 2 {
		return fmt.Errorf("workflow mutations require board version 2")
	}
	candidate := TaskBoard{
		Version: 2, Workflow: b.Workflow,
		History: slices.Clone(b.History), Tasks: make([]Task, len(b.Tasks)),
	}
	for i := range b.Tasks {
		candidate.Tasks[i] = cloneTask(&b.Tasks[i])
	}
	task, err := taskByID(&candidate, id)
	if err != nil {
		return err
	}
	if err := fn(&candidate, task); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*b = candidate
	return nil
}

func (b *TaskBoard) CreateTask(task *Task, m TaskMutation) error {
	if err := m.validate(); err != nil {
		return err
	}
	if !m.manages() || task == nil || b.Version != 2 {
		return fmt.Errorf("creating task requires master or user and version 2 board")
	}
	if task.State != "" && b.Category(task.State) != "todo" {
		return fmt.Errorf("new task starts in backlog")
	}
	created := cloneTask(task)
	created.State = b.Destination("todo")
	created.Worker = created.Assignee
	created.CreatedAt = m.Now.UTC()
	created.History = nil
	created.Comments = nil
	created.Review = nil
	created.Completion = nil
	created.LegacyCompletion = false
	candidate := TaskBoard{
		Version: 2, Workflow: b.Workflow,
		Tasks: slices.Clone(b.Tasks), History: slices.Clone(b.History),
	}
	taskEvent(&candidate, &created, m, "created", "", created.State)
	candidate.Tasks = append(candidate.Tasks, created)
	if err := candidate.Validate(); err != nil {
		return err
	}
	*b = candidate
	return nil
}

func (b *TaskBoard) UpdateTask(id string, replacement *Task, m TaskMutation) error {
	return b.mutate(id, m, func(board *TaskBoard, task *Task) error {
		if !m.manages() || replacement == nil {
			return fmt.Errorf("task editing requires master or user")
		}
		if TaskShipped(task) {
			return fmt.Errorf("shipped task is immutable; create follow-up work")
		}
		if replacement.ID != id || replacement.State != task.State || replacement.Assignee != task.Assignee ||
			replacement.Worker != task.Worker {
			return fmt.Errorf("use workflow actions to change state or assignee")
		}
		updated := cloneTask(replacement)
		updated.History = task.History
		updated.Comments = task.Comments
		updated.CreatedAt = task.CreatedAt
		updated.Review = nil
		updated.Completion = nil
		updated.LegacyCompletion = false
		*task = updated
		taskEvent(board, task, m, "edited", "", "")
		return nil
	})
}

func (b *TaskBoard) SetAssignee(id, member string, m TaskMutation) error {
	return b.mutate(id, m, func(board *TaskBoard, task *Task) error {
		if !m.manages() {
			return fmt.Errorf("assignment requires master or user")
		}
		if TaskAccepted(task) {
			return fmt.Errorf("accepted task cannot be reassigned")
		}
		before := task.Assignee
		task.Assignee = member
		task.Worker = member
		task.Review = nil
		task.Completion = nil
		task.Report = nil
		task.Measured = nil
		task.LegacyCompletion = false
		if TaskActive(task) {
			task.State = board.Destination("ready")
		}
		taskEvent(board, task, m, "assignee", before, member)
		return nil
	})
}

func (b *TaskBoard) MoveTask(id, state string, m TaskMutation) error {
	if err := b.checkAutomaticTaskStart(id, state, m.Actor.Automation); err != nil {
		return err
	}
	return b.moveSchedulableTask(id, state, m)
}

func (b *TaskBoard) moveSchedulableTask(id, state string, m TaskMutation) error {
	return b.mutate(id, m, func(board *TaskBoard, task *Task) error {
		from, to := taskCategory(task), board.Category(state)
		if to == "shipped" {
			return fmt.Errorf("shipment requires MoveTaskRelease and version metadata")
		}
		if !m.manages() && (task.Assignee != m.Actor.Member || !workerCategoryMove(from, to)) {
			return fmt.Errorf("worker may only progress or block its assigned task")
		}
		if err := board.authorizeTransition(task.State, state, m); err != nil {
			return err
		}
		if !validCategoryMove(from, to) || task.State == state {
			return fmt.Errorf("invalid transition %s -> %s", task.State, state)
		}
		if (to == "ready" || to == "active") && !dependenciesDone(board, task) {
			return fmt.Errorf("task dependencies must be done")
		}
		if to == "active" && task.Assignee != "" && otherActiveTask(board, task) {
			return fmt.Errorf("%s already has an active task", task.Assignee)
		}
		before := task.State
		task.State = state
		if to != "done" {
			task.Review = nil
			task.Completion = nil
		}
		taskEvent(board, task, m, "state", before, state)
		return nil
	})
}

func dependenciesDone(board *TaskBoard, task *Task) bool {
	for _, dependency := range task.Dependencies {
		other, err := taskByID(board, dependency)
		if err != nil || !TaskAccepted(other) {
			return false
		}
	}
	return true
}

func otherActiveTask(board *TaskBoard, task *Task) bool {
	return slices.ContainsFunc(board.Tasks, func(other Task) bool {
		return other.ID != task.ID && other.Assignee == task.Assignee && TaskActive(&other)
	})
}

func (b *TaskBoard) CommentTask(id, text string, m TaskMutation) error {
	return b.mutate(id, m, func(board *TaskBoard, task *Task) error {
		if !m.manages() && m.Actor.Member != task.Assignee && m.Actor.Member != task.Reviewer {
			return fmt.Errorf("comment requires task participant")
		}
		if !boundedTaskText(text) {
			return fmt.Errorf("comment must contain bounded text")
		}
		if len(task.Comments) >= 500 {
			return fmt.Errorf("task comment limit reached")
		}
		id := fmt.Sprintf("comment-%d-%d", m.Now.UnixNano(), len(task.Comments))
		task.Comments = append(task.Comments, TaskComment{ID: id, Actor: m.name(), At: m.Now.UTC(), Text: text})
		taskEvent(board, task, m, "comment", "", id)
		return nil
	})
}

func (b *TaskBoard) ReviewTask(id, decision, comment string, m TaskMutation) error {
	return b.mutate(id, m, func(board *TaskBoard, task *Task) error {
		if taskCategory(task) != "review" || (!m.Actor.User && m.Actor.Member != task.Reviewer) {
			return fmt.Errorf("review requires assigned reviewer or user on a review task")
		}
		if !m.Actor.User && task.Assignee == m.Actor.Member {
			return fmt.Errorf("assignee cannot accept its own review")
		}
		if !boundedTaskText(comment) || (decision != "accept" && decision != "changes") {
			return fmt.Errorf("review needs accept/changes and a comment")
		}
		before := task.State
		target := board.Destination("active")
		if decision == "accept" {
			target = board.Destination("done")
		}
		if err := board.authorizeTransition(before, target, m); err != nil {
			return err
		}
		if decision == "accept" {
			task.Review = &TaskReview{
				Reviewer: task.Reviewer, Accepted: true, Comment: comment, At: m.Now.UTC(), Digest: taskContentDigest(task),
			}
			task.State = board.Destination("done")
		} else {
			task.Review = nil
			task.State = board.Destination("active")
		}
		taskEvent(board, task, m, "review", before, task.State)
		return nil
	})
}

// CompleteTask accepts a trusted stored report, not caller-supplied completion booleans.
//
// Deprecated: integration should use CompleteTaskForTeam to validate current membership and session binding.
func (b *TaskBoard) CompleteTask(id string, record *ReportRecord, m TaskMutation) error {
	return b.mutate(id, m, func(board *TaskBoard, task *Task) error {
		if !m.manages() {
			return fmt.Errorf("report completion requires trusted master or user integration")
		}
		if !TaskActive(task) {
			return fmt.Errorf("completion requires active task")
		}
		task.Completion = record
		if !taskCompletionValid(task) {
			return fmt.Errorf("report does not pass current acceptance criteria")
		}
		before := task.State
		if err := board.authorizeTransition(before, board.Destination("done"), m); err != nil {
			return err
		}
		task.State = board.Destination("done")
		cloneWorkflowFields(task)
		taskEvent(board, task, m, "state", before, task.State)
		return nil
	})
}

func TaskPending(task *Task) bool { return taskCategory(task) == "ready" }
func TaskActive(task *Task) bool {
	return taskCategory(task) == "active" || taskCategory(task) == "review"
}

func TaskFailed(task *Task) bool {
	return taskCategory(task) == "blocked" || taskCategory(task) == TaskCancelled
}
