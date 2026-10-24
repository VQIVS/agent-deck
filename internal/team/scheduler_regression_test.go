package team

import (
	"testing"
	"time"
)

func TestSchedulerCandidateLeaseAndOfflineCapacity(t *testing.T) {
	input := schedulerInput(schedulerTask("own", "own/**"))
	input.Workers = []SchedulerWorker{{
		Name: "candidate", State: "idle", Leases: Leases{"own/**"},
		IdleSince: input.Now.Add(-time.Hour),
	}}
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 || len(plan.Retire) != 0 {
		t.Fatalf("own lease blocked candidate: %+v %v", plan, err)
	}
	input.Workers = []SchedulerWorker{{Name: "dead", State: "offline"}}
	input.Policy.MaxWorkers = 1
	plan, err = PlanSchedule(input)
	if err != nil || len(plan.Proposals) != 1 || plan.Desired != 1 || len(plan.Replace) != 1 {
		t.Fatalf("offline worker consumed capacity: %+v %v", plan, err)
	}
}

func TestSchedulerRejectedHeadDoesNotReserve(t *testing.T) {
	for _, gate := range []string{"off", "cooldown", "maximum", "budget"} {
		t.Run(gate, func(t *testing.T) {
			head, next := schedulerTask("head", "shared/**"), schedulerTask("next", "shared/file")
			head.Role, head.Estimate.Parallel, head.Estimate.Tokens = "missing", false, 101
			input := schedulerInput(head, next)
			input.Workers = []SchedulerWorker{{Name: "available", State: "idle", Role: "coder"}}
			switch gate {
			case "off":
				input.Policy.Approval = "off"
			case "cooldown":
				input.LastScale = input.Now
			case "maximum":
				input.Policy.MaxWorkers = 1
			case "budget":
				input.Policy.TokenBudget = 100
			}
			plan, err := PlanSchedule(input)
			if err != nil || len(plan.Proposals) != 0 || len(plan.Assignments) != 1 ||
				plan.Assignments[0].Task != "next" {
				t.Fatalf("rejected head blocked runnable task: %+v %v", plan, err)
			}
			input.ReadOnly = true
			plan, err = PlanSchedule(input)
			if err != nil || len(plan.Assignments)+len(plan.Proposals)+len(plan.Retire)+len(plan.Replace) != 0 {
				t.Fatal("read-only emitted actions")
			}
		})
	}
}

func TestSchedulerBudgetsIncrementallyReserve(t *testing.T) {
	for _, estimates := range [][]int64{{60, 60}, {1, 1, 90}} {
		tasks := make([]Task, 0, len(estimates))
		for i, tokens := range estimates {
			id := string(rune('a' + i))
			task := schedulerTask(id, id+"/**")
			task.Estimate.Tokens, task.Estimate.Cost = tokens, float64(tokens)
			tasks = append(tasks, task)
		}
		input := schedulerInput(tasks...)
		input.Policy.TokenBudget, input.Policy.CostBudget = 100, 100
		plan, err := PlanSchedule(input)
		want := 1
		if estimates[0] == 1 {
			want = 3
		}
		if err != nil || len(plan.Proposals) != want {
			t.Fatalf("incremental budget %v: %+v %v", estimates, plan, err)
		}
	}
	assigned := schedulerTask("assigned", "assigned/**")
	assigned.State, assigned.Worker, assigned.Estimate.Tokens = "assigned", "busy", 50
	pending := schedulerTask("pending", "pending/**")
	pending.Estimate.Tokens = 60
	input := schedulerInput(assigned, pending)
	input.Workers = []SchedulerWorker{{Name: "busy", State: "busy"}}
	input.Policy.TokenBudget = 100
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Proposals) != 0 {
		t.Fatalf("assigned reservation ignored: %+v %v", plan, err)
	}
	input.Board.Tasks[0].State = "done"
	input.Board.Tasks[0].Report = &TaskReport{Text: "finished"}
	input.Board.Tasks[0].Measured = &TaskUsage{Tokens: 20, Source: "server"}
	plan, err = PlanSchedule(input)
	if err != nil || len(plan.Proposals) != 1 {
		t.Fatalf("measured usage did not replace estimate: %+v %v", plan, err)
	}
}

func TestFinishRequiresAuthenticatedAssignedWorker(t *testing.T) {
	task := schedulerTask("task", "task/**")
	task.State, task.Worker = "assigned", "owner"
	b := TaskBoard{Version: 1, Tasks: []Task{task}}
	for _, worker := range []string{"", "forged"} {
		if err := b.Finish("task", worker, TaskReport{Text: "done"}, false, false); err == nil ||
			b.Tasks[0].State != "assigned" {
			t.Fatal("unauthorized worker finished task")
		}
	}
	if err := b.Finish("task", "owner", TaskReport{Text: "done"}, false, false); err != nil {
		t.Fatal(err)
	}
}
