package team

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

type compactedWorker struct {
	Tokens int64 `json:"tokens"`
}

func compactedName(session string) string {
	return fmt.Sprintf("compacted-%x.json", sha256.Sum256([]byte(session)))
}

func selfHealKind(kind string) bool {
	return kind == "compact" || kind == "replace" || kind == "recover"
}
func gatedLifecycle(kind string) bool { return kind == "create" || selfHealKind(kind) }
func boundLifecycle(kind string) bool { return kind == "retire" || selfHealKind(kind) }

func stateStalled(threshold *time.Duration, status *MemberStatus, now time.Time) bool {
	return threshold != nil && *threshold > 0 && status.LastActivity > 0 &&
		now.Sub(time.UnixMilli(status.LastActivity)) >= *threshold
}

func selfHealActions(snapshot *ReconcileSnapshot, plan *SchedulingPlan) []ReconcileAction {
	var actions []ReconcileAction
	if snapshot.Input.ReadOnly || snapshot.Input.Policy.Approval == "off" || snapshot.Input.coolingDown() {
		return actions
	}
	if snapshot.Input.Policy.MaxWorkers == 0 || plan.Desired > snapshot.Input.Policy.MaxWorkers {
		return actions
	}
	for i := range snapshot.Input.Workers {
		worker := &snapshot.Input.Workers[i]
		index := slices.IndexFunc(snapshot.Team.Members, func(m Member) bool { return m.Name == worker.Name })
		if index < 0 {
			continue
		}
		member := &snapshot.Team.Members[index]
		if !member.PoolCreated || member.Mode != "shared" || member.Owned || member.Muted {
			continue
		}
		kind := healingKind(worker, &snapshot.Input.Policy, plan)
		if slices.Contains(plan.Retire, worker.Name) {
			continue
		}
		if kind != "" {
			actions = append(actions, ReconcileAction{Kind: kind, Worker: worker.Name})
		}
	}
	return actions
}

func healingKind(worker *SchedulerWorker, policy *SchedulingPolicy, plan *SchedulingPlan) string {
	full := policy.ContextLimit > 0 && worker.ContextTokens >= policy.ContextLimit
	if worker.State == "idle" && full && worker.CompactedTokens != worker.ContextTokens {
		return "compact"
	}
	if worker.State == "offline" {
		return "recover"
	}
	if slices.Contains(plan.Replace, worker.Name) || worker.State == "busy" && full {
		return "replace"
	}
	return ""
}

func releaseHealingTasks(board *TaskBoard, request *ReconcileRequest) error {
	if request.Action.Kind == "compact" {
		return nil
	}
	for i := range board.Tasks {
		task := &board.Tasks[i]
		if !TaskActive(task) || task.Worker != request.Member.Name {
			continue
		}
		if board.Version < 2 {
			task.State, task.Worker = "pending", ""
			continue
		}
		mutation := TaskMutation{Actor: request.Actor, Now: time.Now().UTC()}
		if err := board.SetAssignee(task.ID, "", mutation); err != nil {
			return err
		}
		if task.State == "blocked" {
			if err := board.MoveTask(task.ID, "ready", mutation); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *LifecycleTransport) resumeHealing(ctx context.Context,
	effect *lifecycleEffect,
) (ReconcileResult, bool, error) {
	manager, err := t.LoadManager(ctx)
	if err != nil {
		return ReconcileResult{}, false, fmt.Errorf("load healing manager: %w", err)
	}
	if !t.writableManager(manager) || t.Store.ReadOnly {
		return ReconcileResult{}, false, fmt.Errorf("healing recovery needs writable manager")
	}
	state, err := t.Store.Load(ctx)
	if err != nil {
		return ReconcileResult{}, false, err
	}
	if state.Policy.Approval == "off" || state.Policy.Approval != "auto" && !state.Approved[effect.Request.Action.ID] {
		return ReconcileResult{}, false, fmt.Errorf("healing recovery is not authorized by current policy")
	}
	if effect.State == "compacting" || effect.State == "aborting" || effect.State == "sending" {
		return ReconcileResult{}, false, fmt.Errorf("uncertain healing request; inspect before retrying")
	}
	result, err := t.heal(ctx, manager, effect)
	if err != nil {
		return result, false, err
	}
	if err := t.commitBoard(ctx, effect.Request, &result); err != nil {
		return result, false, err
	}
	effect.State, effect.Result = "done", result
	return result, true, t.Store.write(ctx, effectName(effect.Request.Action.ID), effect)
}

func (t *LifecycleTransport) heal(ctx context.Context, manager *Manager,
	effect *lifecycleEffect,
) (ReconcileResult, error) {
	request := effect.Request
	index, err := manager.MemberIndex(request.Member.Name)
	if err != nil {
		return ReconcileResult{}, err
	}
	member := &manager.Team.Members[index]
	if err := validateHealingBinding(member, effect); err != nil {
		return ReconcileResult{}, err
	}
	if !member.PoolCreated || member.Mode != "shared" || member.Owned || member.Muted {
		return ReconcileResult{}, fmt.Errorf("self healing requires a managed shared pool worker")
	}
	if request.Action.Kind == "compact" {
		return t.compactWorker(ctx, manager, index, effect)
	}
	return t.replaceWorker(ctx, manager, index, effect)
}

func validateHealingBinding(member *Member, effect *lifecycleEffect) error {
	original := &effect.Request.Member
	if member.ID == "" || member.ID != original.ID {
		return fmt.Errorf("healing member identity changed")
	}
	if sameHealingBinding(member, original) || effect.Binding != nil && sameHealingBinding(member, effect.Binding) {
		return nil
	}
	return fmt.Errorf("healing session or control binding changed outside intent")
}

func sameHealingBinding(member, expected *Member) bool {
	return member.ID == expected.ID && member.Session == expected.Session &&
		member.ControlDir == expected.ControlDir && member.Dir == expected.Dir
}

func (t *LifecycleTransport) saveHealing(ctx context.Context, effect *lifecycleEffect, phase string) error {
	effect.State = phase
	return t.Store.write(context.WithoutCancel(ctx), effectName(effect.Request.Action.ID), effect)
}

func (t *LifecycleTransport) compactWorker(ctx context.Context, manager *Manager, index int,
	effect *lifecycleEffect,
) (ReconcileResult, error) {
	member := &manager.Team.Members[index]
	result := ReconcileResult{Worker: member.Name, Session: member.Session}
	if effect.State == "healed" {
		return result, nil
	}
	if member.Session != effect.Request.Member.Session || manager.Observe(ctx, index).State != "idle" {
		return result, fmt.Errorf("compaction requires unchanged idle binding")
	}
	options, err := Options(ctx, &manager.Team, member, "")
	if err != nil || options.Model == nil {
		return result, fmt.Errorf("compaction requires configured session model")
	}
	if err := t.saveHealing(ctx, effect, "compacting"); err != nil {
		return result, err
	}
	client := api.New(manager.Team.Server, false)
	if err := client.Summarize(ctx, member.SessionDir(), member.Session, *options.Model); err != nil {
		return result, fmt.Errorf("compact pool worker: %w", err)
	}
	status := manager.Observe(ctx, index)
	marker := compactedWorker{Tokens: status.ContextTokens}
	if err := t.Store.write(ctx, compactedName(member.Session), &marker); err != nil {
		return result, err
	}
	return result, t.saveHealing(ctx, effect, "healed")
}
