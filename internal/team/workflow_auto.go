package team

import (
	"context"
	"errors"
	"fmt"
)

// AutoReconcile applies what policy allows without a person: assignments always, worker creation
// and retirement only in auto mode. Approvals are never granted here.
func (p *PoolRuntime) AutoReconcile(ctx context.Context) ([]TaskNotice, error) {
	before, err := p.Status(ctx)
	if err != nil {
		return nil, err
	}
	if before.Revision == 0 {
		return nil, nil
	}
	actor := ReconcileActor{Automation: true}
	controller := p.Controller(actor)
	actions, err := controller.Propose(ctx, actor)
	if err != nil {
		return nil, fmt.Errorf("plan automatic reconcile: %w", err)
	}
	var failures []error
	for i := range actions {
		if actions[i].Kind != "assign" && before.Policy.Approval != "auto" {
			continue
		}
		if _, err := controller.Apply(ctx, actor, actions[i].ID); err != nil {
			failures = append(failures, fmt.Errorf("%s %s: %w", actions[i].Kind, actions[i].Task+actions[i].Worker, err))
		}
	}
	after, err := p.Status(ctx)
	if err != nil {
		return nil, errors.Join(append(failures, err)...)
	}
	manager, err := p.manager(ctx)
	if err != nil {
		return nil, errors.Join(append(failures, err)...)
	}
	notices := TaskNotices(&before.Board, &after.Board, &manager.Team, AutomationActor)
	return notices, errors.Join(failures...)
}
