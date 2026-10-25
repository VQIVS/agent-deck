package workflowdemo

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

// TestAcceptedTaskSurvivesRestartAndStaleWrite is a cross-package compatibility
// baseline: every state below is reached through production mutations.
func TestAcceptedTaskSurvivesRestartAndStaleWrite(t *testing.T) {
	board := team.TaskBoard{Version: 2}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	user := team.TaskMutation{Actor: team.TaskActor{User: true}, Now: now}
	worker := team.TaskMutation{Actor: team.TaskActor{Member: "Builder", Role: "worker"}, Now: now.Add(time.Minute)}
	reviewer := team.TaskMutation{Actor: team.TaskActor{Member: "Reviewer", Role: "worker"}, Now: now.Add(2 * time.Minute)}
	task := team.Task{
		ID: "compatibility", Title: "Preserve accepted evidence", Assignee: "Builder", Reviewer: "Reviewer",
		Paths: []string{"fixture/**"}, Criteria: []team.TaskCriterion{{ID: "persist", Text: "Evidence survives restart"}},
	}
	if err := board.CreateTask(&task, user); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ready", "in_progress"} {
		if err := board.MoveTask(task.ID, state, user); err != nil {
			t.Fatal(err)
		}
	}
	if err := board.CommentTask(task.ID, "Observed isolated fixture; no implementation claim", worker); err != nil {
		t.Fatal(err)
	}
	if err := board.UpdateTaskProgress(task.ID, map[string]bool{"persist": true}, worker); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask(task.ID, "review", worker); err != nil {
		t.Fatal(err)
	}
	if err := board.ReviewTask(task.ID, "accept", "Independent fixture review", reviewer); err != nil {
		t.Fatal(err)
	}
	store := team.BoardStore{Root: t.TempDir(), Team: "workflow-verification"}
	saved := team.BoardState{Version: 2, Board: board}
	if err := store.Save(t.Context(), &saved, 0); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the store to avoid relying on process-local state.
	restarted := team.BoardStore{Root: store.Root, Team: store.Team}
	loaded, err := restarted.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Board, saved.Board) {
		t.Fatal("restart changed task evidence, history or acceptance")
	}
	if loaded.Board.Tasks[0].Review == nil || loaded.Board.Tasks[0].CompletedAt.IsZero() {
		t.Fatal("fixture did not persist accepted completion")
	}
	stale := loaded
	if err := restarted.Save(t.Context(), &loaded, loaded.Revision); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Save(t.Context(), &stale, stale.Revision); !errors.Is(err, team.ErrBoardChanged) {
		t.Fatalf("stale save must explain reload requirement: %v", err)
	}
	after, err := restarted.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Board, saved.Board) {
		t.Fatal("stale rejection changed persisted task evidence")
	}
}
