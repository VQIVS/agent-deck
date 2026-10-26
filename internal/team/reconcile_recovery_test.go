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
			ID: "failed", Title: "failed", Paths: Leases{"a/**"}, State: "blocked", Assignee: "worker",
			Criteria: []TaskCriterion{{ID: "check", Text: "Check"}},
			Estimate: TaskEstimate{Parallel: true},
		},
		{ID: "blocked", Title: "blocked", Paths: Leases{"b/**"}, State: "backlog", Dependencies: []string{"failed"}},
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

func TestAdoptionRejectsHostileStaleEffectBeforeMutation(t *testing.T) {
	store, state := boardStoreFixture(t)
	tm := example()
	tm.Name = "pool"
	tm.Members = tm.Members[:1]
	teamStore := Store{Root: t.TempDir()}
	version, err := teamStore.Save(t.Context(), &tm, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Store: teamStore, Team: tm, Version: version}
	transport := &LifecycleTransport{Store: store}
	recovery := &PoolRecovery{
		Result: ReconcileResult{Worker: "forged", Session: "ses_orphan"},
		Request: &ReconcileRequest{
			Action: ReconcileAction{ID: "stale-action", Kind: "create"},
			Member: Member{Name: "forged", Capability: "full", Permissions: Preset("full"), Leases: []string{"*"}},
		},
	}
	if err := transport.prepareAdoption(t.Context(), manager, recovery); err == nil {
		t.Fatal("stale hostile recovery policy accepted")
	}
	loaded, err := store.Load(t.Context())
	if err != nil || loaded.Revision != state.Revision {
		t.Fatal("rejected adoption mutated board", err)
	}
}

func TestAdoptionRebuildsHostileMemberFromCurrentPolicy(t *testing.T) {
	store, _ := boardStoreFixture(t)
	tm := example()
	tm.Name = "pool"
	tm.Members = tm.Members[:1]
	teamStore := Store{Root: t.TempDir()}
	version, err := teamStore.Save(t.Context(), &tm, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Store: teamStore, Team: tm, Version: version}
	pool, err := OpenPool(t.Context(), teamStore, "pool", store)
	if err != nil {
		t.Fatal(err)
	}
	actions, err := pool.Pending(t.Context(), ReconcileActor{User: true})
	if err != nil || len(actions) != 1 {
		t.Fatal(actions, err)
	}
	recovery := &PoolRecovery{
		Result: ReconcileResult{Worker: "forged", Session: "ses_orphan"},
		Request: &ReconcileRequest{Action: actions[0], Member: Member{
			Name: "forged", Role: "master", Capability: "full", Permissions: Preset("full"), Leases: []string{"*"},
		}},
	}
	transport := &LifecycleTransport{Store: store}
	if err := transport.prepareAdoption(t.Context(), manager, recovery); err == nil {
		t.Fatal("adoption bypassed current ask approval")
	}
	if err := pool.Approve(t.Context(), ReconcileActor{User: true}, actions[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := transport.prepareAdoption(t.Context(), manager, recovery); err != nil {
		t.Fatal(err)
	}
	member := &recovery.Request.Member
	if member.Name == "forged" || member.Role != "worker" || member.Capability != "coder" ||
		len(member.Leases) != 0 || member.Permissions["bash"] != "ask" {
		t.Fatal("adoption trusted hostile effect member", member)
	}
}
