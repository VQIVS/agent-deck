package team

import "slices"

func (input *SchedulingInput) scale(plan *SchedulingPlan, reserved map[string]bool, tokens int64, cost float64) {
	for i := range input.Workers {
		worker := &input.Workers[i]
		if worker.State == "stalled" || worker.State == "context-full" || worker.State == "offline" {
			plan.Replace = append(plan.Replace, worker.Name)
		}
	}
	if input.Policy.Approval == "off" {
		input.retire(plan, reserved)
		return
	}
	if input.coolingDown() {
		plan.Reason = "scaling cooldown"
		return
	}
	for plan.Desired < input.Policy.MinWorkers && input.withinBudget(tokens, cost) {
		plan.Proposals = append(plan.Proposals, WorkerProposal{
			RequiresApproval: input.Policy.Approval != "auto",
		})
		plan.Desired++
	}
	if input.hasRunnablePending() || len(plan.Assignments) != 0 || len(plan.Proposals) != 0 {
		return
	}
	input.retire(plan, reserved)
}

func (input *SchedulingInput) hasRunnablePending() bool {
	for i := range input.Board.Tasks {
		task := &input.Board.Tasks[i]
		if task.State == "pending" && !input.failedDependency(task, map[string]bool{}) {
			return true
		}
	}
	return false
}

func (input *SchedulingInput) failedDependency(task *Task, visited map[string]bool) bool {
	if visited[task.ID] {
		return false
	}
	visited[task.ID] = true
	for _, dependency := range task.Dependencies {
		for i := range input.Board.Tasks {
			other := &input.Board.Tasks[i]
			if other.ID == dependency && (other.State == "failed" || input.failedDependency(other, visited)) {
				return true
			}
		}
	}
	return false
}

func (input *SchedulingInput) coolingDown() bool {
	return !input.LastScale.IsZero() && input.Now.Sub(input.LastScale) < input.Policy.Cooldown
}

func (input *SchedulingInput) propose(plan *SchedulingPlan, task *Task, tokens *int64, cost *float64) bool {
	reason := ""
	switch {
	case input.Policy.Approval == "off":
		reason = "scaling is disabled"
	case input.coolingDown():
		reason = "scaling cooldown"
	case plan.Desired >= input.Policy.MaxWorkers:
		reason = "maximum worker count"
	case !input.fitsBudget(*tokens, *cost, task):
		reason = "usage budget exhausted"
	}
	if reason != "" {
		plan.Blocked[task.ID] = reason
		return false
	}
	plan.Proposals = append(plan.Proposals, WorkerProposal{
		Task: task.ID, Role: task.Role, Skills: task.Skills, Paths: task.Paths,
		Model: input.Policy.Models[task.Role], RequiresApproval: input.Policy.Approval != "auto",
	})
	plan.Desired++
	*tokens = saturatingTokens(*tokens, task.Estimate.Tokens)
	*cost += task.Estimate.Cost
	return true
}

func (input *SchedulingInput) fitsBudget(tokens int64, cost float64, task *Task) bool {
	return input.withinBudget(tokens, cost) && (input.Policy.TokenBudget == 0 ||
		(tokens <= input.Policy.TokenBudget && task.Estimate.Tokens <= input.Policy.TokenBudget-tokens)) &&
		(input.Policy.CostBudget == 0 || cost+task.Estimate.Cost <= input.Policy.CostBudget)
}

func (input *SchedulingInput) retire(plan *SchedulingPlan, reserved map[string]bool) {
	for i := range input.Workers {
		worker := &input.Workers[i]
		if plan.Desired <= input.Policy.MinWorkers {
			break
		}
		if worker.State != "idle" || reserved[worker.Name] || input.assigned(worker.Name) ||
			worker.IdleSince.IsZero() || input.Now.Sub(worker.IdleSince) < input.Policy.Cooldown {
			continue
		}
		plan.Retire = append(plan.Retire, worker.Name)
		plan.Desired--
	}
}

func (input *SchedulingInput) committedUsage() (tokens int64, cost float64) {
	for i := range input.Board.Tasks {
		task := &input.Board.Tasks[i]
		if task.Measured != nil {
			tokens = saturatingTokens(tokens, task.Measured.Tokens)
			cost += task.Measured.Cost
		} else if task.State == "assigned" || task.State == "done" || task.State == "failed" {
			// Unmeasured work reserves its estimate; reports never count as measurements.
			tokens = saturatingTokens(tokens, task.Estimate.Tokens)
			cost += task.Estimate.Cost
		}
	}
	return tokens, cost
}

func saturatingTokens(total, addition int64) int64 {
	const maxTokens = int64(1<<63 - 1)
	if maxTokens-total < addition {
		return maxTokens
	}
	return total + addition
}

func (input *SchedulingInput) withinBudget(tokens int64, cost float64) bool {
	return (input.Policy.TokenBudget == 0 || tokens < input.Policy.TokenBudget) &&
		(input.Policy.CostBudget == 0 || cost < input.Policy.CostBudget)
}

// ScalingApproved checks the current policy gate for a proposed capacity increase.
// Call again immediately before session creation; approval does not bypass other limits.
func ScalingApproved(input *SchedulingInput, proposal *WorkerProposal, approved bool) bool {
	if proposal == nil {
		return false
	}
	plan, err := PlanSchedule(input)
	if err != nil || input.ReadOnly || input.Policy.Approval == "off" {
		return false
	}
	for i := range plan.Proposals {
		current := &plan.Proposals[i]
		if current.Task == proposal.Task && current.Role == proposal.Role && current.Model == proposal.Model &&
			slices.Equal(current.Paths, proposal.Paths) && slices.Equal(current.Skills, proposal.Skills) {
			return input.Policy.Approval == "auto" || approved
		}
	}
	return false
}

// RetirementApproved revalidates idle age and current capacity before a retirement.
func RetirementApproved(input *SchedulingInput, worker string) bool {
	plan, err := PlanSchedule(input)
	if err != nil {
		return false
	}
	for _, name := range plan.Retire {
		if name == worker {
			return true
		}
	}
	return false
}
