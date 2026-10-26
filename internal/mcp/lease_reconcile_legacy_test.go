package mcp

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestLegacyManualLeasesDoNotBlockRuntimeRegistration(t *testing.T) {
	_, manager := poolIdentityFixture(t)
	var registrations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/mcp":
			registrations.Add(1)
			fmt.Fprint(w, `{}`)
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/disconnect"):
			fmt.Fprint(w, `{}`)
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/prompt_async"):
			w.WriteHeader(http.StatusNoContent)
		case req.URL.Path == "/session":
			fmt.Fprint(w, `[{"id":"legacy-worker"}]`)
		case req.URL.Path == "/session/status":
			fmt.Fprint(w, `{}`)
		case req.URL.Path == "/permission" || req.URL.Path == "/question" || strings.HasSuffix(req.URL.Path, "/message"):
			fmt.Fprint(w, `[]`)
		default:
			t.Errorf("unexpected native effect %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	stateDir := t.TempDir()
	owner := &Runtime{options: RuntimeOptions{StateDir: stateDir}}
	identity, err := PrepareWorkflowIdentity(t.Context(), owner.workflowRoot(manager.Team.Name),
		manager.Team.Members[0].ID, manager.Team.Repo)
	if err != nil {
		t.Fatal(err)
	}
	worker := &manager.Team.Members[0]
	worker.ControlDir, worker.Session, worker.Leases = identity.ControlDir, "legacy-worker", []string{"legacy/**"}
	manager.Team.Server, manager.Team.LeasePool = server.URL, true
	for i := range manager.Team.Members {
		manager.Team.Members[i].LeasePolicy = manager.Team.LeaseFingerprint(&manager.Team.Members[i])
	}
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	board := team.BoardStore{Root: t.TempDir(), Team: manager.Team.Name}
	state := team.BoardState{Version: 2, Board: team.TaskBoard{Version: 2, Tasks: []team.Task{{
		ID: "old", Title: "old", State: "blocked", Assignee: worker.Name, Worker: worker.Name,
		Paths: team.Leases{"legacy/**"}, Criteria: []team.TaskCriterion{{ID: "c", Text: "check"}},
	}}}, Policy: team.SchedulingPolicy{Approval: "off"}}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	provider := func(ctx context.Context) (*team.Manager, error) {
		configured, version, err := manager.Store.Load(ctx, manager.Team.Name)
		return &team.Manager{Store: manager.Store, Team: configured, Version: version}, err
	}
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	options := RuntimeOptions{
		Provider: provider, StateDir: stateDir,
		JournalPath: filepath.Join(t.TempDir(), "channel"), Interval: time.Hour,
		Tasks: &TaskTools{Provider: func(ctx context.Context) (*team.Manager, team.BoardStore, error) {
			current, err := provider(ctx)
			return current, board, err
		}},
	}
	runtime, err := StartRuntime(t.Context(), &options)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := runtime.Step(t.Context()); err != nil {
			runtime.Close()
			t.Fatal(err)
		}
	}
	runtime.Close()
	if registrations.Load() != 1 {
		t.Fatal("runtime did not register worker tools", registrations.Load())
	}
	restarted, err := StartRuntime(t.Context(), &options)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Step(t.Context()); err != nil {
		restarted.Close()
		t.Fatal(err)
	}
	restarted.Close()
	if registrations.Load() != 2 {
		t.Fatal("runtime did not restore tools on restart", registrations.Load())
	}
	if got := strings.Count(logs.String(), "unknown-origin leases retained as manual"); got != 1 {
		t.Fatal("legacy warning not logged exactly once", got, logs.String())
	}
	reopened, err := provider(t.Context())
	if err != nil || len(reopened.Team.Members[0].Leases) != 1 {
		t.Fatal("manual lease was revoked", err)
	}
}
