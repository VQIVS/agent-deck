package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func TestReconciledGrantRecordsOriginBeforeRestartAndRelease(t *testing.T) {
	server := httptest.NewServer(appendPolicyFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected effect %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m := leasedManager(t, server.URL)
	m.Team.LeasePool = true
	m.Team.Members[1].Leases = nil
	worker := m.Team.Members[1].Name
	board := TaskBoard{Version: 2, Tasks: []Task{{
		ID: "grant", Title: "restored active task", State: "in_progress", Assignee: worker, Worker: worker,
		Paths: Leases{"src/**"}, Criteria: []TaskCriterion{{ID: "c", Text: "check"}},
	}}}
	if err := ReconcileTaskLeases(t.Context(), m, &board); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Team.Members[1].Leases, []string{"src/**"}) {
		t.Fatal("active grant missing")
	}
	configured, version, err := m.Store.Load(t.Context(), m.Team.Name)
	if err != nil {
		t.Fatal(err)
	}
	restored := &Manager{Store: m.Store, Team: configured, Version: version}
	board.Tasks[0].State = "blocked"
	if err := ReconcileTaskLeases(t.Context(), restored, &board); err != nil {
		t.Fatal(err)
	}
	if len(restored.Team.Members[1].Leases) != 0 {
		t.Fatal("reconciler-created grant became ambiguous after restart")
	}
}

func TestLifecycleAssignmentImmediatelyPreservesManualPolicy(t *testing.T) {
	var rules []api.Rule
	server := httptest.NewServer(appendPolicyFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			var body struct{ Permission []api.Rule }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			rules = body.Permission
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch r.URL.Path {
		case "/session":
			fmt.Fprint(w, `[{"id":"worker"},{"id":"master"}]`)
		case "/session/status":
			fmt.Fprint(w, `{}`)
		default:
			if r.Method != http.MethodGet {
				t.Errorf("unexpected effect %s %s", r.Method, r.URL.Path)
			}
			fmt.Fprint(w, `[]`)
		}
	}))
	defer server.Close()
	m := leasedManager(t, server.URL)
	manual := slices.Clone(m.Team.Members[1].Leases)
	requested := m.Team.Members[1]
	requested.Leases = []string{"src/**"}
	transport := &LifecycleTransport{}
	_, err := transport.execute(t.Context(), m, &ReconcileRequest{
		Action: ReconcileAction{Kind: "assign", Worker: requested.Name}, Member: requested,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertLeaseRule(t, rules, "edit", manual[0], "allow")
	assertLeaseRule(t, rules, "edit", "src/**", "allow")
	origins, err := loadLeaseOrigins(t.Context(), m)
	if err != nil || !slices.Equal(origins[requested.ID].Manual, manual) {
		t.Fatal(origins, err)
	}
}
