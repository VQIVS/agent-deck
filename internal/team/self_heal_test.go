package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSelfHealDesiredPolicyAndOwnership(t *testing.T) {
	for _, tc := range []struct{ state, kind string }{
		{"idle", "compact"}, {"busy", "replace"}, {"stalled", "replace"}, {"offline", "recover"}, {"waiting", ""},
	} {
		t.Run(tc.state, func(t *testing.T) {
			_, snapshot, _, _ := reconcileFixture(t)
			snapshot.Input.Policy = SchedulingPolicy{Approval: "auto", MaxWorkers: 1, ContextLimit: 100}
			snapshot.Team.Members = append(snapshot.Team.Members, Member{
				Name: "worker", Role: "worker",
				Mode: "shared", PoolCreated: true, Session: "ses_worker",
			})
			snapshot.Input.Workers = []SchedulerWorker{{Name: "worker", Role: "worker", State: tc.state, ContextTokens: 100}}
			plan, err := PlanSchedule(&snapshot.Input)
			if err != nil {
				t.Fatal(err)
			}
			actions := selfHealActions(snapshot, &plan)
			if tc.kind == "" {
				if len(actions) != 0 {
					t.Fatal(actions)
				}
				return
			}
			if len(actions) != 1 || actions[0].Kind != tc.kind {
				t.Fatal(actions)
			}
			snapshot.Team.Members[1].PoolCreated = false
			if len(selfHealActions(snapshot, &plan)) != 0 {
				t.Fatal("user session action")
			}
			snapshot.Team.Members[1].PoolCreated = true
			snapshot.Input.ReadOnly = true
			if len(selfHealActions(snapshot, &plan)) != 0 {
				t.Fatal("read-only action")
			}
			snapshot.Input.ReadOnly = false
			snapshot.Input.Policy.Approval = "off"
			if len(selfHealActions(snapshot, &plan)) != 0 {
				t.Fatal("off action")
			}
		})
	}
}

func healingFixture(t *testing.T, kind string) (*LifecycleTransport, *Manager, *ReconcileRequest, *int) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts++
			_, _ = fmt.Fprint(w, `true`)
			return
		}
		switch r.URL.Path {
		case "/session/status":
			_, _ = fmt.Fprint(w, `{}`)
		case "/session":
			if kind == "recover" {
				_, _ = fmt.Fprint(w, `[]`)
			} else {
				_, _ = fmt.Fprint(w, `[{"id":"ses_worker","time":{"updated":1}}]`)
			}
		default:
			_, _ = fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(server.Close)
	store, board := poolRuntimeFixture(t, server.URL)
	pool, err := OpenPool(t.Context(), store, "pool", board)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := pool.manager(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	member := &manager.Team.Members[1]
	member.PoolCreated, member.Mode, member.Owned, member.Model = true, "shared", false, "provider/model"
	member.Dir, member.Leases = manager.Team.Repo, nil
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	state := BoardState{
		Version: 2, Board: TaskBoard{Version: 2},
		Policy: SchedulingPolicy{Approval: "auto", MaxWorkers: 2},
	}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	request := &ReconcileRequest{
		Actor:  AutomationActor,
		Action: ReconcileAction{ID: "healing", Kind: kind, Worker: manager.Team.Members[1].Name},
		Member: manager.Team.Members[1],
	}
	transport := &LifecycleTransport{
		Store: board, LoadManager: pool.manager,
		Gate: func(context.Context, *ReconcileAction) error { return nil },
		WorkerSetup: func(ctx context.Context, m *Manager, index int) error {
			m.Team.Members[index].Session = "ses_replacement"
			return m.Save(ctx)
		},
	}
	return transport, manager, request, &posts
}

func TestSelfHealCompactionAndOfflineRecoveryIdempotent(t *testing.T) {
	for _, kind := range []string{"compact", "recover", "replace"} {
		t.Run(kind, func(t *testing.T) {
			transport, _, request, posts := healingFixture(t, kind)
			result, err := transport.Execute(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			before := *posts
			if _, err := transport.Execute(t.Context(), request); err != nil || *posts != before {
				t.Fatalf("duplicate effect: posts %d->%d err=%v", before, *posts, err)
			}
			if kind != "compact" && result.Session != "ses_replacement" {
				t.Fatal(result)
			}
		})
	}
}

func TestSelfHealUncertainAndDeniedEffects(t *testing.T) {
	transport, _, request, posts := healingFixture(t, "compact")
	effect := lifecycleEffect{State: "compacting", Request: request}
	if err := transport.Store.write(t.Context(), effectName(request.Action.ID), &effect); err != nil {
		t.Fatal(err)
	}
	if _, _, err := transport.Lookup(t.Context(), request.Action.ID); err == nil || *posts != 0 {
		t.Fatal("uncertain compaction repeated", err)
	}
}

func TestSelfHealLifecycleGatesBeforeEffects(t *testing.T) {
	for _, deny := range []string{"user-session", "read-only", "capacity", "approval"} {
		t.Run(deny, func(t *testing.T) {
			transport, manager, request, posts := healingFixture(t, "recover")
			switch deny {
			case "user-session":
				manager.Team.Members[1].PoolCreated = false
				if err := manager.Save(t.Context()); err != nil {
					t.Fatal(err)
				}
			case "read-only":
				transport.Store.ReadOnly = true
			case "capacity":
				manager.Team.Capacity = 1
				manager.Team.Members = append(manager.Team.Members, Member{
					Name: "other", Role: "worker",
					Mode: "shared", Dir: manager.Team.Repo, Session: "ses_other",
				})
				transport.LoadManager = func(context.Context) (*Manager, error) { return manager, nil }
			case "approval":
				transport.Gate = func(context.Context, *ReconcileAction) error { return fmt.Errorf("approval denied") }
			}
			if _, err := transport.Execute(t.Context(), request); err == nil || *posts != 0 {
				t.Fatal("denied action caused effects", err, *posts)
			}
		})
	}
}

func TestSelfHealResumeAfterSetupFailure(t *testing.T) {
	transport, _, request, posts := healingFixture(t, "recover")
	setup := transport.WorkerSetup
	transport.WorkerSetup = func(context.Context, *Manager, int) error { return fmt.Errorf("setup unavailable") }
	if _, err := transport.Execute(t.Context(), request); err == nil {
		t.Fatal("setup failure ignored")
	}
	transport.WorkerSetup = setup
	result, done, err := transport.Lookup(t.Context(), request.Action.ID)
	if err != nil || !done || result.Session != "ses_replacement" || *posts != 1 {
		t.Fatalf("recovery %+v done=%v posts=%d err=%v", result, done, *posts, err)
	}
	if _, done, err := transport.Lookup(t.Context(), request.Action.ID); err != nil || !done || *posts != 1 {
		t.Fatal("completed recovery repeated handoff", err)
	}
}

func TestSelfHealRecoveryRejectsReboundMember(t *testing.T) {
	for _, phase := range []string{"prepared", "aborted", "unbound", "healed"} {
		for _, change := range []string{"identity", "session", "control"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				transport, manager, request, posts := healingFixture(t, "replace")
				effect := lifecycleEffect{State: phase, Request: request, Handoff: "durable handoff"}
				member := &manager.Team.Members[1]
				switch change {
				case "identity":
					member.ID = "unrelated-new-member"
				case "session":
					member.Session = "externally-rebound"
				case "control":
					member.ControlDir = t.TempDir()
				}
				transport.LoadManager = func(context.Context) (*Manager, error) { return manager, nil }
				setups := 0
				transport.WorkerSetup = func(context.Context, *Manager, int) error { setups++; return nil }
				if err := transport.Store.write(t.Context(), effectName(request.Action.ID), &effect); err != nil {
					t.Fatal(err)
				}
				if _, _, err := transport.Lookup(t.Context(), request.Action.ID); err == nil {
					t.Fatal("unrelated binding accepted")
				}
				if *posts != 0 || setups != 0 {
					t.Fatalf("effects: posts=%d setup=%d", *posts, setups)
				}
			})
		}
	}
}

func TestSelfHealRecoveryAcceptsOnlyRecordedReplacement(t *testing.T) {
	transport, manager, request, posts := healingFixture(t, "replace")
	member := &manager.Team.Members[1]
	member.Session, member.ControlDir = "recorded-replacement", t.TempDir()
	bound := *member
	effect := lifecycleEffect{State: "healed", Request: request, Binding: &bound}
	transport.LoadManager = func(context.Context) (*Manager, error) { return manager, nil }
	if err := transport.Store.write(t.Context(), effectName(request.Action.ID), &effect); err != nil {
		t.Fatal(err)
	}
	result, done, err := transport.Lookup(t.Context(), request.Action.ID)
	if err != nil || !done || result.Session != bound.Session || *posts != 0 {
		t.Fatalf("recorded replacement recovery %+v done=%v effects=%d err=%v", result, done, *posts, err)
	}
}

func TestSelfHealReleaseTasksPreservesChecklist(t *testing.T) {
	var board TaskBoard
	err := json.Unmarshal([]byte(`{"version":2,"tasks":[{"id":"T-1","title":"task","state":"in_progress",
		"assignee":"worker","worker":"worker","paths":["internal/test.go"],
		"criteria":[{"id":"c1","text":"done","checked":true}],
		"estimate":{"duration":"1m","parallel":true}}]}`), &board)
	if err != nil {
		t.Fatal(err)
	}
	request := &ReconcileRequest{
		Actor: AutomationActor, Action: ReconcileAction{Kind: "recover"},
		Member: Member{Name: "worker"},
	}
	if err := releaseHealingTasks(&board, request); err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].State != "ready" || board.Tasks[0].Assignee != "" || !board.Tasks[0].Criteria[0].Checked {
		t.Fatal(board.Tasks[0])
	}
	input := SchedulingInput{
		Board: &board, Now: time.Now(), Policy: SchedulingPolicy{Approval: "auto", MaxWorkers: 1},
		Workers: []SchedulerWorker{{Name: "replacement", Role: "worker", State: "idle"}},
	}
	plan, err := PlanSchedule(&input)
	if err != nil || len(plan.Assignments) != 1 || plan.Assignments[0].Worker != "replacement" {
		t.Fatalf("released task not schedulable: %+v %v", plan, err)
	}
	threshold := time.Minute
	if !stateStalled(&threshold, &MemberStatus{LastActivity: time.Now().Add(-2 * time.Minute).UnixMilli()}, time.Now()) {
		t.Fatal("stalled activity ignored")
	}
}

func TestAssignSkipsPoolSetupForConfiguredWorkers(t *testing.T) {
	transport, manager, request, _ := healingFixture(t, "assign")
	manager.Team.Members[1].PoolCreated = false
	manager.Team.Members[1].ControlDir = t.TempDir()
	transport.LoadManager = func(context.Context) (*Manager, error) { return manager, nil }
	request.Member = manager.Team.Members[1]
	transport.WorkerSetup = func(context.Context, *Manager, int) error {
		return fmt.Errorf("automatic workflow setup requires a pool-created shared worker")
	}
	_, err := transport.Execute(t.Context(), request)
	if err != nil && strings.Contains(err.Error(), "prepare worker identity") {
		t.Fatalf("assign ran pool identity setup for a configured worker: %v", err)
	}
}
