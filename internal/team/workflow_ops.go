package team

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TaskSpec is the plannable part of a task, as the master or user writes it.
type TaskSpec struct {
	ID           string   `json:"id,omitempty"`
	Title        string   `json:"title,omitempty"`
	Description  string   `json:"description,omitempty"`
	Criteria     []string `json:"criteria,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Priority     string   `json:"priority,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Estimate     string   `json:"estimate,omitempty"`
	Tokens       int64    `json:"tokens,omitempty"`
	Assignee     string   `json:"assignee,omitempty"`
	Reviewer     string   `json:"reviewer,omitempty"`
	Ready        bool     `json:"ready,omitempty"`
	// Parallel defaults to true: tasks with non-overlapping paths may run at the same time.
	Parallel *bool `json:"parallel,omitempty"`
}

// PlanTask creates a task from a spec with the next free T-<n> ID, optionally ready and assigned.
func (b *TaskBoard) PlanTask(spec *TaskSpec, m TaskMutation) (string, error) {
	task, err := spec.task(b.nextTaskID())
	if err != nil {
		return "", err
	}
	if err := b.CreateTask(&task, m); err != nil {
		return "", err
	}
	if spec.Ready {
		if err := b.MoveTask(task.ID, b.Destination("ready"), m); err != nil {
			return "", err
		}
	}
	if spec.Assignee != "" {
		return task.ID, b.SetAssignee(task.ID, spec.Assignee, m)
	}
	return task.ID, nil
}

func (s *TaskSpec) task(id string) (Task, error) {
	task := Task{
		ID: id, Title: strings.TrimSpace(s.Title), Description: s.Description, Paths: s.Paths,
		Dependencies: s.Dependencies, Priority: s.Priority, Labels: s.Labels, Reviewer: s.Reviewer,
	}
	for i, text := range s.Criteria {
		task.Criteria = append(task.Criteria, TaskCriterion{ID: "c" + strconv.Itoa(i+1), Text: text})
	}
	if s.Estimate != "" {
		duration, err := time.ParseDuration(s.Estimate)
		if err != nil {
			return task, fmt.Errorf("estimate must be a duration like 45m or 2h: %w", err)
		}
		task.Estimate.Duration = duration
	}
	task.Estimate.Tokens = s.Tokens
	task.Estimate.Parallel = s.Parallel == nil || *s.Parallel
	return task, nil
}

func (b *TaskBoard) nextTaskID() string {
	highest := 0
	for i := range b.Tasks {
		if n, err := strconv.Atoi(strings.TrimPrefix(b.Tasks[i].ID, "T-")); err == nil && n > highest {
			highest = n
		}
	}
	return "T-" + strconv.Itoa(highest+1)
}

// EditTask applies the fields a spec sets; state and assignee change only through workflow actions.
func (b *TaskBoard) EditTask(spec *TaskSpec, m TaskMutation) error {
	current, err := taskByID(b, spec.ID)
	if err != nil {
		return err
	}
	next := cloneTask(current)
	fields, err := spec.task(spec.ID)
	if err != nil {
		return err
	}
	spec.merge(&next, &fields)
	return b.UpdateTask(spec.ID, &next, m)
}

func (s *TaskSpec) merge(next, fields *Task) {
	if fields.Title != "" {
		next.Title = fields.Title
	}
	if s.Description != "" {
		next.Description = fields.Description
	}
	if len(s.Criteria) > 0 {
		next.Criteria = fields.Criteria
	}
	if len(s.Paths) > 0 {
		next.Paths = fields.Paths
	}
	if s.Dependencies != nil {
		next.Dependencies = fields.Dependencies
	}
	if s.Priority != "" {
		next.Priority = fields.Priority
	}
	if s.Labels != nil {
		next.Labels = fields.Labels
	}
	if s.Reviewer != "" {
		next.Reviewer = fields.Reviewer
	}
	if s.Estimate != "" || s.Tokens != 0 {
		next.Estimate.Duration, next.Estimate.Tokens = fields.Estimate.Duration, fields.Estimate.Tokens
	}
	if s.Parallel != nil {
		next.Estimate.Parallel = *s.Parallel
	}
}

// SplitTask replaces a task with at least two planned parts and cancels the original.
func (b *TaskBoard) SplitTask(id string, parts []TaskSpec, m TaskMutation) error {
	if err := b.CanTransition(id, b.Destination(TaskCancelled), m); err != nil {
		return err
	}
	if _, err := taskByID(b, id); err != nil || len(parts) < 2 {
		return fmt.Errorf("split needs an existing task and at least two parts")
	}
	created := make([]string, 0, len(parts))
	for i := range parts {
		planned, err := b.PlanTask(&parts[i], m)
		if err != nil {
			return fmt.Errorf("part %d: %w", i+1, err)
		}
		created = append(created, planned)
	}
	if err := b.CommentTask(id, "Split into "+strings.Join(created, ", "), m); err != nil {
		return err
	}
	return b.MoveTask(id, b.Destination(TaskCancelled), m)
}

// AssignTask sets the reviewer (when given) and the assignee in one change.
func (b *TaskBoard) AssignTask(id, assignee, reviewer string, m TaskMutation) error {
	if reviewer != "" {
		if err := b.EditTask(&TaskSpec{ID: id, Reviewer: reviewer}, m); err != nil {
			return err
		}
	}
	return b.SetAssignee(id, assignee, m)
}

// UpdateOwnTask is the worker's single progress action: tick criteria, comment, then move.
func (b *TaskBoard) UpdateOwnTask(id string, checked []string, comment, state string, m TaskMutation) error {
	task, err := taskByID(b, id)
	if err != nil || task.Assignee != m.Actor.Member {
		return fmt.Errorf("task %q is not assigned to you", id)
	}
	if len(checked) > 0 {
		claims := make(map[string]bool, len(checked))
		for _, criterion := range checked {
			claims[criterion] = true
		}
		if err := b.UpdateTaskProgress(id, claims, m); err != nil {
			return err
		}
	}
	if comment != "" {
		if err := b.CommentTask(id, comment, m); err != nil {
			return err
		}
	}
	if state != "" {
		return b.MoveTask(id, state, m)
	}
	return nil
}

// moveByTool leaves starting assigned work to agent-deck, which applies leases and one-task-per-worker.
func (b *TaskBoard) moveByTool(id, state string, m TaskMutation) error {
	task, err := taskByID(b, id)
	if err != nil {
		return err
	}
	resumes := taskCategory(task) == "blocked" && !task.StartedAt.IsZero()
	if b.Category(state) == "active" && !resumes && !m.Actor.User && !m.Actor.Automation {
		return fmt.Errorf("agent-deck starts assigned ready tasks when the worker is free; use set_assignee")
	}
	return b.MoveTask(id, state, m)
}

// WorkflowCall is one decoded board tool call.
type WorkflowCall struct {
	Version    string `json:"version,omitempty"`
	ReleaseURL string `json:"release_url,omitempty"`
	TaskSpec
	State    string     `json:"state,omitempty"`
	Text     string     `json:"text,omitempty"`
	Decision string     `json:"decision,omitempty"`
	Comment  string     `json:"comment,omitempty"`
	Checked  []string   `json:"checked,omitempty"`
	Parts    []TaskSpec `json:"parts,omitempty"`
}

// WorkflowChange maps a board tool to the mutation it performs.
func WorkflowChange(name string, call *WorkflowCall) (func(*TaskBoard, TaskMutation) error, error) {
	changes := map[string]func(*TaskBoard, TaskMutation) error{
		"create_task": func(b *TaskBoard, m TaskMutation) error {
			_, err := b.PlanTask(&call.TaskSpec, m)
			return err
		},
		"update_task": func(b *TaskBoard, m TaskMutation) error { return b.EditTask(&call.TaskSpec, m) },
		"split_task":  func(b *TaskBoard, m TaskMutation) error { return b.SplitTask(call.ID, call.Parts, m) },
		"set_assignee": func(b *TaskBoard, m TaskMutation) error {
			return b.AssignTask(call.ID, call.Assignee, call.Reviewer, m)
		},
		"move_task": func(b *TaskBoard, m TaskMutation) error {
			if b.Category(call.State) == "shipped" {
				return b.MoveTaskRelease(call.ID, call.State, &TaskRelease{Version: call.Version, URL: call.ReleaseURL}, m)
			}
			return b.moveByTool(call.ID, call.State, m)
		},
		"comment_task": func(b *TaskBoard, m TaskMutation) error { return b.CommentTask(call.ID, call.Text, m) },
		"review_task": func(b *TaskBoard, m TaskMutation) error {
			return b.ReviewTask(call.ID, call.Decision, call.Comment, m)
		},
		"update_my_task": func(b *TaskBoard, m TaskMutation) error {
			return b.UpdateOwnTask(call.ID, call.Checked, call.Comment, call.State, m)
		},
	}
	change, ok := changes[name]
	if !ok {
		return nil, fmt.Errorf("unknown task tool %q", name)
	}
	return change, nil
}
