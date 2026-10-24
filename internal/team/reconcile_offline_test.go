package team

import (
	"context"
	"testing"
	"time"
)

func TestRecoverMissingActiveWorkerAfterSpareRetirement(t *testing.T) {
	_, snapshot, _, _ := reconcileFixture(t)
	snapshot.Input.Policy = SchedulingPolicy{Approval: "auto", MaxWorkers: 2}
	snapshot.Team.Members = append(snapshot.Team.Members, Member{
		Name: "missing", Role: "worker", Mode: "shared", PoolCreated: true, Session: "ses_missing",
	})
	snapshot.Input.Board = &TaskBoard{Version: 2, Tasks: []Task{{
		ID: "T-1", Title: "Active", State: "in_progress", Assignee: "missing", Worker: "missing",
		Paths: []string{"fixture/**"}, Criteria: []TaskCriterion{{ID: "c1", Text: "Observed"}},
	}}}
	snapshot.Input.Workers = []SchedulerWorker{
		{Name: "missing", Role: "worker", State: "offline"},
		{Name: "spare", Role: "worker", State: "idle", IdleSince: time.Unix(1, 0)},
	}
	actions, err := DesiredReconcileActions(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, action := range actions {
		if action.Kind == "recover" && action.Worker == "missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing recovery: %+v", actions)
	}
}

func TestRecoverIntentDoesNotMaskItsOwnInitialPreparation(t *testing.T) {
	reconciler, snapshot, journal, transport := reconcileFixture(t)
	snapshot.Input.Policy = SchedulingPolicy{Approval: "auto", MaxWorkers: 1}
	snapshot.Team.Members = append(snapshot.Team.Members, Member{
		Name: "missing", Role: "worker", Mode: "shared", PoolCreated: true, Session: "ses_missing",
	})
	snapshot.Input.Workers = []SchedulerWorker{{Name: "missing", Role: "worker", State: "offline"}}
	_, board := poolRuntimeFixture(t, "http://unused")
	state := BoardState{Version: 2, Board: TaskBoard{Version: 2}}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	pool := &PoolRuntime{board: board}
	reconciler.Snapshot = func(ctx context.Context) (ReconcileSnapshot, error) {
		if n := len(journal.entries); n > 0 {
			entry := journal.entries[n-1]
			if err := board.Append(ctx, &entry); err != nil {
				return ReconcileSnapshot{}, err
			}
		}
		copy := *snapshot
		copy.Input.Workers = append([]SchedulerWorker(nil), snapshot.Input.Workers...)
		var err error
		copy.Input.Workers, err = pool.maskHealingWorkers(ctx, copy.Input.Workers)
		return copy, err
	}
	actions, err := DesiredReconcileActions(snapshot)
	if err != nil || len(actions) != 1 {
		t.Fatalf("recovery action: %+v %v", actions, err)
	}
	if _, err := reconciler.Apply(t.Context(), ReconcileActor{Automation: true}, &actions[0]); err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 1 {
		t.Fatal("recovery did not execute")
	}
}

func TestHealingMasksStartedEffectsButNotBareIntents(t *testing.T) {
	_, board := poolRuntimeFixture(t, "http://unused")
	state := BoardState{Version: 2, Board: TaskBoard{Version: 2}}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	entry := ReconcileEntry{Action: ReconcileAction{ID: "recover", Kind: "recover", Worker: "missing"}, State: "intent"}
	if err := board.Append(t.Context(), &entry); err != nil {
		t.Fatal(err)
	}
	pool := &PoolRuntime{board: board}
	workers := []SchedulerWorker{{Name: "missing", State: "offline"}}
	got, err := pool.maskHealingWorkers(t.Context(), workers)
	if err != nil || got[0].State != "offline" {
		t.Fatalf("bare intent masked: %+v %v", got, err)
	}
	if err := board.write(t.Context(), effectName(entry.Action.ID), &lifecycleEffect{State: "started"}); err != nil {
		t.Fatal(err)
	}
	got, err = pool.maskHealingWorkers(t.Context(), workers)
	if err != nil || got[0].State != "waiting" {
		t.Fatalf("started effect not masked: %+v %v", got, err)
	}
}
