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
	return Task{ID: id, Title: id, Paths: Leases{path}, State: "pending", Estimate: TaskEstimate{Parallel: true}}
}

func schedulerInput(tasks ...Task) *SchedulingInput {
	return &SchedulingInput{
		Board:  &TaskBoard{Version: 1, Tasks: tasks},
		Policy: SchedulingPolicy{MaxWorkers: 4, Cooldown: time.Minute},
		Now:    time.Unix(10000, 0),
	}
}

func TestSchedulerDependencyAndLeaseContention(t *testing.T) {
	a := schedulerTask("a", "internal/a/**")
	b := schedulerTask("b", "internal/b/**")
	b.Dependencies = []string{"a"}
	input := schedulerInput(a, b)
	input.Workers = []SchedulerWorker{{Name: "coder", State: "idle"}}
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 || plan.Blocked["b"] == "" {
		t.Fatalf("dependency plan: %+v, %v", plan, err)
	}
	input.Workers[0].Leases = Leases{"internal/a/*"}
	plan, err = PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 0 || len(plan.Proposals) != 0 || plan.Blocked["a"] == "" {
		t.Fatalf("lease contention should serialize, not scale: %+v, %v", plan, err)
	}
	input.Board.Tasks[0].State, input.Board.Tasks[0].Worker = "done", "old"
	input.Board.Tasks[0].Report = &TaskReport{Text: "done"}
	input.Workers[0].Leases = nil
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
			proposal := &plan.Proposals[0]
			if ScalingApproved(input, proposal, false) != (mode == "auto") || !ScalingApproved(input, proposal, true) {
				t.Fatal("approval policy bypassed")
			}
			proposal.Paths = Leases{"outside/**"}
			if ScalingApproved(input, proposal, true) {
				t.Fatal("forged proposal paths accepted")
			}
		})
	}
	input := schedulerInput(schedulerTask("a", "a/**"), schedulerTask("b", "b/**"))
	input.Policy.MaxWorkers = 1
	plan, _ := PlanSchedule(input)
	if len(plan.Proposals) != 1 || plan.Blocked["b"] == "" {
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

func TestSchedulerBudgetUsesMeasurementsNotReports(t *testing.T) {
	a := schedulerTask("a", "a/**")
	a.Estimate.Tokens, a.Estimate.Cost = 100, 2
	input := schedulerInput(a)
	input.Policy.TokenBudget = 100
	plan, _ := PlanSchedule(input)
	if len(plan.Proposals) != 0 {
		t.Fatal("token budget ignored")
	}
	input.Policy.TokenBudget, input.Policy.CostBudget = 0, 2
	plan, _ = PlanSchedule(input)
	if len(plan.Proposals) != 0 {
		t.Fatal("cost budget ignored")
	}
	input.Board.Tasks[0].State, input.Board.Tasks[0].Worker = "done", "old"
	input.Board.Tasks[0].Report = &TaskReport{Text: "claimed zero cost"}
	input.Board.Tasks = append(input.Board.Tasks, schedulerTask("b", "b/**"))
	plan, _ = PlanSchedule(input)
	if len(plan.Proposals) != 0 {
		t.Fatal("reported usage bypassed reservation")
	}
	if err := input.Board.RecordUsage("a", TaskUsage{Cost: 0.5, Source: "server"}, false); err != nil {
		t.Fatal(err)
	}
	plan, _ = PlanSchedule(input)
	if len(plan.Proposals) != 1 || input.Board.Tasks[0].Estimate.Cost != 2 {
		t.Fatal("measured usage not separate from estimate")
	}
	if saturatingTokens(math.MaxInt64, 1) != math.MaxInt64 {
		t.Fatal("token total overflowed")
	}
}

func TestTaskBoardValidationAndTransitions(t *testing.T) {
	input := schedulerInput(schedulerTask("a", "a/**"))
	input.Workers = []SchedulerWorker{{Name: "coder", State: "idle"}}
	b := input.Board
	input.ReadOnly = true
	if err := b.Assign("a", "coder", input); err == nil {
		t.Fatal("read-only assignment allowed")
	}
	input.ReadOnly = false
	if err := b.Assign("a", "coder", input); err != nil {
		t.Fatal(err)
	}
	if err := b.Finish("a", TaskReport{Text: "done", Tests: "claimed pass"}, false, true); err == nil {
		t.Fatal("read-only finish allowed")
	}
	if err := b.Finish("a", TaskReport{Text: "done"}, false, false); err != nil {
		t.Fatal(err)
	}
	if b.Tasks[0].Measured != nil {
		t.Fatal("report manufactured measured usage")
	}
	if err := b.RecordUsage("a", TaskUsage{Source: "server"}, true); err == nil {
		t.Fatal("read-only measured usage allowed")
	}
	if err := b.RecordUsage("a", TaskUsage{}, false); err == nil {
		t.Fatal("missing measurement provenance accepted")
	}
	for _, change := range []func(*TaskBoard){
		func(b *TaskBoard) { b.Version = 2 },
		func(b *TaskBoard) { b.Tasks[0].Dependencies = []string{"missing"} },
		func(b *TaskBoard) { b.Tasks[0].Dependencies = []string{"a"} },
		func(b *TaskBoard) { b.Tasks[0].Paths = Leases{"../escape"} },
		func(b *TaskBoard) { b.Tasks[0].Estimate.Cost = math.NaN() },
		func(b *TaskBoard) { b.Tasks = append(b.Tasks, b.Tasks[0]) },
	} {
		board := &TaskBoard{Version: 1, Tasks: []Task{schedulerTask("a", "a/**")}}
		change(board)
		if err := board.Validate(); err == nil {
			t.Fatal("invalid board accepted")
		}
	}
}

func TestSchedulerBoardAddAndProposalRevalidation(t *testing.T) {
	input := schedulerInput()
	task := schedulerTask("a", "a/**")
	if err := input.Board.Add(&task, true); err == nil || len(input.Board.Tasks) != 0 {
		t.Fatal("read-only add mutated board")
	}
	if err := input.Board.Add(&task, false); err != nil {
		t.Fatal(err)
	}
	task.Paths[0] = "changed/**"
	if input.Board.Tasks[0].Paths[0] != "a/**" {
		t.Fatal("added task retained caller slice")
	}
	if err := input.Board.Add(&task, false); err == nil || len(input.Board.Tasks) != 1 {
		t.Fatal("invalid add was not atomic")
	}
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Proposals) != 1 {
		t.Fatalf("plan: %+v, %v", plan, err)
	}
	proposal := &plan.Proposals[0]
	input.Policy.MaxWorkers = 0
	if ScalingApproved(input, proposal, true) {
		t.Fatal("stale approval bypassed current maximum")
	}
	input.Policy.MaxWorkers = 4
	input.ReadOnly = true
	if ScalingApproved(input, proposal, true) {
		t.Fatal("approval bypassed read-only")
	}
	input.ReadOnly = false
	input.Workers = []SchedulerWorker{{Name: "coder", State: "idle"}}
	if err := input.Board.Assign("a", "coder", input); err != nil {
		t.Fatal(err)
	}
	if err := input.Board.Finish("a", TaskReport{Text: "failed"}, true, false); err != nil {
		t.Fatal(err)
	}
	if input.Board.Tasks[0].State != "failed" {
		t.Fatal("failed report became done")
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
