package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestActiveTaskPathMutationReconcilesOwnerPolicy(t *testing.T) {
	r, manager := poolIdentityFixture(t)
	var mu sync.Mutex
	var policies [][]api.Rule
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPatch {
			t.Errorf("unexpected session operation: %s %s", req.Method, req.URL)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var payload struct {
			Permission []api.Rule `json:"permission"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		mu.Lock()
		defer mu.Unlock()
		policies = append(policies, payload.Permission)
		if fail {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	manager.Team.Server, manager.Team.LeasePool = server.URL, true
	worker := &manager.Team.Members[0]
	worker.Session = "same-worker-session"
	worker.Leases = team.Leases{"manual/**", "shared/**"}
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := team.RecordTaskLeaseGrant(t.Context(), manager, 0, []string{"old/**", "shared/**"}); err != nil {
		t.Fatal(err)
	}
	worker.Leases = append(worker.Leases, "old/**")
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	store := team.BoardStore{Root: filepath.Join(t.TempDir(), "boards"), Team: manager.Team.Name}
	state := team.BoardState{Version: 2, Board: team.TaskBoard{Version: 2, Tasks: []team.Task{{
		ID: "T-1", Title: "active paths", State: "in_progress", Assignee: worker.Name, Worker: worker.Name,
		Paths: team.Leases{"old/**", "shared/**"}, Criteria: []team.TaskCriterion{{ID: "c1", Text: "works"}},
	}}}}
	if err := store.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	provider := func(ctx context.Context) (*team.Manager, error) {
		current, version, err := manager.Store.Load(ctx, manager.Team.Name)
		return &team.Manager{Store: manager.Store, Team: current, Version: version}, err
	}
	r.manager, r.options.Provider = manager, provider
	tools := &TaskTools{Provider: func(ctx context.Context) (*team.Manager, team.BoardStore, error) {
		current, err := provider(ctx)
		return current, store, err
	}}
	r.options.Tasks = tools
	service := newTestService(t, func(options *Options) {
		options.Team, options.Transport = manager.Team.Name, r
		options.Instructions = func(context.Context, string) (string, error) { return "", nil }
	})
	tools.Notify = service.Notify
	identity := Identity{Team: manager.Team.Name, Member: manager.Team.Members[1].Name}
	mutate := func(paths []string) taskResult {
		t.Helper()
		arguments, err := json.Marshal(map[string]any{"id": "T-1", "paths": paths})
		if err != nil {
			t.Fatal(err)
		}
		result, err := tools.Call(t.Context(), identity, "update_task", arguments)
		if err != nil {
			t.Fatal(err)
		}
		return result.(taskResult)
	}
	assertPolicy := func(wanted, unwanted string) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if len(policies) == 0 {
			t.Fatal("paths-only mutation skipped native policy callback")
		}
		rules := policies[len(policies)-1]
		allows := func(path string) bool {
			return slices.ContainsFunc(rules, func(rule api.Rule) bool {
				return rule.Permission == "edit" && rule.Pattern == path && rule.Action == "allow"
			})
		}
		if !allows(wanted) || unwanted != "" && allows(unwanted) || !allows("manual/**") || !allows("shared/**") {
			t.Fatalf("incorrect effective native edit grants: %+v", rules)
		}
	}
	if result := mutate([]string{"old/**", "shared/**", "config/**"}); result.Warning != "" {
		t.Fatal(result.Warning)
	}
	assertPolicy("config/**", "")
	if result := mutate([]string{"config/**"}); result.Warning != "" {
		t.Fatal(result.Warning)
	}
	assertPolicy("config/**", "old/**")
	current, err := provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(current.Team.Members[0].Leases, team.Leases{"config/**", "manual/**", "shared/**"}) ||
		current.Team.Members[0].Session != "same-worker-session" {
		t.Fatal("effective leases/session incorrect", current.Team.Members[0])
	}
	mu.Lock()
	fail = true
	mu.Unlock()
	if result := mutate([]string{"retry/**"}); result.Warning == "" {
		t.Fatal("native failure did not return saved/retry diagnostic")
	}
	persisted, err := store.Load(t.Context())
	if err != nil || !slices.Equal(persisted.Board.Tasks[0].Paths, team.Leases{"retry/**"}) {
		t.Fatal("native failure lost committed task", err)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	restarted := &Runtime{manager: manager, options: r.options}
	if err := restarted.ReconcileTaskLeases(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertPolicy("retry/**", "config/**")
	repaired, err := provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(repaired.Team.Members[0].Leases, team.Leases{"manual/**", "retry/**", "shared/**"}) {
		t.Fatal("restart did not persist repaired effective leases", repaired.Team.Members[0].Leases)
	}
	records, err := service.options.Journal.Snapshot(t.Context())
	if err != nil || len(records) != 0 {
		t.Fatal("paths-only mutations fabricated workflow notices", records, err)
	}
	mu.Lock()
	before := len(policies)
	mu.Unlock()
	if _, err := tools.Call(t.Context(), identity, "comment_task",
		json.RawMessage(`{"id":"T-1","text":"policy unchanged"}`)); err != nil {
		t.Fatal(err)
	}
	workerIdentity := Identity{Team: manager.Team.Name, Member: manager.Team.Members[0].Name}
	if _, err := tools.Call(t.Context(), workerIdentity, "update_task",
		json.RawMessage(`{"id":"T-1","paths":["unauthorized/**"]}`)); err == nil {
		t.Fatal("worker edited master-owned task fields")
	}
	originalProvider := tools.Provider
	tools.Provider = func(ctx context.Context) (*team.Manager, team.BoardStore, error) {
		current, board, err := originalProvider(ctx)
		if current != nil {
			current.ReadOnly = true
		}
		return current, board, err
	}
	if _, err := tools.Call(t.Context(), identity, "update_task",
		json.RawMessage(`{"id":"T-1","paths":["readonly/**"]}`)); err == nil {
		t.Fatal("read-only task paths changed")
	}
	tools.Provider = originalProvider
	unchanged, err := store.Load(t.Context())
	if err != nil || !slices.Equal(unchanged.Board.Tasks[0].Paths, team.Leases{"retry/**"}) {
		t.Fatal("denied mutation changed task paths", err)
	}
	if err := restarted.ReconcileTaskLeases(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(policies) != before {
		t.Fatal("restart repair was not idempotent")
	}
}
