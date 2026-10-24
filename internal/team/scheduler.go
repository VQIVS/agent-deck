package team

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// SchedulerWorker is an observed worker, including active ownership and idle age.
type SchedulerWorker struct {
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Skills    []string  `json:"skills,omitempty"`
	Leases    Leases    `json:"leases,omitempty"`
	State     string    `json:"state"`
	IdleSince time.Time `json:"idle_since,omitempty"`
}

// SchedulingPolicy bounds proposals; zero budgets mean no configured budget limit.
type SchedulingPolicy struct {
	MinWorkers  int               `json:"min_workers"`
	MaxWorkers  int               `json:"max_workers"`
	Approval    string            `json:"approval,omitempty"`
	Cooldown    time.Duration     `json:"cooldown"`
	TokenBudget int64             `json:"token_budget,omitempty"`
	CostBudget  float64           `json:"cost_budget,omitempty"`
	Models      map[string]string `json:"models,omitempty"`
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

func (p SchedulingPolicy) Validate() error {
	if p.MinWorkers < 0 || p.MaxWorkers < p.MinWorkers || p.Cooldown < 0 {
		return fmt.Errorf("scheduling policy needs 0 <= min <= max and nonnegative cooldown")
	}
	if p.TokenBudget < 0 || invalidCost(p.CostBudget) {
		return fmt.Errorf("scheduling budgets must be finite and nonnegative")
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
		if err := worker.Leases.Validate(); err != nil {
			return fmt.Errorf("worker %q leases: %w", worker.Name, err)
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
		if task.State != "assigned" {
			continue
		}
		if !names[task.Worker] || assigned[task.Worker] {
			return fmt.Errorf("assigned tasks require distinct current workers")
		}
		assigned[task.Worker] = true
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
	tokens, cost := input.committedUsage()
	for i := range ready {
		task := &ready[i]
		if tasksContend(task, selected) {
			plan.Blocked[task.ID] = "path contention or serial task"
			continue
		}
		worker := input.fitWorker(task, reserved)
		if worker == "" {
			if input.leaseContention(task, "") {
				plan.Blocked[task.ID] = "active lease contention"
				continue
			}
			if !input.propose(&plan, task, &tokens, &cost) {
				continue
			}
		} else {
			plan.Assignments = append(plan.Assignments, TaskAssignment{Task: task.ID, Worker: worker})
			reserved[worker] = true
			tokens = saturatingTokens(tokens, task.Estimate.Tokens)
			cost += task.Estimate.Cost
		}
		selected = append(selected, *task)
	}
	input.scale(&plan, reserved, tokens, cost)
	return plan, nil
}

func (input *SchedulingInput) ready(plan *SchedulingPlan) []Task {
	var ready []Task
	for i := range input.Board.Tasks {
		task := &input.Board.Tasks[i]
		if task.State != "pending" {
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
	for _, dependency := range task.Dependencies {
		for i := range input.Board.Tasks {
			other := &input.Board.Tasks[i]
			if other.ID == dependency && other.State != "done" {
				return "dependencies are not done"
			}
		}
	}
	for i := range input.Board.Tasks {
		other := &input.Board.Tasks[i]
		if other.State == "assigned" && (!task.Estimate.Parallel || !other.Estimate.Parallel ||
			pathsContend(task.Paths, other.Paths)) {
			return "active task contention"
		}
	}
	return ""
}

func (input *SchedulingInput) leaseContention(task *Task, candidate string) bool {
	for i := range input.Workers {
		worker := &input.Workers[i]
		if worker.Name != candidate && pathsContend(task.Paths, worker.Leases) {
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
		if input.leaseContention(task, worker.Name) {
			continue
		}
		if task.Role != "" && task.Role != worker.Role {
			continue
		}
		if slices.ContainsFunc(task.Skills, func(skill string) bool { return !slices.Contains(worker.Skills, skill) }) {
			continue
		}
		return worker.Name
	}
	return ""
}

func (input *SchedulingInput) assigned(worker string) bool {
	return slices.ContainsFunc(input.Board.Tasks, func(task Task) bool {
		return task.State == "assigned" && task.Worker == worker
	})
}

func (b *TaskBoard) Assign(id, worker string, input *SchedulingInput) error {
	if input == nil || input.Board != b {
		return fmt.Errorf("assignment requires the current board snapshot")
	}
	plan, err := PlanSchedule(input)
	if err != nil {
		return err
	}
	if !slices.Contains(plan.Assignments, TaskAssignment{Task: id, Worker: worker}) {
		return fmt.Errorf("assignment is not currently schedulable")
	}
	for i := range b.Tasks {
		if b.Tasks[i].ID == id {
			b.Tasks[i].State, b.Tasks[i].Worker = "assigned", worker
			return nil
		}
	}
	return fmt.Errorf("unknown task %q", id)
}
