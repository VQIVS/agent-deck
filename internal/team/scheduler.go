package team

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// SchedulerWorker is an observed worker, including active ownership and idle age.
type SchedulerWorker struct {
	Name            string    `json:"name"`
	Role            string    `json:"role"`
	Skills          []string  `json:"skills,omitempty"`
	Areas           Leases    `json:"areas,omitempty"`
	Leases          Leases    `json:"leases,omitempty"`
	State           string    `json:"state"`
	IdleSince       time.Time `json:"idle_since,omitempty"`
	ContextTokens   int64     `json:"context_tokens,omitempty"`
	CompactedTokens int64     `json:"compacted_tokens,omitempty"`
}

// SchedulingPolicy bounds worker capacity and scaling proposals.
type SchedulingPolicy struct {
	MinWorkers   int               `json:"min_workers"`
	MaxWorkers   int               `json:"max_workers"`
	Approval     string            `json:"approval,omitempty"`
	Cooldown     time.Duration     `json:"cooldown"`
	Models       map[string]string `json:"models,omitempty"`
	ContextLimit int64             `json:"context_limit,omitempty"`
	StallAfter   time.Duration     `json:"stall_after,omitempty"`
}

// SchedulingInput is an immutable snapshot; Now and LastScale make time explicit.
type SchedulingInput struct {
	Board     *TaskBoard
	Workers   []SchedulerWorker
	Policy    SchedulingPolicy
	Now       time.Time
	LastScale time.Time
	ReadOnly  bool
}

// TaskAssignment proposes an assignment without modifying a board or worker.
type TaskAssignment struct {
	Task   string `json:"task"`
	Worker string `json:"worker"`
}

// WorkerProposal requests capacity for a task or to satisfy the minimum count.
type WorkerProposal struct {
	Task             string   `json:"task,omitempty"`
	Role             string   `json:"role,omitempty"`
	Skills           []string `json:"skills,omitempty"`
	Paths            Leases   `json:"paths,omitempty"`
	Model            string   `json:"model,omitempty"`
	RequiresApproval bool     `json:"requires_approval"`
}

// SchedulingPlan is advisory; integration must persist approved actions and reconcile sessions.
type SchedulingPlan struct {
	Desired     int               `json:"desired"`
	Assignments []TaskAssignment  `json:"assignments"`
	Proposals   []WorkerProposal  `json:"proposals"`
	Retire      []string          `json:"retire"`
	Replace     []string          `json:"replace"`
	Blocked     map[string]string `json:"blocked"`
	Reason      string            `json:"reason,omitempty"`
}

func (p *SchedulingPolicy) Validate() error {
	if p.MinWorkers < 0 || p.MaxWorkers < p.MinWorkers || p.Cooldown < 0 {
		return fmt.Errorf("scheduling policy needs 0 <= min <= max and nonnegative cooldown")
	}
	if p.ContextLimit < 0 || p.StallAfter < 0 {
		return fmt.Errorf("scheduling context limit and stall threshold must be nonnegative")
	}
	if p.Approval != "" && p.Approval != "ask" && p.Approval != "auto" && p.Approval != "off" {
		return fmt.Errorf("scheduling approval must be ask, auto or off")
	}
	return nil
}

func (input *SchedulingInput) validate() error {
	if input == nil {
		return fmt.Errorf("scheduling input is required")
	}
	if err := input.Board.Validate(); err != nil {
		return err
	}
	if err := input.Policy.Validate(); err != nil {
		return err
	}
	if input.Now.IsZero() {
		return fmt.Errorf("scheduler requires an explicit current time")
	}
	names := make(map[string]bool)
	for i := range input.Workers {
		worker := &input.Workers[i]
		if worker.Name == "" || names[worker.Name] {
			return fmt.Errorf("scheduler worker names must be nonempty and unique")
		}
		names[worker.Name] = true
		if err := worker.validatePaths(); err != nil {
			return err
		}
		switch worker.State {
		case "idle", "busy", "waiting", "stalled", "context-full", "offline":
		default:
			return fmt.Errorf("unknown worker state %q", worker.State)
		}
	}
	assigned := make(map[string]bool)
	for i := range input.Board.Tasks {
		task := &input.Board.Tasks[i]
		if !TaskActive(task) {
			continue
		}
		if !names[task.Assignee] {
			continue
		}
		if assigned[task.Assignee] {
			return fmt.Errorf("assigned tasks require distinct current workers")
		}
		assigned[task.Assignee] = true
	}
	return nil
}

func PlanSchedule(input *SchedulingInput) (SchedulingPlan, error) {
	if err := input.validate(); err != nil {
		return SchedulingPlan{}, err
	}
	plan := SchedulingPlan{Blocked: make(map[string]string)}
	for i := range input.Workers {
		if input.Workers[i].State != "offline" {
			plan.Desired++
		}
	}
	if input.ReadOnly {
		plan.Reason = "scheduling is disabled"
		return plan, nil
	}
	ready := input.ready(&plan)
	reserved := make(map[string]bool)
	var selected []Task
	for i := range ready {
		task := &ready[i]
		if tasksContend(task, selected) {
			plan.Blocked[task.ID] = "path contention or serial task"
			continue
		}
		worker := input.fitWorker(task, reserved)
		if worker == "" && task.Assignee != "" {
			plan.Blocked[task.ID] = input.assigneeRejection(task)
			continue
		}
		if worker == "" {
			if input.leaseContention(task, "") {
				plan.Blocked[task.ID] = "active lease contention"
				continue
			}
			if !input.propose(&plan, task) {
				continue
			}
		} else {
			plan.Assignments = append(plan.Assignments, TaskAssignment{Task: task.ID, Worker: worker})
			reserved[worker] = true
		}
		selected = append(selected, *task)
	}
	input.scale(&plan, reserved)
	return plan, nil
}

func (input *SchedulingInput) ready(plan *SchedulingPlan) []Task {
	var ready []Task
	for i := range input.Board.Tasks {
		task := &input.Board.Tasks[i]
		if !TaskPending(task) {
			continue
		}
		if reason := input.blocked(task); reason != "" {
			plan.Blocked[task.ID] = reason
			continue
		}
		ready = append(ready, cloneTask(task))
	}
	return ready
}

func (input *SchedulingInput) blocked(task *Task) string {
	if !TaskSchedulable(task) {
		return "epic is planning-only; schedule implementation tasks"
	}
	for _, dependency := range task.Dependencies {
		for i := range input.Board.Tasks {
			other := &input.Board.Tasks[i]
			if other.ID == dependency && !TaskAccepted(other) {
				return "dependencies are not done"
			}
		}
	}
	for i := range input.Board.Tasks {
		other := &input.Board.Tasks[i]
		if TaskActive(other) && (!task.Estimate.Parallel || !other.Estimate.Parallel ||
			pathsContend(task.Paths, other.Paths)) {
			return "active task contention"
		}
	}
	return ""
}

func (input *SchedulingInput) leaseContention(task *Task, candidate string) bool {
	for i := range input.Workers {
		worker := &input.Workers[i]
		if worker.Name != candidate && input.assigned(worker.Name) && pathsContend(task.Paths, worker.Leases) {
			return true
		}
	}
	return false
}

func pathsContend(left, right Leases) bool {
	for _, a := range left {
		for _, b := range right {
			a, b := leasePrefix(a), leasePrefix(b)
			if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
				return true
			}
		}
	}
	return false
}

func tasksContend(task *Task, selected []Task) bool {
	for i := range selected {
		other := &selected[i]
		if !task.Estimate.Parallel || !other.Estimate.Parallel || pathsContend(task.Paths, other.Paths) {
			return true
		}
	}
	return false
}

func (input *SchedulingInput) fitWorker(task *Task, reserved map[string]bool) string {
	for i := range input.Workers {
		worker := &input.Workers[i]
		if worker.State != "idle" || reserved[worker.Name] || input.assigned(worker.Name) {
			continue
		}
		if task.Assignee != "" && task.Assignee != worker.Name {
			continue
		}
		if input.leaseContention(task, worker.Name) {
			continue
		}
		if workerCompatibility(task, worker) != "" {
			continue
		}
		return worker.Name
	}
	return ""
}

func (input *SchedulingInput) assigneeRejection(task *Task) string {
	for i := range input.Workers {
		worker := &input.Workers[i]
		if worker.Name == task.Assignee {
			if reason := workerCompatibility(task, worker); reason != "" {
				return "assignee " + worker.Name + ": " + reason
			}
			return "waiting for " + worker.Name
		}
	}
	return "assignee " + task.Assignee + " is absent from the bound worker snapshot"
}

func (input *SchedulingInput) assigned(worker string) bool {
	return slices.ContainsFunc(input.Board.Tasks, func(task Task) bool {
		return TaskActive(&task) && task.Assignee == worker
	})
}
