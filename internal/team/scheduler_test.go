package team

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"
)

func schedulerTask(id, path string) Task {
	return Task{
		ID: id, Title: id, Paths: Leases{path}, State: "ready",
		Criteria: []TaskCriterion{{ID: "check", Text: "Check"}},
		Estimate: TaskEstimate{Parallel: true},
	}
}

func schedulerInput(tasks ...Task) *SchedulingInput {
	return &SchedulingInput{
		Board:  &TaskBoard{Version: 2, Tasks: tasks},
		Policy: SchedulingPolicy{MaxWorkers: 4, Cooldown: time.Minute},
		Now:    time.Unix(10000, 0),
	}
}

func TestSchedulerDependencyAndLeaseContention(t *testing.T) {
	a := schedulerTask("a", "internal/a/**")
	b := schedulerTask("b", "internal/b/**")
	b.Dependencies, b.State = []string{"a"}, "backlog"
	input := schedulerInput(a, b)
	input.Workers = []SchedulerWorker{{Name: "coder", State: "idle"}}
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 {
		t.Fatalf("dependency plan: %+v, %v", plan, err)
	}
	input.Workers[0].Leases = Leases{"internal/a/*"}
	plan, err = PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 || len(plan.Proposals) != 0 || len(plan.Retire) != 0 {
		t.Fatalf("candidate's own lease must permit assignment: %+v, %v", plan, err)
	}
	acceptSchedulerTask(&input.Board.Tasks[0])
	input.Workers[0].Leases = nil
	input.Board.Tasks[1].State = "ready"
	plan, err = PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 || plan.Assignments[0].Task != "b" {
		t.Fatalf("done dependency: %+v, %v", plan, err)
	}
}

func TestSchedulerParallelismSkillsAndPurity(t *testing.T) {
	a, b, c := schedulerTask("a", "a/**"), schedulerTask("b", "a/x.go"), schedulerTask("c", "c/**")
	a.Role, a.Skills = "tester", []string{"go"}
	input := schedulerInput(a, b, c)
	input.Workers = []SchedulerWorker{
		{Name: "wrong", State: "idle", Role: "coder"},
		{Name: "right", State: "idle", Role: "tester", Skills: []string{"go"}},
	}
	before, _ := json.Marshal(input)
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 2 || plan.Assignments[0].Worker != "right" || plan.Blocked["b"] == "" {
		t.Fatalf("fit/parallel plan: %+v, %v", plan, err)
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("planning mutated input")
	}
	input.Board.Tasks[0].Estimate.Parallel = false
	plan, err = PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 || len(plan.Proposals) != 0 {
		t.Fatalf("serial task ran in parallel: %+v, %v", plan, err)
	}
}

func TestSchedulerScalingPolicy(t *testing.T) {
	for _, mode := range []string{"", "ask", "auto", "off"} {
		t.Run("mode-"+mode, func(t *testing.T) {
			input := schedulerInput(schedulerTask("a", "a/**"))
			input.Policy.Approval = mode
			input.Policy.Models = map[string]string{"": "provider/model"}
			plan, err := PlanSchedule(input)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "off" {
				if len(plan.Proposals) != 0 {
					t.Fatal("off proposed scaling")
				}
				return
			}
			if len(plan.Proposals) != 1 || plan.Desired != 1 || plan.Proposals[0].Model != "provider/model" {
				t.Fatalf("proposal: %+v", plan)
			}
		})
	}
	input := schedulerInput(schedulerTask("a", "a/**"), schedulerTask("b", "b/**"))
	input.Policy.MaxWorkers = 1
	plan, _ := PlanSchedule(input)
	if len(plan.Proposals) != 1 {
		t.Fatalf("max ignored: %+v", plan)
	}
	input.LastScale = input.Now.Add(-time.Second)
	plan, _ = PlanSchedule(input)
	if len(plan.Proposals) != 0 || plan.Reason != "scaling cooldown" {
		t.Fatal("cooldown ignored")
	}
	input.LastScale = time.Time{}
	input.ReadOnly = true
	plan, _ = PlanSchedule(input)
	if len(plan.Proposals)+len(plan.Assignments)+len(plan.Retire)+len(plan.Replace) != 0 {
		t.Fatal("read-only emitted actions")
	}
}

func TestSchedulerMinimumRetirementAndReplacement(t *testing.T) {
	input := schedulerInput()
	input.Policy.MinWorkers = 2
	plan, err := PlanSchedule(input)
	if err != nil || plan.Desired != 2 || len(plan.Proposals) != 2 {
		t.Fatalf("minimum ignored: %+v, %v", plan, err)
	}
	input.Policy.MinWorkers = 1
	input.Workers = []SchedulerWorker{
		{Name: "old", State: "idle", IdleSince: input.Now.Add(-2 * time.Minute)},
		{Name: "young", State: "idle", IdleSince: input.Now.Add(-time.Second)},
		{Name: "busy", State: "busy"},
		{Name: "stalled", State: "stalled"},
	}
	plan, _ = PlanSchedule(input)
	if !reflect.DeepEqual(plan.Retire, []string{"old"}) || !reflect.DeepEqual(plan.Replace, []string{"stalled"}) {
		t.Fatalf("retire/replace signals: %+v", plan)
	}
	if !RetirementApproved(input, "old") || RetirementApproved(input, "young") {
		t.Fatal("retirement guard ignored idle age")
	}
}

func TestTaskBoardValidation(t *testing.T) {
	for _, change := range []func(*TaskBoard){
		func(b *TaskBoard) { b.Version = 1 },
		func(b *TaskBoard) { b.Tasks[0].Dependencies = []string{"missing"} },
		func(b *TaskBoard) { b.Tasks[0].Dependencies = []string{"a"} },
		func(b *TaskBoard) { b.Tasks[0].Paths = Leases{"../escape"} },
		func(b *TaskBoard) { b.Tasks[0].Estimate.Cost = math.NaN() },
		func(b *TaskBoard) { b.Tasks = append(b.Tasks, b.Tasks[0]) },
	} {
		board := &TaskBoard{Version: 2, Tasks: []Task{schedulerTask("a", "a/**")}}
		change(board)
		if err := board.Validate(); err == nil {
			t.Fatal("invalid board accepted")
		}
	}
}

func TestSchedulerBoardRoundTrip(t *testing.T) {
	input := schedulerInput(schedulerTask("a", "a/**"))
	if err := input.Board.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(input.Board)
	if err != nil {
		t.Fatal(err)
	}
	var restored TaskBoard
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Validate(); err != nil || !reflect.DeepEqual(input.Board, &restored) {
		t.Fatalf("board round trip: %+v, %v", restored, err)
	}
}

func TestSchedulerStartsPlannedTaskOnlyWithItsAssignee(t *testing.T) {
	board := &TaskBoard{Version: 2}
	m := TaskMutation{Actor: TaskActor{Member: "lead", Role: "master"}, Now: time.Unix(10000, 0)}
	if _, err := board.PlanTask(&TaskSpec{
		Title: "x", Criteria: []string{"a"}, Paths: []string{"x/*"},
		Ready: true, Assignee: "alice", Reviewer: "bob",
	}, m); err != nil {
		t.Fatal(err)
	}
	input := &SchedulingInput{
		Board: board, Policy: SchedulingPolicy{MaxWorkers: 4, Cooldown: time.Minute}, Now: time.Unix(10000, 0),
		Workers: []SchedulerWorker{
			{Name: "bob", Role: "worker", State: "idle"}, {Name: "alice", Role: "worker", State: "idle"},
		},
	}
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 || plan.Assignments[0].Worker != "alice" {
		t.Fatalf("plan=%+v err=%v", plan.Assignments, err)
	}
	input.Workers = input.Workers[:1]
	if plan, err = PlanSchedule(input); err != nil || len(plan.Assignments) != 0 {
		t.Fatalf("planned task went to someone else: %+v err=%v", plan.Assignments, err)
	}
}

func acceptSchedulerTask(task *Task) {
	task.State, task.Reviewer = "done", "qa"
	now := time.Unix(10000, 0).UTC()
	task.Review = &TaskReview{Reviewer: "qa", Accepted: true, At: now, Digest: taskContentDigest(task)}
	task.History = append(task.History, TaskChange{Actor: "qa", At: now, Field: "review", From: "review", To: "done"})
}
