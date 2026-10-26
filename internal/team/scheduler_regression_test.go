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
	for _, gate := range []string{"off", "cooldown", "maximum"} {
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
