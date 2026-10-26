package team

import (
	"context"
	"fmt"
	"slices"
)

// BoardController exposes authenticated planning and action execution without direct session calls.
type BoardController struct {
	Store      BoardStore
	Reconciler *Reconciler
	Snapshot   func(context.Context) (ReconcileSnapshot, error)
}

func (c *BoardController) Propose(ctx context.Context, actor ReconcileActor) ([]ReconcileAction, error) {
	if c.Snapshot == nil {
		return nil, fmt.Errorf("board controller requires current snapshot callback")
	}
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("read board planning snapshot: %w", err)
	}
	if err := authorizeReconcile(&snapshot, actor); err != nil {
		return nil, err
	}
	return DesiredReconcileActions(&snapshot)
}

func (c *BoardController) Apply(ctx context.Context, actor ReconcileActor, actionID string) (ReconcileResult, error) {
	if c.Reconciler == nil {
		return ReconcileResult{}, fmt.Errorf("board controller requires reconciler")
	}
	actions, err := c.Propose(ctx, actor)
	if err != nil {
		return ReconcileResult{}, err
	}
	for i := range actions {
		if actions[i].ID == actionID {
			return c.Reconciler.Apply(ctx, actor, &actions[i])
		}
	}
	entries, err := c.Store.Entries(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Action.ID == actionID {
			return c.Reconciler.Apply(ctx, actor, &entries[i].Action)
		}
	}
	return ReconcileResult{}, fmt.Errorf("unknown desired action ID")
}

func (c *BoardController) Assign(ctx context.Context, actor ReconcileActor,
	task, worker string,
) (ReconcileResult, error) {
	actions, err := c.Propose(ctx, actor)
	if err != nil {
		return ReconcileResult{}, err
	}
	index := slices.IndexFunc(actions, func(action ReconcileAction) bool {
		return action.Kind == "assign" && action.Task == task && action.Worker == worker
	})
	if index < 0 {
		return ReconcileResult{}, fmt.Errorf("assignment is not currently desired")
	}
	return c.Apply(ctx, actor, actions[index].ID)
}

func (c *BoardController) Retire(ctx context.Context, actor ReconcileActor, worker string) (ReconcileResult, error) {
	actions, err := c.Propose(ctx, actor)
	if err != nil {
		return ReconcileResult{}, err
	}
	index := slices.IndexFunc(actions, func(action ReconcileAction) bool {
		return action.Kind == "retire" && action.Worker == worker
	})
	if index < 0 {
		return ReconcileResult{}, fmt.Errorf("retirement is not currently desired")
	}
	return c.Apply(ctx, actor, actions[index].ID)
}
