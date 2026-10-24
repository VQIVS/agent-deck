package team

import (
	"context"
	"errors"
	"fmt"
)

// ResumeHealing retries durable, policy-authorized healing effects before new scheduling.
func (p *PoolRuntime) ResumeHealing(ctx context.Context) error {
	if p.board.ReadOnly {
		return fmt.Errorf("read-only: healing recovery disabled")
	}
	entries, err := p.board.Entries(ctx)
	if err != nil {
		return err
	}
	latest := make(map[string]*ReconcileEntry)
	for i := range entries {
		latest[entries[i].Action.ID] = &entries[i]
	}
	controller := p.Controller(ReconcileActor{Automation: true})
	var failures []error
	for _, entry := range latest {
		if entry.State != "intent" || !gatedLifecycle(entry.Action.Kind) {
			continue
		}
		if _, err := controller.Reconciler.Apply(ctx, ReconcileActor{Automation: true}, &entry.Action); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
