package team

func (input *SchedulingInput) scale(plan *SchedulingPlan, reserved map[string]bool) {
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
	for plan.Desired < input.Policy.MinWorkers {
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
		if TaskPending(task) && !input.failedDependency(task, map[string]bool{}) {
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
			if other.ID == dependency && (TaskFailed(other) || input.failedDependency(other, visited)) {
				return true
			}
		}
	}
	return false
}

func (input *SchedulingInput) coolingDown() bool {
	return !input.LastScale.IsZero() && input.Now.Sub(input.LastScale) < input.Policy.Cooldown
}

func (input *SchedulingInput) propose(plan *SchedulingPlan, task *Task) bool {
	reason := ""
	switch {
	case input.Policy.Approval == "off":
		reason = "scaling is disabled"
	case input.coolingDown():
		reason = "scaling cooldown"
	case plan.Desired >= input.Policy.MaxWorkers:
		reason = "maximum worker count"
	}
	if reason != "" {
		if mismatch := input.compatibilityRejections(task); mismatch != "" {
			reason += "; " + mismatch
		}
		plan.Blocked[task.ID] = reason
		return false
	}
	plan.Proposals = append(plan.Proposals, WorkerProposal{
		Task: task.ID, Role: task.Role, Skills: task.Skills, Paths: task.Paths,
		Model: input.Policy.Models[task.Role], RequiresApproval: input.Policy.Approval != "auto",
	})
	plan.Desired++
	return true
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
