package team

import "testing"

func TestWorkflowSchedulerAssignsReadyButNotBacklog(t *testing.T) {
	board, m := workflowFixture(t)
	input := &SchedulingInput{
		Board: board, Now: m.Now, Policy: SchedulingPolicy{MaxWorkers: 1, Approval: "off"},
		Workers: []SchedulerWorker{{Name: "worker", Role: "worker", State: "idle"}},
	}
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 0 {
		t.Fatal("scheduled backlog", plan, err)
	}
	if err := board.MoveTask("task", "ready", m); err != nil {
		t.Fatal(err)
	}
	plan, err = PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 {
		t.Fatal("ready task not schedulable", plan, err)
	}
	if err := recordTaskAssignmentAs(board, "task", "worker", TaskActor{User: true}); err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].State != "in_progress" || board.Tasks[0].Assignee != "worker" || len(board.Tasks[0].History) < 3 {
		t.Fatal("assignment bypassed workflow", board.Tasks[0])
	}
	if err := board.Validate(); err != nil {
		t.Fatal(err)
	}
}
