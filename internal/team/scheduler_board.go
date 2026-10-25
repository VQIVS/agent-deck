package team

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// TaskEstimate is a planner's prediction, never an observed result.
type TaskEstimate struct {
	Size     string        `json:"size"`
	Parallel bool          `json:"parallel"`
	Duration time.Duration `json:"duration"`
	Tokens   int64         `json:"tokens"`
	Cost     float64       `json:"cost"`
}

// TaskUsage contains externally measured usage and its provenance.
type TaskUsage struct {
	Duration time.Duration `json:"duration"`
	Tokens   int64         `json:"tokens"`
	Cost     float64       `json:"cost"`
	Rework   int           `json:"rework"`
	Source   string        `json:"source"`
}

// TaskReport is worker-reported evidence, distinct from measured usage.
type TaskReport struct {
	Text  string `json:"text"`
	Tests string `json:"tests,omitempty"`
}

// Task is one independently owned unit of work on a versioned board.
type Task struct {
	category         string
	categoryState    string
	ReleaseVersion   string          `json:"release_version,omitempty"`
	ReleaseURL       string          `json:"release_url,omitempty"`
	ShippedAt        time.Time       `json:"shipped_at,omitempty"`
	LegacyCompletion bool            `json:"legacy_completion,omitempty"`
	StartedAt        time.Time       `json:"started_at,omitempty"`
	CompletedAt      time.Time       `json:"completed_at,omitempty"`
	Description      string          `json:"description,omitempty"`
	Criteria         []TaskCriterion `json:"criteria,omitempty"`
	Priority         string          `json:"priority,omitempty"`
	Labels           []string        `json:"labels,omitempty"`
	Assignee         string          `json:"assignee,omitempty"`
	Reviewer         string          `json:"reviewer,omitempty"`
	Branch           string          `json:"branch,omitempty"`
	Commits          []string        `json:"commits,omitempty"`
	CreatedAt        time.Time       `json:"created_at,omitempty"`
	UpdatedAt        time.Time       `json:"updated_at,omitempty"`
	History          []TaskChange    `json:"history,omitempty"`
	Comments         []TaskComment   `json:"comments,omitempty"`
	Review           *TaskReview     `json:"review,omitempty"`
	Completion       *ReportRecord   `json:"completion,omitempty"`
	ID               string          `json:"id"`
	Title            string          `json:"title"`
	Paths            Leases          `json:"paths"`
	Dependencies     []string        `json:"dependencies,omitempty"`
	Role             string          `json:"role,omitempty"`
	Skills           []string        `json:"skills,omitempty"`
	Estimate         TaskEstimate    `json:"estimate"`
	State            string          `json:"state"`
	Worker           string          `json:"worker,omitempty"`
	Report           *TaskReport     `json:"report,omitempty"`
	Measured         *TaskUsage      `json:"measured,omitempty"`
}

// TaskBoard stores task state separately from team session bindings.
type TaskBoard struct {
	Workflow WorkflowDefinition `json:"workflow,omitempty"`
	Version  int                `json:"version"`
	History  []TaskChange       `json:"history,omitempty"`
	Tasks    []Task             `json:"tasks"`
}

func (b *TaskBoard) Validate() error {
	if b == nil || (b.Version != 1 && b.Version != 2) {
		return fmt.Errorf("task board requires version 1 or 2")
	}
	if err := b.Workflow.Validate(); err != nil {
		return err
	}
	index := make(map[string]int, len(b.Tasks))
	for i := range b.Tasks {
		task := &b.Tasks[i]
		if b.Version == 2 {
			bindTaskCategory(b, task)
			if task.category == "" {
				return fmt.Errorf("task %q uses undefined workflow stage %q", task.ID, task.State)
			}
		}
		if _, exists := index[task.ID]; task.ID == "" || exists {
			return fmt.Errorf("task ids must be nonempty and unique")
		}
		if err := validateBoardTask(task, b.Version); err != nil {
			return fmt.Errorf("task %q: %w", task.ID, err)
		}
		index[task.ID] = i
	}
	marks := make(map[string]int, len(b.Tasks))
	for i := range b.Tasks {
		if err := b.visit(b.Tasks[i].ID, index, marks); err != nil {
			return err
		}
	}
	if b.Version == 2 {
		return validateWorkflowBoard(b)
	}
	return nil
}

func validateTask(task *Task) error {
	if task.Title == "" || len(task.Paths) == 0 {
		return fmt.Errorf("title and owned paths are required")
	}
	if err := task.Paths.Validate(); err != nil {
		return fmt.Errorf("owned paths: %w", err)
	}
	if task.Estimate.Duration < 0 || task.Estimate.Tokens < 0 || invalidCost(task.Estimate.Cost) {
		return fmt.Errorf("estimate must be finite and nonnegative")
	}
	if err := validateTaskState(task); err != nil {
		return err
	}
	if task.Measured != nil {
		return validateUsage(task.Measured)
	}
	return nil
}

func validateTaskState(task *Task) error {
	switch task.State {
	case "pending":
		if task.Worker != "" || task.Report != nil || task.Measured != nil {
			return fmt.Errorf("pending task cannot have assignment or outcomes")
		}
	case "assigned":
		if task.Worker == "" || task.Report != nil || task.Measured != nil {
			return fmt.Errorf("assigned task requires worker and no outcomes")
		}
	case "done", "failed":
		if task.Worker == "" || task.Report == nil || task.Report.Text == "" {
			return fmt.Errorf("finished task requires worker and report")
		}
	default:
		return fmt.Errorf("unknown task state %q", task.State)
	}
	return nil
}

func validateUsage(usage *TaskUsage) error {
	if usage.Source == "" || usage.Duration < 0 || usage.Tokens < 0 || usage.Rework < 0 || invalidCost(usage.Cost) {
		return fmt.Errorf("measured usage requires provenance and finite nonnegative values")
	}
	return nil
}

func invalidCost(value float64) bool {
	return value < 0 || math.IsNaN(value) || math.IsInf(value, 0)
}

func (b *TaskBoard) visit(id string, index, marks map[string]int) error {
	if marks[id] == 1 {
		return fmt.Errorf("task dependency cycle at %q", id)
	}
	if marks[id] == 2 {
		return nil
	}
	i, exists := index[id]
	if !exists {
		return fmt.Errorf("unknown task dependency %q", id)
	}
	marks[id] = 1
	seen := make(map[string]bool)
	for _, dependency := range b.Tasks[i].Dependencies {
		if seen[dependency] {
			return fmt.Errorf("duplicate dependency %q", dependency)
		}
		seen[dependency] = true
		if err := b.visit(dependency, index, marks); err != nil {
			return err
		}
	}
	marks[id] = 2
	return nil
}

func (b *TaskBoard) Finish(id, authenticatedWorker string, report TaskReport, failed, readOnly bool) error {
	if b.Version == 2 {
		return fmt.Errorf("workflow completion requires ReviewTask or CompleteTask; failures use MoveTask blocked")
	}
	if readOnly {
		return fmt.Errorf("read-only: task changes disabled")
	}
	if err := b.Validate(); err != nil {
		return err
	}
	for i := range b.Tasks {
		task := &b.Tasks[i]
		if task.ID != id {
			continue
		}
		if authenticatedWorker == "" || task.Worker != authenticatedWorker {
			return fmt.Errorf("finishing requires the authenticated assigned worker")
		}
		if task.State != "assigned" || report.Text == "" {
			return fmt.Errorf("finishing requires an assigned task and a report")
		}
		task.State, task.Report = "done", &report
		if failed {
			task.State = "failed"
		}
		return nil
	}
	return fmt.Errorf("unknown task %q", id)
}

func (b *TaskBoard) RecordUsage(id string, usage TaskUsage, readOnly bool) error {
	if readOnly {
		return fmt.Errorf("read-only: task changes disabled")
	}
	if err := b.Validate(); err != nil {
		return err
	}
	if err := validateUsage(&usage); err != nil {
		return err
	}
	for i := range b.Tasks {
		task := &b.Tasks[i]
		if task.ID == id {
			if task.State != "done" && task.State != "failed" {
				return fmt.Errorf("measured usage requires a finished task")
			}
			task.Measured = &usage
			return nil
		}
	}
	return fmt.Errorf("unknown task %q", id)
}

func cloneTask(task *Task) Task {
	result := *task
	result.Paths = slices.Clone(task.Paths)
	result.Dependencies = slices.Clone(task.Dependencies)
	result.Skills = slices.Clone(task.Skills)
	cloneWorkflowFields(&result)
	return result
}

func (b *TaskBoard) Add(task *Task, readOnly bool) error {
	if readOnly {
		return fmt.Errorf("read-only: task changes disabled")
	}
	if err := b.Validate(); err != nil {
		return err
	}
	if task == nil || task.State != "pending" {
		return fmt.Errorf("new task must be pending")
	}
	candidate := TaskBoard{Version: b.Version, Tasks: slices.Clone(b.Tasks)}
	candidate.Tasks = append(candidate.Tasks, cloneTask(task))
	if err := candidate.Validate(); err != nil {
		return err
	}
	b.Tasks = candidate.Tasks
	return nil
}
