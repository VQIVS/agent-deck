package mcp

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestActiveTaskPathNativeCapGatesSchedulingAndDelivery(t *testing.T) {
	testActiveTaskPathMutation(t, func(t *testing.T, r *Runtime, tools *TaskTools, service *Service,
		manager *team.Manager, store team.BoardStore, identity Identity, mu *sync.Mutex,
		policies *[][]api.Rule, nativePolicies map[string][]api.Rule,
	) {
		mu.Lock()
		before := len(*policies)
		nativePolicies["same-worker-session"] = make([]api.Rule, api.MaxNativePermissionRules)
		for i := range nativePolicies["same-worker-session"] {
			nativePolicies["same-worker-session"][i] = api.Rule{Permission: "edit", Pattern: "*", Action: "deny"}
		}
		mu.Unlock()
		result, err := tools.Call(t.Context(), identity, "update_task",
			json.RawMessage(`{"id":"T-1","paths":["capped/**"]}`))
		if err != nil || result.(taskResult).Warning == "" {
			t.Fatal("native cap did not produce saved retryable diagnostic", err)
		}
		capped, err := store.Load(t.Context())
		if err != nil || !slices.Equal(capped.Board.Tasks[0].Paths, team.Leases{"capped/**"}) {
			t.Fatal("native cap lost durable task mutation", err)
		}
		pending, err := r.options.Provider(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		member := &pending.Team.Members[0]
		if member.LeasePolicy == pending.Team.LeaseFingerprint(member) {
			t.Fatal("native cap incorrectly persisted successful policy fingerprint")
		}
		pool, err := team.OpenPool(t.Context(), manager.Store, manager.Team.Name, store)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.AutoReconcile(t.Context()); err == nil {
			t.Fatal("native cap did not gate scheduling")
		}
		if err := service.Deliver(t.Context()); err == nil {
			t.Fatal("native cap did not gate delivery")
		}
		mu.Lock()
		defer mu.Unlock()
		if len(*policies) != before || len(nativePolicies["same-worker-session"]) != api.MaxNativePermissionRules {
			t.Fatal("native cap allowed policy append")
		}
	})
}
