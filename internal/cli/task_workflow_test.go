package cli

import (
	"io"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestConfiguredWorkflowFlags(t *testing.T) {
	flags, err := parseTaskFlags("move", []string{
		"--team", "example", "--id", "T-1", "--state", "Published",
		"--version", "v1.2.3", "--release-url", "https://example.test/releases/v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if flags.Call.State != "Published" || flags.Version != "v1.2.3" ||
		flags.ReleaseURL != "https://example.test/releases/v1.2.3" {
		t.Fatalf("configured stage/version lost: %+v", flags)
	}
	for _, command := range []string{"workflow", "pending-release"} {
		if _, err := parseTaskFlags(command, []string{"--team", "example", "--json", "--read-only"}); err != nil {
			t.Fatalf("readonly discovery %s: %v", command, err)
		}
	}
}

func TestCLIReleaseMetadataUsesEngineGuards(t *testing.T) {
	board := team.TaskBoard{Version: 2, Workflow: team.DefaultWorkflow(), Tasks: []team.Task{{
		ID: "T-1", Title: "accepted", State: "review", Reviewer: "qa", Assignee: "worker",
		CompletedAt: time.Now().Add(-time.Hour),
		Criteria:    []team.TaskCriterion{{ID: "c1", Text: "works"}}, Paths: team.Leases{"own/**"},
	}}}
	mutation := team.TaskMutation{Actor: team.TaskActor{User: true}, Now: time.Now().Add(-time.Hour)}
	if err := board.ReviewTask("T-1", "accept", "verified", mutation); err != nil {
		t.Fatal(err)
	}
	store := team.BoardStore{Root: t.TempDir(), Team: "release"}
	state := team.BoardState{Version: 2, Board: board, Policy: team.SchedulingPolicy{Approval: "off"}}
	if err := store.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	manager := &team.Manager{Team: team.Team{Name: "release"}}
	flags := taskFlags{JSON: true, Call: team.WorkflowCall{TaskSpec: team.TaskSpec{ID: "T-1"}, State: "released"}}
	if err := applyTaskCommand(t.Context(), manager, store, &flags, "move", io.Discard); err == nil {
		t.Fatal("CLI shipment without version accepted")
	}
	flags.Version = "v2.0"
	if err := applyTaskCommand(t.Context(), manager, store, &flags, "move", io.Discard); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Board.Tasks[0].ReleaseVersion != "v2.0" {
		t.Fatal("CLI version lost")
	}
}
