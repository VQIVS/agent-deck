package team

import (
	"context"
	"fmt"
	"testing"
)

func TestLifecycleTransportUncertainEffectsNeverRetry(t *testing.T) {
	store, _ := boardStoreFixture(t)
	transport := &LifecycleTransport{Store: store}
	id := "uncertain-action"
	if err := store.Exclusive(t.Context(), func() error {
		return store.write(t.Context(), effectName(id), &lifecycleEffect{State: "started"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := transport.Lookup(t.Context(), id); err == nil {
		t.Fatal("uncertain effects silently retried")
	}
	if err := store.Exclusive(t.Context(), func() error {
		return store.write(t.Context(), effectName(id), &lifecycleEffect{
			State: "done", Result: ReconcileResult{Worker: "worker", Session: "session"},
		})
	}); err != nil {
		t.Fatal(err)
	}
	result, done, err := transport.Lookup(t.Context(), id)
	if err != nil || !done || result.Session != "session" {
		t.Fatal("committed effect not recovered", result, err)
	}
}

func TestLifecycleTransportPolicyBeforeEffects(t *testing.T) {
	store, _ := boardStoreFixture(t)
	transport := &LifecycleTransport{
		Store: store,
		LoadManager: func(context.Context) (*Manager, error) {
			return &Manager{Team: Team{Name: "pool"}}, nil
		},
		Gate: func(context.Context, *ReconcileAction) error { return fmt.Errorf("approval revoked") },
	}
	request := &ReconcileRequest{Action: ReconcileAction{ID: "blocked", Kind: "create"}}
	if _, err := transport.Execute(t.Context(), request); err == nil {
		t.Fatal("revoked policy executed")
	}
	if _, done, err := transport.Lookup(t.Context(), "blocked"); err != nil || done {
		t.Fatal("gate denial left effects", err)
	}
}
