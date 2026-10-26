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

func TestWorkflowBoardRejectsV1WithoutWriting(t *testing.T) {
	for _, versions := range [][2]int{{1, 1}, {1, 2}, {2, 1}} {
		for _, readOnly := range []bool{false, true} {
			store := BoardStore{Root: t.TempDir(), Team: "legacy"}
			legacy := BoardState{Version: versions[0], Board: TaskBoard{Version: versions[1]}}
			if err := store.Exclusive(t.Context(), func() error {
				return store.write(t.Context(), "board.json", &legacy)
			}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.Root, store.Team, "board.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store.ReadOnly = readOnly
			_, err = store.Load(t.Context())
			if err == nil || err.Error() != "board v1 unsupported; open it once with agent-deck v0.3.x" {
				t.Fatalf("versions %v read-only %v: %v", versions, readOnly, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejected board was modified", err)
			}
		}
	}
}

func TestWorkflowV2IgnoresRemovedFieldsWithoutWriting(t *testing.T) {
	store, _ := boardStoreFixture(t)
	path := filepath.Join(store.Root, store.Team, "board.json")
	data := []byte(`{"version":2,"revision":4,"board":{"version":2,"tasks":[{
		"id":"task","title":"planned","state":"ready","assignee":"worker","worker":"obsolete",
		"paths":["src/**"],"criteria":[{"id":"check","text":"Check"}],
		"report":{"text":"old"},"measured":{"tokens":123},"legacy_completion":true
	}]},"policy":{"max_workers":2,"token_budget":1,"cost_budget":1}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := store.Load(t.Context())
	if err != nil || state.Revision != 4 || state.Board.Tasks[0].Assignee != "worker" {
		t.Fatal("v2 compatibility lost", state, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("v2 load rewrote board", err)
	}
	state.Board.Tasks[0].State = "done"
	if err := state.Validate(); err == nil {
		t.Fatal("obsolete completion flag bypassed review")
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
