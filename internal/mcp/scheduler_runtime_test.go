package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestPoolSchedulerAskAndRevisionGate(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer server.Close()
	store := team.Store{Root: t.TempDir()}
	tm := team.Team{
		Version: 1, Name: "pool", Server: server.URL, Repo: t.TempDir(), Base: "main", Capacity: 3,
		Members: []team.Member{{
			Name: "master", Role: "master", Session: "session-master",
			Mode: "shared", Dir: t.TempDir(), Capability: "inherited",
		}},
	}
	if _, err := store.Save(t.Context(), &tm, nil); err != nil {
		t.Fatal(err)
	}
	board := team.BoardStore{Root: filepath.Join(t.TempDir(), "boards"), Team: tm.Name}
	pool, err := team.OpenPool(t.Context(), store, tm.Name, board)
	if err != nil {
		t.Fatal(err)
	}
	state := team.BoardState{
		Version: 2, Board: team.TaskBoard{Version: 2},
		Policy: team.SchedulingPolicy{MinWorkers: 1, MaxWorkers: 2, Approval: "ask"},
	}
	if err := pool.Configure(t.Context(), team.ReconcileActor{User: true}, &state, 0); err != nil {
		t.Fatal(err)
	}
	readOnly := false
	provider := func(ctx context.Context) (*team.Manager, team.BoardStore, error) {
		loaded, version, err := store.Load(ctx, tm.Name)
		return &team.Manager{Store: store, Team: loaded, Version: version, ReadOnly: readOnly}, board, err
	}
	tools, err := NewPoolScheduler(provider)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tools.Call(t.Context(), Identity{Team: tm.Name, Member: "master"}, "propose_workers", nil)
	if err != nil {
		t.Fatal(err)
	}
	proposal := result.(PoolProposalResult)
	if len(proposal.Pending) != 1 || len(proposal.Applied) != 0 || calls.Load() != 0 {
		t.Fatal("ask proposal spawned session", proposal, calls.Load())
	}
	snapshot, err := tools.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	action := &SchedulerAction{Team: tm.Name, Master: "master", Revision: snapshot.Revision, Kind: "propose_workers"}
	current, err := pool.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current.Policy.MaxWorkers = 3
	if err := pool.Configure(t.Context(), team.ReconcileActor{User: true}, &current, current.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.Actions.ApplySchedulerAction(t.Context(), action); err == nil {
		t.Fatal("stale revision executed")
	}
	readOnly = true
	if _, err := tools.Call(t.Context(), Identity{Team: tm.Name, Member: "master"}, "propose_workers", nil); err == nil {
		t.Fatal("read-only provider bypassed")
	}
	if _, err := NewPoolScheduler(nil); err == nil {
		t.Fatal("nil provider accepted")
	}
}
