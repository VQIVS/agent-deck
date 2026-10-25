package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/vqivs/agent-deck/internal/team"
)

type taskLeaseReconciler interface{ ReconcileTaskLeases(context.Context) error }

// ReconcileTaskLeases runs at owner boundaries, outside service/credential locks.
// Persisted task ownership and policy fingerprints are the durable retry intent.
func (r *Runtime) ReconcileTaskLeases(ctx context.Context) error {
	manager, store, err := r.taskTools().Provider(ctx)
	if err != nil {
		return fmt.Errorf("load task lease owner: %w", err)
	}
	if manager.ReadOnly || store.ReadOnly || !manager.Team.LeasePool {
		return nil
	}
	if _, err := store.Load(ctx); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("load task lease board: %w", err)
	}
	pool, err := team.OpenPool(ctx, manager.Store, manager.Team.Name, store)
	if err != nil {
		return fmt.Errorf("open task lease pool: %w", err)
	}
	if err := pool.ReconcileTaskLeases(ctx); err != nil {
		return fmt.Errorf("reconcile native task leases: %w", err)
	}
	current, err := r.current(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.manager = current
	r.mu.Unlock()
	return nil
}

func (s *Service) reconcileTaskLeases(ctx context.Context) error {
	if s.options.ReadOnly != nil && s.options.ReadOnly() {
		return nil
	}
	if owner, ok := s.options.Transport.(taskLeaseReconciler); ok {
		if err := owner.ReconcileTaskLeases(ctx); err != nil {
			return fmt.Errorf("owner task lease reconciliation: %w", err)
		}
	}
	return nil
}

func (r *Runtime) reconcileTaskLeaseManager(ctx context.Context) (*team.Manager, error) {
	if err := r.ReconcileTaskLeases(ctx); err != nil {
		return nil, err
	}
	return r.current(ctx)
}
