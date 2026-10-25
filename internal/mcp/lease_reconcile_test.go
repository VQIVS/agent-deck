package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestOwnerNoticeReleasesNativeLeasesAndRestartRetries(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var patches atomic.Int32
	var policyMu sync.Mutex
	nativePolicies := map[string][]api.Rule{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		policyMu.Lock()
		defer policyMu.Unlock()
		if req.Method == http.MethodGet {
			switch req.URL.Path {
			case "/session/status":
				w.Write([]byte(`{"worker-session":{"type":"busy"}}`))
			case "/permission", "/question", "/session":
				w.Write([]byte(`[]`))
			default:
				if strings.HasPrefix(req.URL.Path, "/session/") {
					id := strings.TrimPrefix(req.URL.Path, "/session/")
					if err := json.NewEncoder(w).Encode(map[string]any{
						"id": id, "permission": nativePolicies[id],
					}); err != nil {
						t.Error(err)
					}
					return
				}
				t.Error("unexpected native read", req.URL)
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		if req.Method != http.MethodPatch {
			t.Error("unexpected native operation", req.Method, req.URL)
			w.WriteHeader(500)
			return
		}
		patches.Add(1)
		var payload struct {
			Permission []api.Rule `json:"permission"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		for _, rule := range payload.Permission {
			if rule.Permission == "edit" && rule.Pattern == "own/**" && rule.Action == "allow" {
				t.Error("terminal task edit grant survived reset")
			}
		}
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		id := strings.TrimPrefix(req.URL.Path, "/session/")
		nativePolicies[id] = append(nativePolicies[id], payload.Permission...)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	r, manager := poolIdentityFixture(t)
	manager.Team.Server = server.URL
	manager.Team.LeasePool = true
	manager.Team.Members[0].Session = "worker-session"
	manager.Team.Members[0].Leases = nil
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := team.RecordTaskLeaseGrant(t.Context(), manager, 0, []string{"own/**"}); err != nil {
		t.Fatal(err)
	}
	manager.Team.Members[0].Leases = team.Leases{"own/**"}
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	board := team.BoardStore{Root: filepath.Join(t.TempDir(), "boards"), Team: manager.Team.Name}
	definition := team.DefaultWorkflow()
	for i := range definition.Stages {
		if definition.Stages[i].Category == "active" {
			definition.Stages[i].Name = "Building"
		}
		if definition.Stages[i].Category == "review" {
			definition.Stages[i].Name = "Checking"
		}
	}
	manager.Team.Workflow = definition
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	state := team.BoardState{Version: 2, Policy: team.SchedulingPolicy{Approval: "off"}, Board: team.TaskBoard{
		Version: 2, Workflow: definition,
		Tasks: []team.Task{{
			ID: "T-1", Title: "active work", State: "Building", Assignee: manager.Team.Members[0].Name,
			Reviewer: manager.Team.Members[1].Name,
			Worker:   manager.Team.Members[0].Name, Paths: team.Leases{"own/**"},
			Criteria: []team.TaskCriterion{{ID: "c1", Text: "works"}},
		}, {
			ID: "T-2", Title: "next work", State: "ready", Paths: team.Leases{"next/**"},
			Criteria: []team.TaskCriterion{{ID: "c1", Text: "next task works"}},
		}},
	}}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	provider := func(ctx context.Context) (*team.Manager, error) {
		current, version, err := manager.Store.Load(ctx, manager.Team.Name)
		return &team.Manager{Store: manager.Store, Team: current, Version: version}, err
	}
	r.manager = manager
	r.options.Provider = provider
	r.options.Tasks = &TaskTools{Provider: func(ctx context.Context) (*team.Manager, team.BoardStore, error) {
		m, e := provider(ctx)
		return m, board, e
	}}
	service := newTestService(t, func(options *Options) {
		options.Team = manager.Team.Name
		options.Transport = r
		options.Instructions = func(context.Context, string) (string, error) { return "", nil }
		options.Members = func() []Member {
			return []Member{{Name: manager.Team.Members[1].Name, Role: "master", ID: "lead-id"}}
		}
	})
	tools := r.options.Tasks
	tools.Notify = service.Notify
	result, err := tools.Call(t.Context(), Identity{Team: manager.Team.Name, Member: manager.Team.Members[0].Name},
		"update_my_task", json.RawMessage(`{"id":"T-1","state":"Checking"}`))
	if err != nil {
		t.Fatal("committed transition failed", err)
	}
	if result.(taskResult).Warning == "" {
		t.Fatal("native failure invisible")
	}
	records, err := service.options.Journal.Snapshot(t.Context())
	if err != nil || len(records) == 0 {
		t.Fatal("committed notices lost", records, err)
	}
	loaded, err := board.Load(t.Context())
	if err != nil || loaded.Board.Tasks[0].State != "Checking" {
		t.Fatal("committed task lost", err)
	}
	if err := service.Deliver(t.Context()); err == nil {
		t.Fatal("native failure did not gate worker wake")
	}
	pool, err := team.OpenPool(t.Context(), manager.Store, manager.Team.Name, board)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.AutoReconcile(t.Context()); err == nil {
		t.Fatal("native reset failure did not block scheduling")
	}
	blocked, err := board.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Revision != loaded.Revision || blocked.Board.Tasks[1].Assignee != "" {
		t.Fatal("scheduler changed ready work before native repair")
	}
	fail.Store(false)
	restarted := &Runtime{manager: manager, options: r.options}
	restartedPool, err := team.OpenPool(t.Context(), manager.Store, manager.Team.Name, board)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restartedPool.AutoReconcile(t.Context()); err != nil {
		t.Fatal("restart repair", err)
	}
	current, err := provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Team.Members[0].Leases) != 0 || patches.Load() < 2 {
		t.Fatal("policy not repaired", patches.Load())
	}
	before := patches.Load()
	if err := restarted.ReconcileTaskLeases(t.Context()); err != nil || patches.Load() != before {
		t.Fatal("successful repair repeated", err)
	}
	current.Team.Members[0].Capability = "inherited"
	current.Team.Members[0].Permissions = nil
	current.Team.Members[0].LeasePolicy = ""
	if err := current.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.ReconcileTaskLeases(t.Context()); err != nil || patches.Load() != before {
		t.Fatal("inherited native policy was replaced", err)
	}
	original := restarted.options.Tasks.Provider
	restarted.options.Tasks = &TaskTools{Provider: func(ctx context.Context) (*team.Manager, team.BoardStore, error) {
		m, s, e := original(ctx)
		m.ReadOnly = true
		return m, s, e
	}}
	if err := restarted.ReconcileTaskLeases(t.Context()); err != nil || patches.Load() != before {
		t.Fatal("read-only reconciliation caused native effects", err)
	}
}
