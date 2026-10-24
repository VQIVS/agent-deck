package team

import (
	"context"
	"fmt"
	"strings"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

// PoolRecovery identifies a durable uncertain action and any known orphan session.
type PoolRecovery struct {
	State   string            `json:"state"`
	Result  ReconcileResult   `json:"result"`
	Request *ReconcileRequest `json:"request,omitempty"`
	Handoff string            `json:"handoff,omitempty"`
	Binding *Member           `json:"binding,omitempty"`
}

func (t *LifecycleTransport) Inspect(ctx context.Context, actor ReconcileActor, id string) (PoolRecovery, error) {
	if !actor.User {
		return PoolRecovery{}, fmt.Errorf("pool recovery requires privileged user")
	}
	var effect lifecycleEffect
	if err := t.Store.read(ctx, effectName(id), &effect); err != nil {
		return PoolRecovery{}, err
	}
	if effect.Request == nil || effect.Request.Action.ID != id {
		return PoolRecovery{}, fmt.Errorf("action has no recoverable request")
	}
	if effect.Result.Session == "" && effect.Request.Action.Kind == "create" {
		manager, err := t.LoadManager(ctx)
		if err != nil {
			return PoolRecovery{}, fmt.Errorf("load recovery manager: %w", err)
		}
		sessions, err := api.New(manager.Team.Server, true).Sessions(ctx, effect.Request.Member.Dir)
		if err != nil {
			return PoolRecovery{}, fmt.Errorf("find orphan session: %w", err)
		}
		for _, session := range sessions {
			if strings.HasSuffix(session.Title, " [pool action "+id+"]") {
				if effect.Result.Session != "" {
					return PoolRecovery{}, fmt.Errorf("multiple orphan sessions match action; inspect manually")
				}
				effect.Result = ReconcileResult{Worker: effect.Request.Member.Name, Session: session.ID}
			}
		}
	}
	return PoolRecovery(effect), nil
}

// Resolve requires explicit user confirmation; adopt binds the known session, delete removes only an unbound orphan.
func (t *LifecycleTransport) Resolve(ctx context.Context, actor ReconcileActor, id, decision string) error {
	if !actor.User || t.Store.ReadOnly {
		return fmt.Errorf("recovery resolution requires writable privileged user action")
	}
	return t.Store.Exclusive(ctx, func() error {
		return t.resolve(ctx, actor, id, decision)
	})
}

func (t *LifecycleTransport) resolve(ctx context.Context, actor ReconcileActor, id, decision string) error {
	recovery, err := t.Inspect(ctx, actor, id)
	if err != nil {
		return err
	}
	if recovery.State == "done" || recovery.Request.Action.Kind != "create" || recovery.Result.Session == "" {
		return fmt.Errorf("resolution needs an uncertain create action with a known session")
	}
	manager, err := t.LoadManager(ctx)
	if err != nil {
		return fmt.Errorf("load resolution manager: %w", err)
	}
	if !t.writableManager(manager) {
		return fmt.Errorf("recovery manager is not writable private team")
	}
	if decision == "adopt" {
		if err := t.prepareAdoption(ctx, manager, &recovery); err != nil {
			return err
		}
	}
	if err := resolvePoolSession(ctx, manager, &recovery, decision); err != nil {
		return err
	}
	if decision == "adopt" {
		if err := t.commitBoard(ctx, recovery.Request, &recovery.Result); err != nil {
			return err
		}
	}
	effect := lifecycleEffect{State: "done", Result: recovery.Result, Request: recovery.Request}
	if err := t.Store.write(ctx, effectName(id), &effect); err != nil {
		return err
	}
	entry := ReconcileEntry{Action: recovery.Request.Action, State: "done", Result: recovery.Result}
	return t.Store.Append(ctx, &entry)
}

func resolvePoolSession(ctx context.Context, manager *Manager, recovery *PoolRecovery, decision string) error {
	for i := range manager.Team.Members {
		member := &manager.Team.Members[i]
		if member.Session == recovery.Result.Session {
			if recoveredBinding(member, recovery, decision) {
				return nil
			}
			return fmt.Errorf("session is already bound; deletion refused")
		}
		if member.Name == recovery.Result.Worker {
			return fmt.Errorf("worker name is already bound to a different session")
		}
	}
	client := api.New(manager.Team.Server, false)
	member := recovery.Request.Member
	sessions, err := client.Sessions(ctx, member.Dir)
	if err != nil {
		return fmt.Errorf("verify recovery session: %w", err)
	}
	found := false
	for _, session := range sessions {
		if session.ID == recovery.Result.Session && strings.HasSuffix(session.Title,
			" [pool action "+recovery.Request.Action.ID+"]") {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("session does not match recorded pool action")
	}
	switch decision {
	case "delete":
		if err := client.Delete(ctx, member.Dir, recovery.Result.Session); err != nil {
			return fmt.Errorf("delete orphan pool session: %w", err)
		}
		return nil
	case "adopt":
		if err := resetAdoptionPermissions(ctx, manager, recovery); err != nil {
			return err
		}
		member.Session, member.PoolCreated = recovery.Result.Session, true
		manager.Team.LeasePool = true
		manager.Team.Members = append(manager.Team.Members, member)
		return manager.Save(ctx)
	default:
		return fmt.Errorf("recovery decision must be adopt or delete")
	}
}

func (t *LifecycleTransport) prepareAdoption(ctx context.Context, manager *Manager, recovery *PoolRecovery) error {
	pool, err := OpenPool(ctx, manager.Store, manager.Team.Name, t.Store)
	if err != nil {
		return fmt.Errorf("load current adoption policy: %w", err)
	}
	snapshot, err := pool.Snapshot(ctx)
	if err != nil {
		return err
	}
	actions, err := DesiredReconcileActions(&snapshot)
	if err != nil {
		return err
	}
	for i := range actions {
		action := &actions[i]
		if action.ID != recovery.Request.Action.ID || action.Kind != "create" {
			continue
		}
		if err := eligibleReconcile(&snapshot, action); err != nil {
			return err
		}
		rejected, err := pool.rejections(ctx)
		if err != nil {
			return err
		}
		if rejected[action.ID] {
			return fmt.Errorf("adoption action was rejected")
		}
		member, err := reconcileMember(&snapshot, action)
		if err != nil {
			return err
		}
		recovery.Request = &ReconcileRequest{Action: *action, Member: member}
		recovery.Result.Worker = member.Name
		return nil
	}
	return fmt.Errorf("orphan action is no longer desired; adoption refused")
}

func resetAdoptionPermissions(ctx context.Context, manager *Manager, recovery *PoolRecovery) error {
	member := recovery.Request.Member
	manager.Team.LeasePool = true
	manager.Team.Members = append(manager.Team.Members, member)
	index := len(manager.Team.Members) - 1
	defer func() { manager.Team.Members = manager.Team.Members[:index] }()
	if err := manager.Team.CanStart(index); err != nil {
		return err
	}
	if err := manager.Team.Validate(); err != nil {
		return err
	}
	rules, err := manager.creationRules(ctx, &member)
	if err != nil {
		return err
	}
	client := api.New(manager.Team.Server, false)
	if err := client.ApplyPermissions(ctx, member.Dir, recovery.Result.Session, rules); err != nil {
		return fmt.Errorf("reset adopted session permissions: %w", err)
	}
	return nil
}

func recoveredBinding(member *Member, recovery *PoolRecovery, decision string) bool {
	return decision == "adopt" && member.Name == recovery.Result.Worker && member.PoolCreated
}
