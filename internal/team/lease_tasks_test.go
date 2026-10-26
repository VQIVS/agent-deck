package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func TestTaskLeaseProjectionReleasesNonExecutionStages(t *testing.T) {
	for _, state := range []string{"review", "done", "blocked", TaskCancelled} {
		t.Run(state, func(t *testing.T) {
			task := Task{
				ID: "T29", Title: "discovery", State: state, Assignee: "researcher",
				Reviewer: "qa", Paths: Leases{"docs/research/session.md"},
				Criteria: []TaskCriterion{{ID: "c1", Text: "check"}},
			}
			if state == "done" {
				acceptSchedulerTask(&task)
			}
			board := TaskBoard{Version: 2, Tasks: []Task{task}}
			members := []Member{{
				ID: "r", Name: "researcher", Role: "worker",
				Leases: []string{"docs/research/session.md", "manual/**"},
			}}
			projected, err := projectOriginLeases(members, &board, map[string]taskLeaseOrigin{
				"r": {Manual: []string{"manual/**"}, Derived: task.Paths},
			})
			if err != nil || !slices.Equal(projected[0].Leases, []string{"manual/**"}) {
				t.Fatal(projected, err)
			}
			if !slices.Equal(board.Tasks[0].Paths, task.Paths) {
				t.Fatal("provenance removed")
			}
		})
	}
}

func TestTaskLeaseProjectionPreservesActiveAndReassignment(t *testing.T) {
	board := TaskBoard{Version: 2, Tasks: []Task{{
		ID: "active", Title: "active", State: "in_progress",
		Assignee: "new", Paths: Leases{"src/**"}, Criteria: []TaskCriterion{{ID: "c", Text: "test"}},
		History: []TaskChange{{Actor: "lead", Field: "assignee", From: "old", To: "new"}},
	}}}
	// Validation requires event timestamps; the migration actor represents retained legacy provenance.
	board.Tasks[0].History[0].Actor = "migration"
	members := []Member{
		{ID: "old", Name: "old", Role: "worker", Leases: []string{"src/**"}}, {Name: "new", Role: "worker"},
	}
	projected, err := projectOriginLeases(members, &board,
		map[string]taskLeaseOrigin{"old": {Derived: []string{"src/**"}}})
	if err != nil || len(projected[0].Leases) != 0 || !slices.Equal(projected[1].Leases, []string{"src/**"}) {
		t.Fatal(projected, err)
	}
}

func TestReconcileTaskLeasesRefreshesResetAndRestart(t *testing.T) {
	patches := map[string][]api.Rule{}
	server := httptest.NewServer(appendPolicyFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		var body struct{ Permission []api.Rule }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		patches[r.URL.Path] = body.Permission
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	manager := leasedManager(t, server.URL)
	manager.Team.LeasePool = true
	recordDerivedFixture(t, manager)
	worker := manager.Team.Members[1].Name
	board := TaskBoard{Version: 2, Tasks: []Task{{
		ID: "T29", Title: "old discovery", State: "blocked",
		Assignee: worker, Paths: Leases{"internal/tests/**"},
		Criteria: []TaskCriterion{{ID: "c", Text: "test"}},
	}}}
	if err := ReconcileTaskLeases(t.Context(), manager, &board); err != nil {
		t.Fatal(err)
	}
	if len(manager.Team.Members[1].Leases) != 0 {
		t.Fatal("stale task lease retained")
	}
	assertLeaseRule(t, patches["/session/worker"], "edit", "*", "deny")
	assertLeaseRule(t, patches["/session/master"], "edit", "*", "allow")
	for _, rule := range patches["/session/worker"] {
		if rule.Permission == "edit" && rule.Pattern == "internal/tests/**" && rule.Action == "allow" {
			t.Fatal("revoked allow in reset")
		}
	}
	configured, version, err := manager.Store.Load(t.Context(), manager.Team.Name)
	if err != nil {
		t.Fatal(err)
	}
	restored := &Manager{Store: manager.Store, Team: configured, Version: version}
	clear(patches)
	if err := ReconcileTaskLeases(t.Context(), restored, &board); err != nil || len(patches) != 0 {
		t.Fatal("restart repeated applied policy", patches, err)
	}
}

func TestTaskLeaseProjectionPreservesOtherActiveTask(t *testing.T) {
	board := TaskBoard{Version: 2, Tasks: []Task{
		{
			ID: "old", Title: "old", State: "blocked", Assignee: "worker",
			Paths: Leases{"old/**"}, Criteria: []TaskCriterion{{ID: "c", Text: "check"}},
		},
		{
			ID: "new", Title: "new", State: "in_progress", Assignee: "worker",
			Paths: Leases{"new/**"}, Criteria: []TaskCriterion{{ID: "c", Text: "check"}},
		},
	}}
	members := []Member{{ID: "w", Name: "worker", Role: "worker", Leases: []string{"old/**", "new/**"}}}
	projected, err := projectOriginLeases(members, &board,
		map[string]taskLeaseOrigin{"w": {Derived: []string{"old/**", "new/**"}}})
	if err != nil || !slices.Equal(projected[0].Leases, []string{"new/**"}) {
		t.Fatal(projected, err)
	}
}

func TestReconcileTaskLeasesRetryFailedNativeReset(t *testing.T) {
	fail := true
	patches := 0
	server := httptest.NewServer(appendPolicyFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected %s", r.Method)
		}
		patches++
		if fail {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	manager := leasedManager(t, server.URL)
	manager.Team.LeasePool = true
	recordDerivedFixture(t, manager)
	worker := manager.Team.Members[1].Name
	board := TaskBoard{Version: 2, Tasks: []Task{{
		ID: "old", Title: "old", State: "blocked",
		Assignee: worker, Paths: Leases{"internal/tests/**"},
		Criteria: []TaskCriterion{{ID: "c", Text: "check"}},
	}}}
	if err := ReconcileTaskLeases(t.Context(), manager, &board); err == nil {
		t.Fatal("failed PATCH ignored")
	}
	configured, version, err := manager.Store.Load(t.Context(), manager.Team.Name)
	if err != nil || len(configured.Members[1].Leases) != 0 {
		t.Fatal(configured, err)
	}
	fail = false
	restored := &Manager{Store: manager.Store, Team: configured, Version: version}
	if err := ReconcileTaskLeases(t.Context(), restored, &board); err != nil || patches < 3 {
		t.Fatal(patches, err)
	}
	readOnly := *restored
	readOnly.ReadOnly = true
	before := patches
	if err := ReconcileTaskLeases(t.Context(), &readOnly, &board); err == nil || patches != before {
		t.Fatal("read-only reset caused effects")
	}
}

func recordDerivedFixture(t *testing.T, manager *Manager) {
	t.Helper()
	paths := slices.Clone(manager.Team.Members[1].Leases)
	manager.Team.Members[1].Leases = nil
	if err := RecordTaskLeaseGrant(t.Context(), manager, 1, paths); err != nil {
		t.Fatal(err)
	}
	manager.Team.Members[1].Leases = paths
}

func TestTaskLeaseOriginsPreserveIdenticalManualAndLegacyPaths(t *testing.T) {
	board := TaskBoard{Version: 2, Tasks: []Task{{
		ID: "old", Title: "old", State: "blocked",
		Assignee: "worker", Paths: Leases{"same/**"},
		Criteria: []TaskCriterion{{ID: "c", Text: "check"}},
	}}}
	members := []Member{{ID: "w", Name: "worker", Role: "worker", Leases: []string{"same/**"}}}
	cases := []map[string]taskLeaseOrigin{nil, {
		"w": {Manual: []string{"same/**"}, Derived: []string{"same/**"}},
	}}
	for _, origins := range cases {
		projected, err := projectOriginLeases(members, &board, origins)
		if err != nil || !slices.Equal(projected[0].Leases, []string{"same/**"}) {
			t.Fatal(projected, err)
		}
	}
	if err := legacyLeaseDiagnostic(members, &board, nil); err == nil {
		t.Fatal("legacy ambiguity hidden")
	}
}

func TestLeaseOriginPersistsManualOverlapAcrossRestart(t *testing.T) {
	manager := leasedManager(t, "http://127.0.0.1:1")
	paths := slices.Clone(manager.Team.Members[1].Leases)
	if err := RecordTaskLeaseGrant(t.Context(), manager, 1, paths); err != nil {
		t.Fatal(err)
	}
	configured, version, err := manager.Store.Load(t.Context(), manager.Team.Name)
	if err != nil {
		t.Fatal(err)
	}
	restored := &Manager{Store: manager.Store, Team: configured, Version: version}
	origins, err := loadLeaseOrigins(t.Context(), restored)
	if err != nil {
		t.Fatal(err)
	}
	board := TaskBoard{Version: 2, Tasks: []Task{{
		ID: "old", Title: "old", State: "blocked",
		Assignee: configured.Members[1].Name, Paths: paths,
		Criteria: []TaskCriterion{{ID: "c", Text: "check"}},
	}}}
	projected, err := projectOriginLeases(configured.Members, &board, origins)
	if err != nil || !slices.Equal(projected[1].Leases, paths) {
		t.Fatal(projected, err)
	}
}

func TestCustomActiveAndShippedLeaseCategories(t *testing.T) {
	w := DefaultWorkflow()
	w.Stages[2].Name, w.Stages[5].Name = "Building", "Delivered"
	w.Transitions = nil
	board := TaskBoard{Version: 2, Workflow: w, Tasks: []Task{{
		ID: "work", Title: "work", State: "Building",
		Assignee: "worker", Paths: Leases{"src/**"}, Criteria: []TaskCriterion{{ID: "c", Text: "test"}},
	}}}
	members := []Member{{ID: "w", Name: "worker", Role: "worker", Leases: []string{"src/**"}}}
	origins := map[string]taskLeaseOrigin{"w": {Derived: []string{"src/**"}}}
	projected, err := projectOriginLeases(members, &board, origins)
	if err != nil || len(projected[0].Leases) != 1 {
		t.Fatal(projected, err)
	}
	acceptSchedulerTask(&board.Tasks[0])
	board.Tasks[0].State = "Delivered"
	board.Tasks[0].ReleaseVersion = "v1"
	projected, err = projectOriginLeases(members, &board, origins)
	if err != nil || len(projected[0].Leases) != 0 {
		t.Fatal(projected, err)
	}
}
