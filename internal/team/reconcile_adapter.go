package team

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"
)

// LifecycleTransport adapts Manager lifecycle calls under the BoardStore exclusive lock.
// LoadManager must load current private bindings, not a repository policy overlay.
type LifecycleTransport struct {
	Store       BoardStore
	LoadManager func(context.Context) (*Manager, error)
	Gate        func(context.Context, *ReconcileAction) error
}

type lifecycleEffect struct {
	State  string          `json:"state"`
	Result ReconcileResult `json:"result"`
}

func effectName(id string) string { return fmt.Sprintf("effect-%x.json", sha256.Sum256([]byte(id))) }

func (t *LifecycleTransport) Lookup(ctx context.Context, id string) (ReconcileResult, bool, error) {
	var effect lifecycleEffect
	err := t.Store.read(ctx, effectName(id), &effect)
	if errors.Is(err, os.ErrNotExist) {
		return ReconcileResult{}, false, nil
	}
	if err != nil {
		return ReconcileResult{}, false, err
	}
	if effect.State != "done" {
		return ReconcileResult{}, false, fmt.Errorf("uncertain lifecycle effects; inspect bindings before resolution")
	}
	return effect.Result, true, nil
}

func (t *LifecycleTransport) Execute(ctx context.Context, request *ReconcileRequest) (ReconcileResult, error) {
	if request == nil || t.LoadManager == nil || t.Gate == nil || t.Store.ReadOnly {
		return ReconcileResult{}, fmt.Errorf("lifecycle transport requires writable store, request and manager loader")
	}
	if result, done, err := t.Lookup(ctx, request.Action.ID); err != nil || done {
		return result, err
	}
	manager, err := t.LoadManager(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load lifecycle manager: %w", err)
	}
	if !t.writableManager(manager) {
		return ReconcileResult{}, fmt.Errorf("lifecycle requires writable current private team bindings")
	}
	if err := t.Gate(ctx, &request.Action); err != nil {
		return ReconcileResult{}, fmt.Errorf("recheck lifecycle policy gate: %w", err)
	}
	if err := t.Store.write(ctx, effectName(request.Action.ID), &lifecycleEffect{State: "started"}); err != nil {
		return ReconcileResult{}, err
	}
	result, err := t.execute(ctx, manager, request)
	if err != nil {
		return result, fmt.Errorf("lifecycle action uncertain; inspect persisted bindings: %w", err)
	}
	if err := t.commitBoard(ctx, request, &result); err != nil {
		return result, err
	}
	effect := lifecycleEffect{State: "done", Result: result}
	if err := t.Store.write(context.WithoutCancel(ctx), effectName(request.Action.ID), &effect); err != nil {
		return result, err
	}
	return result, nil
}

func (t *LifecycleTransport) writableManager(manager *Manager) bool {
	return !manager.ReadOnly && !manager.Team.ProjectOverlay && manager.Team.Name == t.Store.Team
}

func (t *LifecycleTransport) commitBoard(ctx context.Context, request *ReconcileRequest,
	result *ReconcileResult,
) error {
	state, err := t.Store.Load(ctx)
	if err != nil {
		return err
	}
	taskID := request.Action.Task
	if request.Action.Proposal != nil {
		taskID = request.Action.Proposal.Task
	}
	if request.Action.Kind != "retire" && taskID != "" {
		for i := range state.Board.Tasks {
			task := &state.Board.Tasks[i]
			if task.ID == taskID {
				task.State, task.Worker = "assigned", result.Worker
			}
		}
	}
	state.Revision++
	state.Approved = map[string]bool{}
	if request.Action.Kind != "assign" {
		state.LastScale = time.Now().UTC()
	}
	if err := t.Store.write(ctx, "board.json", &state); err != nil {
		return err
	}
	return nil
}

func (t *LifecycleTransport) execute(ctx context.Context, manager *Manager,
	request *ReconcileRequest,
) (ReconcileResult, error) {
	switch request.Action.Kind {
	case "create":
		if slices.ContainsFunc(manager.Team.Members, func(member Member) bool { return member.Name == request.Member.Name }) {
			return ReconcileResult{}, fmt.Errorf("generated worker already exists")
		}
		manager.Team.LeasePool = true
		manager.Team.Members = append(manager.Team.Members, request.Member)
		index := len(manager.Team.Members) - 1
		if err := manager.Start(ctx, index); err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{Worker: manager.Team.Members[index].Name, Session: manager.Team.Members[index].Session}, nil
	case "assign":
		index, err := manager.MemberIndex(request.Action.Worker)
		if err != nil {
			return ReconcileResult{}, err
		}
		member := &manager.Team.Members[index]
		if member.Session != request.Member.Session || member.Role != "worker" {
			return ReconcileResult{}, fmt.Errorf("assignment binding changed")
		}
		member.Leases = slices.Clone(request.Member.Leases)
		manager.Team.LeasePool = true
		if err := manager.ApplyLeases(ctx, index); err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{Worker: member.Name, Session: member.Session}, nil
	case "retire":
		return retireLifecycle(ctx, manager, request)
	default:
		return ReconcileResult{}, fmt.Errorf("unknown lifecycle action")
	}
}

func retireLifecycle(ctx context.Context, manager *Manager, request *ReconcileRequest) (ReconcileResult, error) {
	index, err := manager.MemberIndex(request.Action.Worker)
	if err != nil {
		return ReconcileResult{}, err
	}
	member := &manager.Team.Members[index]
	if member.Role != "worker" || member.Session != request.Member.Session || member.Owned {
		return ReconcileResult{}, fmt.Errorf("retirement requires unchanged shared worker binding")
	}
	if status := manager.Observe(ctx, index); status.State != "idle" {
		return ReconcileResult{}, fmt.Errorf("retirement requires idle worker")
	}
	result := ReconcileResult{Worker: member.Name, Session: member.Session}
	return result, manager.Remove(ctx, index, false)
}
