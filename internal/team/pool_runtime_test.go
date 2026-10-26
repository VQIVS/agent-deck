package team

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func poolRuntimeFixture(t *testing.T, server string) (Store, BoardStore) {
	t.Helper()
	tm := example()
	tm.Name, tm.Server = "pool", server
	tm.Members[0].Session = "ses_lead"
	tm.Members[1].Session = "ses_worker"
	store := Store{Root: t.TempDir()}
	if _, err := store.Save(t.Context(), &tm, nil); err != nil {
		t.Fatal(err)
	}
	return store, BoardStore{Root: filepath.Join(t.TempDir(), "boards"), Team: "pool"}
}

func TestOpenPoolUnconfiguredDoesNotPollOrWrite(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	store, board := poolRuntimeFixture(t, server.URL)
	pool, err := OpenPool(t.Context(), store, "pool", board)
	if err != nil {
		t.Fatal(err)
	}
	state, err := pool.Status(t.Context())
	if err != nil || state.Policy.Approval != "off" || state.Revision != 0 {
		t.Fatal("unsafe missing-board default", state, err)
	}
	actions, err := pool.Actions(t.Context(), ReconcileActor{User: true})
	if err != nil || len(actions) != 0 || calls.Load() != 0 {
		t.Fatal("unconfigured pool polled server or proposed work", actions, err)
	}
	if _, err := os.Stat(board.Root); !os.IsNotExist(err) {
		t.Fatal("factory created board state", err)
	}
}

func TestPoolRuntimeRejectPersistsAndBlocksApproval(t *testing.T) {
	store, board := poolRuntimeFixture(t, "http://127.0.0.1:1")
	tm, version, err := store.Load(t.Context(), "pool")
	if err != nil {
		t.Fatal(err)
	}
	tm.Members[1].Session = ""
	if _, err := store.Save(t.Context(), &tm, version); err != nil {
		t.Fatal(err)
	}
	pool, err := OpenPool(t.Context(), store, "pool", board)
	if err != nil {
		t.Fatal(err)
	}
	state := BoardState{
		Version: 2, Board: TaskBoard{Version: 2},
		Policy: SchedulingPolicy{MinWorkers: 1, MaxWorkers: 2, Approval: "ask"},
	}
	actor := ReconcileActor{User: true}
	if err := pool.Configure(t.Context(), actor, &state, 0); err != nil {
		t.Fatal(err)
	}
	actions, err := pool.Actions(t.Context(), actor)
	if err != nil || len(actions) != 1 {
		t.Fatal(actions, err)
	}
	if err := pool.Reject(t.Context(), ReconcileActor{Member: tm.Members[0].Name}, actions[0].ID); err == nil {
		t.Fatal("master forged privileged rejection")
	}
	if err := pool.Reject(t.Context(), actor, actions[0].ID); err != nil {
		t.Fatal(err)
	}
	pool, err = OpenPool(t.Context(), store, "pool", board)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Approve(t.Context(), actor, actions[0].ID); err == nil {
		t.Fatal("rejected action approved")
	}
	remaining, err := pool.Pending(t.Context(), actor)
	if err != nil || len(remaining) != 0 {
		t.Fatal("rejected action resurfaced", remaining, err)
	}
	if _, err := pool.Controller(actor).Reconciler.Apply(t.Context(), actor, &actions[0]); err == nil {
		t.Fatal("rejected action bypassed transport gate")
	}
}

func TestAutoReconcileAskModeNeverCreatesOrApproves(t *testing.T) {
	store, board := poolRuntimeFixture(t, "http://127.0.0.1:1")
	tm, version, err := store.Load(t.Context(), "pool")
	if err != nil {
		t.Fatal(err)
	}
	tm.Members[1].Session = ""
	if _, err := store.Save(t.Context(), &tm, version); err != nil {
		t.Fatal(err)
	}
	pool, err := OpenPool(t.Context(), store, "pool", board)
	if err != nil {
		t.Fatal(err)
	}
	if notices, err := pool.AutoReconcile(t.Context()); err != nil || notices != nil {
		t.Fatal("unconfigured board reconciled", notices, err)
	}
	state := BoardState{
		Version: 2, Board: TaskBoard{Version: 2},
		Policy: SchedulingPolicy{MinWorkers: 1, MaxWorkers: 2, Approval: "ask"},
	}
	if err := pool.Configure(t.Context(), ReconcileActor{User: true}, &state, 0); err != nil {
		t.Fatal(err)
	}
	actions, err := pool.Actions(t.Context(), ReconcileActor{User: true})
	if err != nil || len(actions) != 1 || actions[0].Kind != "create" {
		t.Fatal(actions, err)
	}
	if _, err := pool.AutoReconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _, err := store.Load(t.Context(), "pool")
	if err != nil || len(after.Members) != len(tm.Members) || after.Members[1].Session != "" {
		t.Fatal("ask mode created a worker without approval", err)
	}
	if err := pool.Approve(t.Context(), ReconcileActor{Automation: true}, actions[0].ID); err == nil {
		t.Fatal("automation approved a creation")
	}
}
