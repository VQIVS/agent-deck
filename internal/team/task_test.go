package team

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workflowFixture(t *testing.T) (*TaskBoard, TaskMutation) {
	t.Helper()
	board := &TaskBoard{Version: 2}
	mutation := TaskMutation{Actor: TaskActor{Member: "lead", Role: "master"}, Now: time.Now().UTC()}
	task := &Task{
		ID: "task", Title: "Implement", Paths: Leases{"own/**"}, Reviewer: "reviewer",
		Criteria: []TaskCriterion{{ID: "tests", Text: "Tests pass"}}, Estimate: TaskEstimate{Duration: 10 * time.Minute},
	}
	if err := board.CreateTask(task, mutation); err != nil {
		t.Fatal(err)
	}
	return board, mutation
}

func TestTaskWorkflowAuthorizationAndReview(t *testing.T) {
	board, m := workflowFixture(t)
	if err := board.MoveTask("task", "done", m); err == nil {
		t.Fatal("backlog jumped to done")
	}
	if err := board.SetAssignee("task", "worker", m); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ready", "in_progress"} {
		if err := board.MoveTask("task", state, m); err != nil {
			t.Fatal(err)
		}
	}
	worker := m
	worker.Actor = TaskActor{Member: "other", Role: "worker"}
	if err := board.MoveTask("task", "review", worker); err == nil {
		t.Fatal("foreign worker changed task")
	}
	worker.Actor.Member = "worker"
	if err := board.UpdateTaskProgress("task", map[string]bool{"tests": true}, worker); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask("task", "done", m); err == nil {
		t.Fatal("checked criterion manufactured completion")
	}
	if err := board.MoveTask("task", "review", worker); err != nil {
		t.Fatal(err)
	}
	if err := board.ReviewTask("task", "accept", "looks good", worker); err == nil {
		t.Fatal("assignee accepted own review")
	}
	reviewer := m
	reviewer.Actor = TaskActor{Member: "reviewer", Role: "worker"}
	if err := board.ReviewTask("task", "changes", "fix test", reviewer); err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].State != "in_progress" {
		t.Fatal("requested changes did not resume work")
	}
	if err := board.MoveTask("task", "review", worker); err != nil {
		t.Fatal(err)
	}
	if err := board.ReviewTask("task", "accept", "verified", reviewer); err != nil {
		t.Fatal(err)
	}
	board.Tasks[0].Title = "tampered"
	if err := board.Validate(); err == nil {
		t.Fatal("raw edit retained stale accepted review")
	}
}

func TestTaskDependenciesCancellationAndReadOnly(t *testing.T) {
	board, m := workflowFixture(t)
	other := &Task{
		ID: "dependent", Title: "Depends", Paths: Leases{"other/**"}, Dependencies: []string{"task"},
		Criteria: []TaskCriterion{{ID: "check", Text: "Check"}},
	}
	if err := board.CreateTask(other, m); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask("dependent", "ready", m); err == nil {
		t.Fatal("unmet dependency allowed")
	}
	if err := board.MoveTask("task", TaskCancelled, m); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask("dependent", "ready", m); err == nil {
		t.Fatal("canceled dependency allowed")
	}
	m.ReadOnly = true
	if err := board.CommentTask("dependent", "hello", m); err == nil {
		t.Fatal("read-only mutated")
	}
}

func TestTaskCloneAndDurationJSON(t *testing.T) {
	board, m := workflowFixture(t)
	if err := board.CommentTask("task", "note", m); err != nil {
		t.Fatal(err)
	}
	clone := cloneTask(&board.Tasks[0])
	clone.Criteria[0].Text = "changed"
	clone.Comments[0].Text = "changed"
	clone.History[0].Actor = "changed"
	if board.Tasks[0].Criteria[0].Text == "changed" || board.Tasks[0].Comments[0].Text == "changed" ||
		board.Tasks[0].History[0].Actor == "changed" {
		t.Fatal("shallow workflow clone")
	}
	data, err := json.Marshal(board.Tasks[0].Estimate)
	if err != nil || !strings.Contains(string(data), `"duration":"10m0s"`) {
		t.Fatal(string(data), err)
	}
	for _, data := range []string{`{"duration":"10m"}`, `{"duration":600000000000}`} {
		var estimate TaskEstimate
		if err := json.Unmarshal([]byte(data), &estimate); err != nil || estimate.Duration != 10*time.Minute {
			t.Fatal(estimate, err)
		}
	}
}

func TestWorkflowBoardMigrationAndPrivateHistory(t *testing.T) {
	store := BoardStore{Root: t.TempDir(), Team: "workflow"}
	legacy := BoardState{
		Version: 1, Revision: 4, Policy: SchedulingPolicy{MaxWorkers: 2},
		Board: TaskBoard{Version: 1, Tasks: []Task{
			{ID: "pending", Title: "Pending", Paths: Leases{"a/**"}, State: "pending"},
			{ID: "assigned", Title: "Assigned", Paths: Leases{"b/**"}, State: "assigned", Worker: "worker"},
			{
				ID: "done", Title: "Done", Paths: Leases{"c/**"}, State: "done", Worker: "worker",
				Report: &TaskReport{Text: "done"},
			},
		}},
	}
	err := store.Exclusive(t.Context(), func() error { return store.write(t.Context(), "board.json", &legacy) })
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root, store.Team, "board.json")
	before, _ := os.ReadFile(path)
	store.ReadOnly = true
	state, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 2 || state.Board.Tasks[0].State != "ready" || state.Board.Tasks[1].Assignee != "worker" ||
		state.Board.Tasks[2].State != "done" || !state.Board.Tasks[2].LegacyCompletion {
		t.Fatal("unsafe migration", state)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("Load wrote migration")
	}
	store.ReadOnly = false
	if _, err := store.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	migrated, _ := os.ReadFile(path)
	if bytes.Equal(before, migrated) {
		t.Fatal("writable Load did not persist migration")
	}
	if _, err := store.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(migrated, again) {
		t.Fatal("migration repeated")
	}
	if err := store.Save(t.Context(), &state, 4); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(store.Root, store.Team, "history", "00000000000000000004.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("missing private history", err)
	}
	backup, _ := os.ReadFile(filepath.Join(store.Root, store.Team, "history", "00000000000000000004.json"))
	if !bytes.Equal(backup, before) {
		t.Fatal("migration history did not preserve exact legacy bytes")
	}
}

func TestTaskStructuredCompletionMatchesCurrentCriteria(t *testing.T) {
	board, m := workflowFixture(t)
	if err := board.SetAssignee("task", "worker", m); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ready", "in_progress"} {
		if err := board.MoveTask("task", state, m); err != nil {
			t.Fatal(err)
		}
	}
	definition := taskDefinition(&board.Tasks[0])
	report := &DoneReport{
		TaskID: "task",
		Member: "worker", Session: "ses_worker", Summary: "done",
		Checklist: []ReportItem{{ID: "tests", Complete: true}},
		Tests:     []ReportTest{{CriterionID: "tests", Command: "go test", Outcome: "pass"}},
	}
	record := &ReportRecord{
		Source: "structured", Member: "worker", Session: "ses_worker", RecordedAt: m.Now,
		ReportedDone: true, Definition: definition, Reported: report,
	}
	report.Tests[0].Outcome = "not_run"
	if err := board.CompleteTask("task", record, m); err == nil {
		t.Fatal("not-run report completed task")
	}
	report.Tests[0].Outcome = "pass"
	record.Session = "other"
	if err := board.CompleteTask("task", record, m); err == nil {
		t.Fatal("mismatched session completed task")
	}
	record.Session = "ses_worker"
	if err := board.CompleteTask("task", record, m); err != nil {
		t.Fatal(err)
	}
	report.Checklist[0].Complete = false
	if err := board.Validate(); err != nil {
		t.Fatal("report clone aliased input", err)
	}
}
