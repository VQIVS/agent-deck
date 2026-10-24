package team

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type reconcileMemory struct {
	mu       sync.Mutex
	entries  []ReconcileEntry
	failDone bool
}

func (m *reconcileMemory) Exclusive(_ context.Context, fn func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn()
}

func (m *reconcileMemory) Entries(context.Context) ([]ReconcileEntry, error) {
	return m.entries, nil
}

func (m *reconcileMemory) Append(_ context.Context, entry *ReconcileEntry) error {
	if entry.State == "done" && m.failDone {
		return fmt.Errorf("disk unavailable")
	}
	m.entries = append(m.entries, *entry)
	return nil
}

type reconcileFakeTransport struct {
	results  map[string]ReconcileResult
	requests []ReconcileRequest
}

func (f *reconcileFakeTransport) Lookup(_ context.Context, id string) (ReconcileResult, bool, error) {
	result, found := f.results[id]
	return result, found, nil
}

func (f *reconcileFakeTransport) Execute(_ context.Context, request *ReconcileRequest) (ReconcileResult, error) {
	f.requests = append(f.requests, *request)
	result := ReconcileResult{Worker: request.Member.Name, Session: "ses_new"}
	f.results[request.Action.ID] = result
	return result, nil
}

func reconcileFixture(t *testing.T) (*Reconciler, *ReconcileSnapshot, *reconcileMemory, *reconcileFakeTransport) {
	t.Helper()
	snapshot := &ReconcileSnapshot{
		Team:     Team{Name: "pool", Repo: "/repo", Members: []Member{{Name: "lead", Role: "master", Session: "ses_lead"}}},
		Revision: "revision-1", Approved: map[string]bool{},
		Input: SchedulingInput{
			Board: &TaskBoard{Version: 1}, Now: time.Now(),
			Policy: SchedulingPolicy{MinWorkers: 1, MaxWorkers: 2, Approval: "ask"},
		},
	}
	journal := &reconcileMemory{}
	transport := &reconcileFakeTransport{results: map[string]ReconcileResult{}}
	r := &Reconciler{
		Journal: journal, Transport: transport,
		Snapshot: func(context.Context) (ReconcileSnapshot, error) { return *snapshot, nil },
	}
	return r, snapshot, journal, transport
}

func TestReconcileApprovalAndCrashRecovery(t *testing.T) {
	r, snapshot, journal, transport := reconcileFixture(t)
	actions, err := DesiredReconcileActions(snapshot)
	if err != nil || len(actions) != 1 {
		t.Fatal(actions, err)
	}
	action := &actions[0]
	actor := ReconcileActor{Member: "lead"}
	if _, err := r.Apply(t.Context(), actor, action); err == nil || len(journal.entries) != 0 {
		t.Fatal("unapproved action persisted", err)
	}
	snapshot.Approved[action.ID] = true
	journal.failDone = true
	if _, err := r.Apply(t.Context(), actor, action); err == nil || len(transport.requests) != 1 {
		t.Fatal("expected completion persistence failure", err)
	}
	journal.failDone = false
	snapshot.Revision = "after-create"
	r = &Reconciler{Journal: journal, Transport: transport, Snapshot: r.Snapshot}
	if _, err := r.Apply(t.Context(), actor, action); err != nil || len(transport.requests) != 1 {
		t.Fatal("crash recovery duplicated effect", err)
	}
	if _, err := r.Apply(t.Context(), actor, action); err != nil || len(transport.requests) != 1 {
		t.Fatal("done replay duplicated effect", err)
	}
	assertLeaseRule(t, transport.requests[0].Rules, "edit", "*", "deny")
	assertLeaseRule(t, transport.requests[0].Rules, "bash", "git commit*", "deny")
}

func TestReconcileRechecksCurrentAuthorization(t *testing.T) {
	for _, mutation := range []string{"read-only", "role", "approval", "revision"} {
		t.Run(mutation, func(t *testing.T) {
			r, snapshot, _, transport := reconcileFixture(t)
			actions, err := DesiredReconcileActions(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			action := &actions[0]
			snapshot.Approved[action.ID] = true
			reads := 0
			r.Snapshot = func(context.Context) (ReconcileSnapshot, error) {
				reads++
				if reads == 2 {
					switch mutation {
					case "read-only":
						snapshot.Input.ReadOnly = true
					case "role":
						snapshot.Team.Members[0].Role = "worker"
					case "approval":
						delete(snapshot.Approved, action.ID)
					case "revision":
						snapshot.Revision = "changed"
					}
				}
				return *snapshot, nil
			}
			if _, err := r.Apply(t.Context(), ReconcileActor{Member: "lead"}, action); err == nil {
				t.Fatal("stale action executed")
			}
			if len(transport.requests) != 0 {
				t.Fatal("transport called after policy changed")
			}
		})
	}
}

func TestReconcileRejectsForgedActionAndWorker(t *testing.T) {
	r, snapshot, journal, transport := reconcileFixture(t)
	actions, err := DesiredReconcileActions(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Approved[actions[0].ID] = true
	if _, err := r.Apply(t.Context(), ReconcileActor{Member: "worker"}, &actions[0]); err == nil {
		t.Fatal("worker authorized")
	}
	actions[0].Proposal.Model = "forged"
	if _, err := r.Apply(t.Context(), ReconcileActor{User: true}, &actions[0]); err == nil {
		t.Fatal("forged approved action accepted")
	}
	if len(journal.entries) != 0 || len(transport.requests) != 0 {
		t.Fatal("rejected action caused side effects")
	}
}

func TestReconcileAssignmentAndRetirement(t *testing.T) {
	r, snapshot, _, transport := reconcileFixture(t)
	snapshot.Input.Policy = SchedulingPolicy{MaxWorkers: 2, Approval: "auto"}
	snapshot.Team.Members = append(snapshot.Team.Members, Member{
		Name: "worker", Role: "worker", Session: "ses_worker", Mode: "shared", Dir: "/repo", Permissions: Preset("coder"),
	})
	snapshot.Input.Workers = []SchedulerWorker{
		{Name: "worker", State: "idle", IdleSince: snapshot.Input.Now.Add(-time.Hour)},
	}
	snapshot.Input.Board.Tasks = []Task{{
		ID: "task", Title: "Implement", State: "pending", Paths: Leases{"owned/**"},
		Estimate: TaskEstimate{Parallel: true},
	}}
	actions, err := DesiredReconcileActions(snapshot)
	if err != nil || len(actions) != 1 || actions[0].Kind != "assign" {
		t.Fatal("assignment not planned", actions, err)
	}
	if _, err := r.Apply(t.Context(), ReconcileActor{User: true}, &actions[0]); err != nil {
		t.Fatal(err)
	}
	request := &transport.requests[0]
	if request.Member.Session != "ses_worker" {
		t.Fatal("assignment lost binding")
	}
	assertLeaseRule(t, request.Rules, "edit", "owned/**", "allow")
	snapshot.Input.Board.Tasks = nil
	snapshot.Revision = "retire"
	actions, err = DesiredReconcileActions(snapshot)
	if err != nil || len(actions) != 1 || actions[0].Kind != "retire" {
		t.Fatal("retirement not planned", actions, err)
	}
	if _, err := r.Apply(t.Context(), ReconcileActor{Member: "lead"}, &actions[0]); err != nil {
		t.Fatal(err)
	}
	if transport.requests[1].Member.Session != "ses_worker" {
		t.Fatal("retirement lost bound identity")
	}
}
