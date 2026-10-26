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

// Task is one independently owned unit of work on a versioned board.
type Task struct {
	Kind           string `json:"kind,omitempty"`
	category       string
	categoryState  string
	ReleaseVersion string          `json:"release_version,omitempty"`
	ReleaseURL     string          `json:"release_url,omitempty"`
	ShippedAt      time.Time       `json:"shipped_at,omitempty"`
	StartedAt      time.Time       `json:"started_at,omitempty"`
	CompletedAt    time.Time       `json:"completed_at,omitempty"`
	Description    string          `json:"description,omitempty"`
	Criteria       []TaskCriterion `json:"criteria,omitempty"`
	Priority       string          `json:"priority,omitempty"`
	Labels         []string        `json:"labels,omitempty"`
	Assignee       string          `json:"assignee,omitempty"`
	Reviewer       string          `json:"reviewer,omitempty"`
	Branch         string          `json:"branch,omitempty"`
	Commits        []string        `json:"commits,omitempty"`
	CreatedAt      time.Time       `json:"created_at,omitempty"`
	UpdatedAt      time.Time       `json:"updated_at,omitempty"`
	History        []TaskChange    `json:"history,omitempty"`
	Comments       []TaskComment   `json:"comments,omitempty"`
	Review         *TaskReview     `json:"review,omitempty"`
	Completion     *ReportRecord   `json:"completion,omitempty"`
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Paths          Leases          `json:"paths"`
	Dependencies   []string        `json:"dependencies,omitempty"`
	Role           string          `json:"role,omitempty"`
	Skills         []string        `json:"skills,omitempty"`
	Estimate       TaskEstimate    `json:"estimate"`
	State          string          `json:"state"`
}

// TaskBoard stores task state separately from team session bindings.
type TaskBoard struct {
	Workflow WorkflowDefinition `json:"workflow,omitempty"`
	Version  int                `json:"version"`
	History  []TaskChange       `json:"history,omitempty"`
	Tasks    []Task             `json:"tasks"`
}

func (b *TaskBoard) Validate() error {
	if b != nil && b.Version == 1 {
		return fmt.Errorf("board v1 unsupported; open it once with agent-deck v0.3.x")
	}
	if b == nil || b.Version != 2 {
		return fmt.Errorf("task board requires version 2")
	}
	if err := b.Workflow.Validate(); err != nil {
		return err
	}
	index := make(map[string]int, len(b.Tasks))
	for i := range b.Tasks {
		task := &b.Tasks[i]
		bindTaskCategory(b, task)
		if task.category == "" {
			return fmt.Errorf("task %q uses undefined workflow stage %q", task.ID, task.State)
		}
		if _, exists := index[task.ID]; task.ID == "" || exists {
			return fmt.Errorf("task ids must be nonempty and unique")
		}
		if err := validateBoardTask(task); err != nil {
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
	return validateWorkflowBoard(b)
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

func cloneTask(task *Task) Task {
	result := *task
	result.Paths = slices.Clone(task.Paths)
	result.Dependencies = slices.Clone(task.Dependencies)
	result.Skills = slices.Clone(task.Skills)
	cloneWorkflowFields(&result)
	return result
}
