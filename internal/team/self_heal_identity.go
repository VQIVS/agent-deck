package team

import (
	"context"
	"fmt"
)

func (t *LifecycleTransport) resumeIdentityCreate(ctx context.Context,
	effect *lifecycleEffect,
) (ReconcileResult, bool, error) {
	manager, err := t.LoadManager(ctx)
	if err != nil {
		return ReconcileResult{}, false, err
	}
	if !t.writableManager(manager) || t.Store.ReadOnly {
		return ReconcileResult{}, false, fmt.Errorf("identity recovery needs writable manager")
	}
	state, err := t.Store.LoadUnlocked(ctx)
	if err != nil {
		return ReconcileResult{}, false, err
	}
	if state.Policy.Approval == "off" || state.Policy.Approval != "auto" && !state.Approved[effect.Request.Action.ID] {
		return ReconcileResult{}, false, fmt.Errorf("identity recovery needs current policy authorization")
	}
	if t.WorkerSetup == nil {
		return ReconcileResult{}, false, fmt.Errorf("identity setup is unavailable")
	}
	result, exists, err := t.resumeWorkerSetup(ctx, manager, effect.Request)
	if err != nil || !exists {
		return result, false, err
	}
	if err := t.commitBoard(ctx, effect.Request, &result); err != nil {
		return result, false, err
	}
	effect.State, effect.Result = "done", result
	return result, true, t.Store.write(ctx, effectName(effect.Request.Action.ID), effect)
}
