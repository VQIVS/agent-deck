package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestRuntimeRepairsRenamedTaskReferencesBeforeCredentialRevoke(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fixture := newRuntimeFixture(t)
	manager, err := fixture.provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	manager.Store = team.Store{Root: t.TempDir()}
	manager.Team.Version = 1
	manager.Team.Capacity = 2
	for i := range manager.Team.Members {
		manager.Team.Members[i].Capability = "coder"
		manager.Team.Members[i].Permissions = team.Preset("coder")
	}
	manager.Team.Members[0].ID = ""
	manager.Team.Members[0].Name = "old"
	manager.Team.Members[0].Role = "master"
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := &Runtime{manager: manager, Tokens: NewTokens(), credentials: map[string]runtimeCredential{}}
	r.options.Provider = func(ctx context.Context) (*team.Manager, error) {
		current, version, err := manager.Store.Load(ctx, manager.Team.Name)
		return &team.Manager{Store: manager.Store, Team: current, Version: version}, err
	}
	_, board, err := RuntimePool(r.options.Provider)(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state := team.BoardState{Version: 2, Board: team.TaskBoard{Version: 2, Tasks: []team.Task{{
		ID: "T-1", Title: "work", State: "ready", Assignee: "old", Worker: "old", Reviewer: "old",
		Paths: team.Leases{"own/**"}, Criteria: []team.TaskCriterion{{ID: "c1", Text: "works"}},
	}}}, Policy: team.SchedulingPolicy{Approval: "off"}}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	journal := map[string]any{"pending": []map[string]string{{
		"id": manager.Team.Members[0].ID, "from": "old", "to": "new",
	}}}
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(board.Root, board.Team, "member-renames.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	manager.Team.Members[0].Name = "new"
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Holding the credential lock proves repair never acquires it; runtime invokes
	// this helper before the normal credential revocation/installation phase.
	r.credentialMu.Lock()
	err = r.repairMemberTaskNames(t.Context(), manager)
	r.credentialMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := board.Load(t.Context())
	if err != nil || loaded.Board.Tasks[0].Assignee != "new" {
		t.Fatal("owner repair missing", err)
	}
}
