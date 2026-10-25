package team

import (
	"context"
	"fmt"
	"slices"
)

func (r *Reconciler) previous(ctx context.Context, action *ReconcileAction) (ReconcileEntry, bool, error) {
	entries, err := r.Journal.Entries(ctx)
	if err != nil {
		return ReconcileEntry{}, false, fmt.Errorf("read reconcile journal: %w", err)
	}
	var found ReconcileEntry
	exists := false
	for i := range entries {
		entry := &entries[i]
		if entry.Action.ID != action.ID {
			continue
		}
		if !sameReconcileAction(&entry.Action, action) || (entry.State != "intent" && entry.State != "done") {
			return ReconcileEntry{}, false, fmt.Errorf("persisted action ID conflicts with requested action")
		}
		found, exists = *entry, true
	}
	return found, exists, nil
}

func (r *Reconciler) resume(ctx context.Context, actor ReconcileActor, action *ReconcileAction,
	entry *ReconcileEntry,
) (ReconcileResult, error) {
	if entry.State == "done" {
		return entry.Result, nil
	}
	result, committed, err := r.Transport.Lookup(ctx, action.ID)
	if err != nil {
		return result, fmt.Errorf("recover reconcile transport result: %w", err)
	}
	if !committed {
		request, err := r.prepare(ctx, actor, action)
		if err != nil {
			return ReconcileResult{}, err
		}
		result, err = r.Transport.Execute(ctx, &request)
		if err != nil {
			return result, fmt.Errorf("execute reconcile transport: %w", err)
		}
	}
	entry.State, entry.Result = "done", result
	if err := r.Journal.Append(context.WithoutCancel(ctx), entry); err != nil {
		return result, fmt.Errorf("persist completed reconcile action: %w", err)
	}
	return result, nil
}

func (r *Reconciler) prepare(ctx context.Context, actor ReconcileActor,
	action *ReconcileAction,
) (ReconcileRequest, error) {
	snapshot, err := r.Snapshot(ctx)
	if err != nil {
		return ReconcileRequest{}, fmt.Errorf("refresh reconcile snapshot: %w", err)
	}
	if err := validateReconcileRequest(&snapshot, actor, action); err != nil {
		return ReconcileRequest{}, err
	}
	request := ReconcileRequest{Action: *action}
	request.Actor, err = reconcileTaskActor(&snapshot.Team, actor)
	if err != nil {
		return request, err
	}
	if boundLifecycle(action.Kind) {
		for i := range snapshot.Team.Members {
			member := &snapshot.Team.Members[i]
			if member.Name == action.Worker && member.Role == "worker" && member.Session != "" && member.PoolCreated {
				request.Member = *member
				return request, nil
			}
		}
		return request, fmt.Errorf("retirement requires a current bound worker")
	}
	member, err := reconcileMember(&snapshot, action)
	if err != nil {
		return request, err
	}
	members := slices.Clone(snapshot.Team.Members)
	for i := range members {
		if members[i].Name == member.Name {
			members[i] = member
		}
	}
	if action.Kind == "create" {
		members = append(members, member)
	}
	rules, err := LeaseRules(snapshot.Team.Repo, members, &member)
	if err != nil {
		return request, fmt.Errorf("build reconcile lease policy: %w", err)
	}
	request.Member, request.Rules = member, rules
	return request, nil
}

func validateReconcileRequest(snapshot *ReconcileSnapshot, actor ReconcileActor, action *ReconcileAction) error {
	if err := authorizeReconcile(snapshot, actor); err != nil {
		return err
	}
	return eligibleReconcile(snapshot, action)
}

func reconcileTaskActor(t *Team, actor ReconcileActor) (TaskActor, error) {
	if actor.User {
		return TaskActor{User: true}, nil
	}
	if actor.Automation {
		return AutomationActor, nil
	}
	return t.ResolveTaskActor(actor.Member)
}

func reconcileMember(snapshot *ReconcileSnapshot, action *ReconcileAction) (Member, error) {
	if action.Kind == "create" {
		return Member{
			Name: "pool-" + action.ID[:16], Role: "worker", Mode: "shared", Dir: snapshot.Team.Repo,
			Agent: "build", Model: action.Proposal.Model, Capability: "coder", Permissions: Preset("coder"),
			Leases:         slices.Clone(action.Proposal.Paths),
			SchedulingRole: action.Proposal.Role, Skills: slices.Clone(action.Proposal.Skills),
		}, nil
	}
	for i := range snapshot.Team.Members {
		member := snapshot.Team.Members[i]
		if member.Name != action.Worker || member.Role != "worker" || member.Session == "" {
			continue
		}
		for j := range snapshot.Input.Board.Tasks {
			task := &snapshot.Input.Board.Tasks[j]
			if task.ID == action.Task {
				member.Leases = slices.Clone(task.Paths)
				return member, nil
			}
		}
	}
	return Member{}, fmt.Errorf("assignment needs current worker binding and task")
}
