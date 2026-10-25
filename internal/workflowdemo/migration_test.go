package workflowdemo

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestLegacyMigrationKeepsEvidenceAndDoesNotInventShipment(t *testing.T) {
	board := team.TaskBoard{Version: 1, Tasks: []team.Task{
		{
			ID: "old-done", Title: "Historical work", State: "done", Worker: "Builder", Paths: []string{"history/**"},
			Report: &team.TaskReport{Text: "Legacy claim remains a claim", Tests: "Historical test text"},
		},
		{ID: "pending", Title: "Waiting work", State: "pending", Paths: []string{"pending/**"}},
	}}
	if err := team.MigrateTaskBoard(&board); err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].State != "done" || board.Tasks[0].Report.Text != "Legacy claim remains a claim" {
		t.Fatal("migration lost historical status or reported evidence")
	}
	if board.Tasks[0].ReleaseVersion != "" || !board.Tasks[0].ShippedAt.IsZero() {
		t.Fatal("migration manufactured shipment")
	}
	before, _ := json.Marshal(board)
	if err := team.MigrateTaskBoard(&board); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(board)
	if !bytes.Equal(before, after) {
		t.Fatal("migration is not idempotent")
	}
	store := team.BoardStore{Root: t.TempDir(), Team: "migration-fixture"}
	saved := team.BoardState{Version: 2, Board: board}
	if err := store.Save(t.Context(), &saved, 0); err != nil {
		t.Fatal(err)
	}
	loaded, err := (team.BoardStore{Root: store.Root, Team: store.Team}).Load(t.Context())
	if err != nil || !reflect.DeepEqual(loaded.Board, saved.Board) {
		t.Fatalf("restart changed migrated evidence: %v", err)
	}
}

func TestInvalidOccupiedWorkflowEditIsAtomic(t *testing.T) {
	board := team.TaskBoard{Version: 2}
	mutation := team.TaskMutation{Actor: team.TaskActor{User: true}, Now: time.Now().UTC()}
	if err := board.SetWorkflow(customWorkflow(), mutation); err != nil {
		t.Fatal(err)
	}
	task := team.Task{ID: "occupied", Title: "Keep task placement", Paths: []string{"occupied/**"}}
	if err := board.CreateTask(&task, mutation); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(board)
	invalid := customWorkflow()
	// Preserve all required categories so this exercises the occupied-stage
	// guard, rather than merely rejecting a missing todo category.
	invalid.Stages = append(invalid.Stages, team.WorkflowStage{
		Name: "Other ideas", Color: "#929BAA", Category: "todo",
	})
	invalid.Stages[0].Category = "ready"
	if err := board.SetWorkflow(invalid, mutation); err == nil {
		t.Fatal("invalid occupied category edit accepted")
	}
	after, _ := json.Marshal(board)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid edit changed workflow or tasks")
	}
}
