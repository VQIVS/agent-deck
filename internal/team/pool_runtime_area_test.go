package team

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGeneratedWorkerRetainsRequirementsAcrossCreateReloadAndSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			fmt.Fprint(w, `{"id":"generated"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/prompt_async"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/session":
			fmt.Fprint(w, `[{"id":"generated"},{"id":"master"}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/session/status":
			fmt.Fprint(w, `{}`)
		case r.Method == http.MethodGet:
			fmt.Fprint(w, `[]`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	manager := leasedManager(t, server.URL)
	manager.Team.Name = "pool"
	manager.Version = nil
	manager.Team.Members = manager.Team.Members[:1]
	manager.Team.Members[0].ID = ""
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	boardStore := BoardStore{Root: filepath.Join(t.TempDir(), "boards"), Team: "pool"}
	pool, err := OpenPool(t.Context(), manager.Store, "pool", boardStore)
	if err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID: "specialized", Title: "specialized", State: "ready", Role: "core", Skills: []string{"go"},
		Paths: Leases{"internal/team/**"}, Criteria: []TaskCriterion{{ID: "c", Text: "check"}},
	}
	state := BoardState{
		Version: 2, Board: TaskBoard{Version: 2, Tasks: []Task{task}},
		Policy: SchedulingPolicy{MaxWorkers: 1, Approval: "auto"},
	}
	if err := pool.Configure(t.Context(), ReconcileActor{User: true}, &state, 0); err != nil {
		t.Fatal(err)
	}
	snapshot, err := pool.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSchedule(&snapshot.Input)
	if err != nil || len(plan.Proposals) != 1 {
		t.Fatal(plan, err)
	}
	action := ReconcileAction{ID: "0123456789abcdef0123456789abcdef", Kind: "create", Proposal: &plan.Proposals[0]}
	member, err := reconcileMember(&snapshot, &action)
	if err != nil {
		t.Fatal(err)
	}
	transport := &LifecycleTransport{Store: boardStore}
	if err := boardStore.Exclusive(t.Context(), func() error {
		_, createErr := transport.create(t.Context(), manager, &ReconcileRequest{Action: action, Member: member})
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	// Reopen the runtime to force persisted team/board reload and fresh observation.
	restored, err := OpenPool(t.Context(), manager.Store, "pool", boardStore)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.Snapshot(t.Context())
	if err != nil || len(after.Input.Workers) != 1 {
		t.Fatal(after, err)
	}
	worker := after.Input.Workers[0]
	if worker.Role != task.Role || !slices.Equal(worker.Skills, task.Skills) {
		t.Fatal("generated scheduling metadata lost", worker)
	}
	matched, err := PlanSchedule(&after.Input)
	if err != nil || len(matched.Assignments) != 1 || matched.Assignments[0].Worker != member.Name {
		t.Fatal("generated capacity cannot satisfy its originating task", matched, err)
	}
}

func TestPoolSnapshotSchedulingMetadataMatchesRequirements(t *testing.T) {
	scenarios := []string{
		"match", "role", "skill", "area", "explicit-match", "explicit-role", "explicit-skill", "explicit-area",
		"legacy", "epic",
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("snapshot mutated session: %s %s", r.Method, r.URL.Path)
				}
				switch r.URL.Path {
				case "/session":
					fmt.Fprint(w, `[{"id":"ses_worker"},{"id":"ses_lead"}]`)
				case "/session/status":
					fmt.Fprint(w, `{}`)
				default:
					fmt.Fprint(w, `[]`)
				}
			}))
			defer server.Close()
			store, boardStore := poolRuntimeFixture(t, server.URL)
			tm, version, err := store.Load(t.Context(), "pool")
			if err != nil {
				t.Fatal(err)
			}
			member := &tm.Members[1]
			member.SchedulingRole, member.Skills, member.Areas = "core", []string{"go"}, Leases{"internal/team/**"}
			task := Task{
				ID: "work", Title: "name must not affect matching", State: "ready",
				Role: "core", Skills: []string{"go"}, Paths: Leases{"internal/team/scheduler.go"},
				Criteria: []TaskCriterion{{ID: "c", Text: "verify"}}, Estimate: TaskEstimate{Parallel: true},
			}
			switch scenario {
			case "role", "explicit-role":
				member.SchedulingRole = "reporting"
			case "skill", "explicit-skill":
				member.Skills = []string{"writing"}
			case "area", "explicit-area":
				member.Areas = Leases{"docs/research/**"}
			case "legacy":
				member.SchedulingRole, member.Skills, member.Areas = "", nil, nil
				task.Role, task.Skills = "worker", nil
			case "epic":
				task.Kind = "epic"
			}
			if strings.HasPrefix(scenario, "explicit-") {
				task.Assignee = member.Name
				task.Worker = member.Name
			}
			if _, err := store.Save(t.Context(), &tm, version); err != nil {
				t.Fatal(err)
			}
			pool, err := OpenPool(t.Context(), store, "pool", boardStore)
			if err != nil {
				t.Fatal(err)
			}
			state := BoardState{
				Version: 2, Board: TaskBoard{Version: 2, Tasks: []Task{task}},
				Policy: SchedulingPolicy{MaxWorkers: 1, Approval: "off"},
			}
			if err := pool.Configure(t.Context(), ReconcileActor{User: true}, &state, 0); err != nil {
				t.Fatal(err)
			}
			snapshot, err := pool.Snapshot(t.Context())
			if err != nil || len(snapshot.Input.Workers) != 1 {
				t.Fatal(snapshot, err)
			}
			worker := &snapshot.Input.Workers[0]
			if !slices.Equal(worker.Skills, member.Skills) || !slices.Equal(worker.Areas, member.Areas) {
				t.Fatal("snapshot lost configured scheduling metadata", worker)
			}
			plan, err := PlanSchedule(&snapshot.Input)
			if err != nil {
				t.Fatal(err)
			}
			wantMatch := scenario == "match" || scenario == "explicit-match" || scenario == "legacy"
			if (len(plan.Assignments) == 1) != wantMatch {
				t.Fatal(scenario, plan)
			}
			if !wantMatch && plan.Blocked[task.ID] == "" {
				t.Fatal("rejection has no actionable reason", plan)
			}
			if strings.Contains(scenario, "role") && !strings.Contains(plan.Blocked[task.ID], "scheduling role") {
				t.Fatal("role rejection reason missing", plan)
			}
			if strings.Contains(scenario, "skill") && !strings.Contains(plan.Blocked[task.ID], "required skill") {
				t.Fatal("skill rejection reason missing", plan)
			}
			if strings.Contains(scenario, "area") && !strings.Contains(plan.Blocked[task.ID], "member areas") {
				t.Fatal("area rejection reason missing", plan)
			}
			if scenario == "epic" && !strings.Contains(plan.Blocked[task.ID], "epic") {
				t.Fatal("epic exclusion lost", plan)
			}
		})
	}
}
