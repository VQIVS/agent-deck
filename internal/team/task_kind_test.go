package team

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReadyEpicDoesNotAssignOrScale(t *testing.T) {
	epic := schedulerTask("T-14", "epic/**")
	epic.Title, epic.Kind = "User-designed board workflow", TaskKindEpic
	implementation := schedulerTask("T-23", "implementation/**")
	implementation.Title = "Epic sounding title remains ordinary implementation"
	input := schedulerInput(epic, implementation)
	input.Workers = []SchedulerWorker{{Name: "worker", Role: "worker", State: "idle"}}
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 1 || plan.Assignments[0].Task != "T-23" || plan.Blocked["T-14"] == "" {
		t.Fatal("epic ready assignment regression", plan, err)
	}
	input = schedulerInput(epic)
	plan, err = PlanSchedule(input)
	if err != nil || len(plan.Assignments) != 0 || len(plan.Proposals) != 0 {
		t.Fatal("epic scaled pool", plan, err)
	}
	if input.Board.Tasks[0].State != "ready" {
		t.Fatal("planning fabricated completion")
	}
}

func TestEpicClassificationEditPersistenceAndAutomationGuard(t *testing.T) {
	board := TaskBoard{Version: 2}
	now := time.Now().UTC()
	user := TaskMutation{Actor: TaskActor{User: true}, Now: now}
	id, err := board.PlanTask(&TaskSpec{
		Title: "Parent", Kind: TaskKindEpic, Paths: []string{"parent/**"},
		Criteria: []string{"accept"}, Ready: true,
	}, user)
	if err != nil {
		t.Fatal(err)
	}
	if err := board.SetAssignee(id, "worker", user); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask(id, "in_progress", TaskMutation{Actor: AutomationActor, Now: now}); err == nil {
		t.Fatal("automation started epic")
	}
	if board.Tasks[0].State != "ready" || !board.Tasks[0].CompletedAt.IsZero() {
		t.Fatal("epic state rewritten")
	}
	data, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	var restored TaskBoard
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Tasks[0].Kind != TaskKindEpic || TaskSchedulable(&restored.Tasks[0]) {
		t.Fatal("epic hidden after reload")
	}
	if err := restored.EditTask(&TaskSpec{ID: id, Kind: TaskKindTask}, user); err != nil {
		t.Fatal(err)
	}
	if !TaskSchedulable(&restored.Tasks[0]) {
		t.Fatal("explicit implementation conversion ignored")
	}
	if err := restored.EditTask(&TaskSpec{ID: id, Kind: "unknown"}, user); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

func TestTaskKindCompatibilityDefaults(t *testing.T) {
	legacy := schedulerTask("old", "old/**")
	if !TaskSchedulable(&legacy) {
		t.Fatal("omitted kind changed old task behavior")
	}
	board := TaskBoard{Version: 2, Tasks: []Task{legacy}}
	if err := board.Validate(); err != nil {
		t.Fatal(err)
	}
	if !TaskSchedulable(&board.Tasks[0]) || board.Tasks[0].State != "ready" {
		t.Fatal("validation changed default", board)
	}
}
