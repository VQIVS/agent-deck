package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPoolRecoveryDeletesOnlyKnownUnboundOrphan(t *testing.T) {
	store, _ := boardStoreFixture(t)
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]map[string]string{{"id": "ses_orphan", "title": "worker [pool action action]"}})
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/session/ses_orphan" {
			deleted = true
			_, _ = w.Write([]byte("true"))
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL)
	}))
	defer server.Close()
	manager := &Manager{Team: Team{Name: "pool", Server: server.URL}}
	transport := &LifecycleTransport{Store: store, LoadManager: func(context.Context) (*Manager, error) {
		return manager, nil
	}}
	request := &ReconcileRequest{
		Action: ReconcileAction{ID: "action", Kind: "create"}, Member: Member{Name: "worker", Dir: "/repo"},
	}
	if err := store.Exclusive(t.Context(), func() error {
		return store.write(t.Context(), effectName("action"), &lifecycleEffect{State: "started", Request: request})
	}); err != nil {
		t.Fatal(err)
	}
	user := ReconcileActor{User: true}
	recovery, err := transport.Inspect(t.Context(), user, "action")
	if err != nil || recovery.Result.Session != "ses_orphan" {
		t.Fatal("orphan not discoverable", recovery, err)
	}
	if err := transport.Resolve(t.Context(), ReconcileActor{Member: "lead"}, "action", "delete"); err == nil {
		t.Fatal("master forged recovery approval")
	}
	manager.Team.Members = []Member{{Name: "worker", Session: "ses_orphan"}}
	if err := transport.Resolve(t.Context(), user, "action", "delete"); err == nil || deleted {
		t.Fatal("bound session deleted")
	}
	manager.Team.Members = nil
	if err := transport.Resolve(t.Context(), user, "action", "delete"); err != nil || !deleted {
		t.Fatal("unbound orphan not deleted", err)
	}
	if _, done, err := transport.Lookup(t.Context(), "action"); err != nil || !done {
		t.Fatal("resolution not durable", err)
	}
}

func TestFailedDependencyAllowsScaleDown(t *testing.T) {
	_, snapshot, _, _ := reconcileFixture(t)
	snapshot.Input.Policy = SchedulingPolicy{MaxWorkers: 2, Approval: "ask"}
	snapshot.Input.Workers = []SchedulerWorker{
		{Name: "worker", State: "idle", IdleSince: snapshot.Input.Now.Add(-snapshot.Input.Policy.Cooldown)},
	}
	snapshot.Input.Board.Tasks = []Task{
		{
			ID: "failed", Title: "failed", Paths: Leases{"a/**"}, State: "failed", Worker: "worker",
			Report: &TaskReport{Text: "failed"}, Estimate: TaskEstimate{Parallel: true},
		},
		{ID: "blocked", Title: "blocked", Paths: Leases{"b/**"}, State: "pending", Dependencies: []string{"failed"}},
	}
	plan, err := PlanSchedule(&snapshot.Input)
	if err != nil || len(plan.Retire) != 1 {
		t.Fatal("failed dependency prevented scale down", plan, err)
	}
}

func TestPoolCreateSaveFailureRetainsSessionForRecovery(t *testing.T) {
	store, _ := boardStoreFixture(t)
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/session" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		creates++
		_, _ = w.Write([]byte(`{"id":"ses_orphan_save_failure"}`))
	}))
	defer server.Close()
	manager := leasedManager(t, server.URL)
	manager.Team.Capacity = 2
	manager.Store.ReadOnly = true
	transport := &LifecycleTransport{Store: store}
	request := &ReconcileRequest{
		Action: ReconcileAction{ID: "save-failure", Kind: "create"},
		Member: Member{
			Name: "new-worker", Role: "worker", Mode: "shared", Dir: manager.Team.Repo,
			Capability: "coder", Permissions: Preset("coder"), Leases: []string{"new/**"},
		},
	}
	if err := store.Exclusive(t.Context(), func() error {
		result, err := transport.create(t.Context(), manager, request)
		if err == nil || result.Session != "ses_orphan_save_failure" {
			t.Fatal("expected binding save failure after session creation", result, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var effect lifecycleEffect
	if err := store.read(t.Context(), effectName(request.Action.ID), &effect); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || effect.State != "session-created" || effect.Result.Session != "ses_orphan_save_failure" {
		t.Fatal("created session identity was not durable before binding save", effect)
	}
	if _, _, err := transport.Lookup(t.Context(), request.Action.ID); err == nil {
		t.Fatal("uncertain creation allowed automatic retry")
	}
}
