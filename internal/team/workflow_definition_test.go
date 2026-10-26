package team

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func customWorkflow() WorkflowDefinition {
	w := DefaultWorkflow()
	w.Transitions = map[string]WorkflowTransition{}
	for i := range w.Stages {
		w.Stages[i].Name = "custom-" + w.Stages[i].Category
	}
	w.Transitions["custom-shipped"] = WorkflowTransition{
		From: []string{"custom-done"}, By: []string{"release-manager", "user"},
	}
	return w
}

func TestCustomWorkflowLifecycleAndRelease(t *testing.T) {
	board := TaskBoard{Version: 2, Workflow: customWorkflow()}
	now := time.Now().UTC()
	lead := TaskMutation{Actor: TaskActor{Member: "lead", Role: "master"}, Now: now}
	worker := TaskMutation{Actor: TaskActor{Member: "worker", Role: "worker"}, Now: now}
	reviewer := TaskMutation{Actor: TaskActor{Member: "qa", Role: "worker"}, Now: now.Add(time.Minute)}
	task := Task{
		ID: "task", Title: "Custom work", Paths: Leases{"src/**"},
		Criteria: []TaskCriterion{{ID: "c1", Text: "works"}}, Assignee: "worker", Reviewer: "qa",
	}
	if err := board.CreateTask(&task, lead); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"custom-ready", "custom-active"} {
		if err := board.MoveTask("task", stage, lead); err != nil {
			t.Fatal(err)
		}
	}
	if !TaskActive(&board.Tasks[0]) {
		t.Fatal("custom active not owning work")
	}
	if err := board.MoveTask("task", "custom-done", lead); err == nil {
		t.Fatal("bypassed completion evidence")
	}
	if err := board.MoveTask("task", "custom-review", worker); err != nil {
		t.Fatal(err)
	}
	if err := board.ReviewTask("task", "accept", "verified", reviewer); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	var restored TaskBoard
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatal("custom board restart", err)
	}
	board = restored
	completed := board.Tasks[0].CompletedAt
	if err := board.MoveTask("task", "custom-shipped", lead); err == nil {
		t.Fatal("lead shipped")
	}
	release := TaskMutation{Actor: TaskActor{Member: "release-manager", Role: "worker"}, Now: now.Add(2 * time.Minute)}
	if err := board.MoveTask("task", "custom-shipped", release); err == nil {
		t.Fatal("missing release version accepted")
	}
	if err := board.MoveTaskRelease("task", "custom-shipped", &TaskRelease{Version: "v1.2.3"}, release); err != nil {
		t.Fatal(err)
	}
	if !TaskAccepted(&board.Tasks[0]) || !TaskShipped(&board.Tasks[0]) ||
		!board.Tasks[0].CompletedAt.Equal(completed) || board.Tasks[0].ReleaseVersion != "v1.2.3" {
		t.Fatal(board.Tasks[0])
	}
	if err := board.MoveTask("task", "custom-active", lead); err == nil {
		t.Fatal("shipped reopened")
	}
	dependent := Task{
		ID: "next", Title: "Next", Paths: Leases{"next/**"}, Dependencies: []string{"task"},
		Criteria: []TaskCriterion{{ID: "c1", Text: "next"}},
	}
	if err := board.CreateTask(&dependent, lead); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask("next", "custom-ready", lead); err != nil {
		t.Fatal("shipped dependency blocked", err)
	}
	input := SchedulingInput{
		Board: &board, Policy: SchedulingPolicy{MaxWorkers: 1, Approval: "auto"}, Now: now,
		Workers: []SchedulerWorker{{Name: "worker", Role: "worker", State: "idle"}},
	}
	plan, err := PlanSchedule(&input)
	if err != nil || len(plan.Assignments) != 1 {
		t.Fatal(plan, err)
	}
}

func TestCustomWorkflowNoticeAndReviewRules(t *testing.T) {
	board := TaskBoard{Version: 2, Workflow: customWorkflow()}
	now := time.Now().UTC()
	m := TaskMutation{Actor: TaskActor{User: true}, Now: now}
	task := Task{
		ID: "custom", Title: "custom notices", Assignee: "worker", Reviewer: "qa",
		Paths: Leases{"src/**"}, Criteria: []TaskCriterion{{ID: "c1", Text: "works"}},
	}
	if err := board.CreateTask(&task, m); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"custom-ready", "custom-active"} {
		if err := board.MoveTask(task.ID, state, m); err != nil {
			t.Fatal(err)
		}
	}
	before := cloneBoard(&board)
	if err := board.MoveTask(task.ID, "custom-review", m); err != nil {
		t.Fatal(err)
	}
	configured := Team{Members: []Member{{Name: "lead", Role: "master"}, {Name: "worker"}, {Name: "qa"}}}
	notices := TaskNotices(&before, &board, &configured, m.Actor)
	if len(notices) < 2 {
		t.Fatal("custom review did not notify reviewer and lead", notices)
	}
	board.Workflow.Transitions["custom-done"] = WorkflowTransition{From: []string{"custom-review"}, By: []string{"user"}}
	reviewer := TaskMutation{Actor: TaskActor{Member: "qa", Role: "worker"}, Now: now}
	if err := board.ReviewTask(task.ID, "accept", "checked", reviewer); err == nil {
		t.Fatal("review bypassed explicit actor constraint")
	}
}

func TestWorkflowCompatibilityAndOccupiedEdits(t *testing.T) {
	board, mutation := workflowFixture(t)
	before := cloneTask(&board.Tasks[0])
	if err := board.Validate(); err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(board)
	if err := board.Validate(); err != nil {
		t.Fatal(err)
	}
	second, _ := json.Marshal(board)
	if !bytes.Equal(first, second) || !reflect.DeepEqual(before.History, board.Tasks[0].History) {
		t.Fatal("validation changed task history")
	}
	if board.Category("released") != "shipped" {
		t.Fatal("default shipment missing")
	}
	w := DefaultWorkflow()
	w.Stages[0].Category = "ready"
	if err := board.SetWorkflow(w, mutation); err == nil {
		t.Fatal("occupied category changed")
	}
}

func TestWorkflowValidationAndExplicitRules(t *testing.T) {
	for _, change := range []func(*WorkflowDefinition){
		func(w *WorkflowDefinition) { w.Stages[0].Name = w.Stages[1].Name },
		func(w *WorkflowDefinition) { w.Stages[0].Color = "red" },
		func(w *WorkflowDefinition) { w.Stages[0].Category = "invented" },
		func(w *WorkflowDefinition) {
			w.Transitions["unknown"] = WorkflowTransition{From: []string{"done"}, By: []string{"user"}}
		},
	} {
		w := DefaultWorkflow()
		change(&w)
		if err := w.Validate(); err == nil {
			t.Fatal("invalid workflow accepted")
		}
	}
	board, mutation := workflowFixture(t)
	board.Workflow = DefaultWorkflow()
	board.Workflow.Transitions["ready"] = WorkflowTransition{From: []string{"backlog"}, By: []string{"user"}}
	if err := board.MoveTask("task", "ready", mutation); err == nil {
		t.Fatal("explicit actor rule ignored")
	}
	mutation.Actor = TaskActor{User: true}
	if err := board.MoveTask("task", "ready", mutation); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowOccupiedChangesAndAtomicFailure(t *testing.T) {
	board, mutation := workflowFixture(t)
	before, _ := json.Marshal(board)
	w := DefaultWorkflow()
	w.Stages = w.Stages[1:]
	w.Stages = append(w.Stages, WorkflowStage{Name: "new-todo", Color: "#112233", Category: "todo"})
	if err := board.SetWorkflow(w, mutation); err == nil {
		t.Fatal("populated stage removed")
	}
	after, _ := json.Marshal(board)
	if !bytes.Equal(before, after) {
		t.Fatal("failed edit mutated board")
	}
	w = DefaultWorkflow()
	w.Stages[1], w.Stages[2] = w.Stages[2], w.Stages[1]
	if err := board.SetWorkflow(w, mutation); err != nil {
		t.Fatal("safe reorder rejected", err)
	}
	w.Stages[0].Color = "#000000"
	if board.Workflow.Stages[0].Color == "#000000" {
		t.Fatal("stored workflow aliases caller mutation")
	}
}
