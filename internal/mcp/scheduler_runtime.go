package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/qivs/agent-deck/internal/team"
)

// PoolProvider reloads the current team's private store bindings without mutating sessions.
type PoolProvider func(context.Context) (*team.Manager, team.BoardStore, error)

type poolSchedulerActions struct {
	provider PoolProvider
	mu       sync.Mutex
	pool     *team.PoolRuntime
	key      string
}

// PoolProposalResult returns approval-pending actions separately from executed auto actions.
type PoolProposalResult struct {
	Pending []team.ReconcileAction `json:"pending"`
	Applied []team.ReconcileResult `json:"applied"`
}

func NewPoolScheduler(provider PoolProvider) (*SchedulerTools, error) {
	if provider == nil {
		return nil, fmt.Errorf("pool scheduler requires a store provider")
	}
	adapter := &poolSchedulerActions{provider: provider}
	return &SchedulerTools{Snapshot: adapter.snapshot, Actions: adapter}, nil
}

func (a *poolSchedulerActions) open(ctx context.Context) (*team.PoolRuntime, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	manager, board, err := a.provider(ctx)
	if err != nil {
		return nil, fmt.Errorf("load scheduler stores: %w", err)
	}
	if manager == nil {
		return nil, fmt.Errorf("pool scheduler requires a current manager")
	}
	store := manager.Store
	store.ReadOnly = store.ReadOnly || manager.ReadOnly
	board.ReadOnly = board.ReadOnly || manager.ReadOnly
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%t\x00%t",
		store.Root, manager.Team.Name, board.Root, store.ReadOnly, board.ReadOnly)
	if a.pool != nil && a.key == key {
		return a.pool, nil
	}
	pool, err := team.OpenPool(ctx, store, manager.Team.Name, board)
	if err != nil {
		return nil, fmt.Errorf("open scheduler pool: %w", err)
	}
	a.pool, a.key = pool, key
	return pool, nil
}

func schedulerSnapshot(snapshot *team.ReconcileSnapshot) *SchedulerSnapshot {
	members := make([]Member, 0, len(snapshot.Team.Members))
	for i := range snapshot.Team.Members {
		member := &snapshot.Team.Members[i]
		members = append(members, Member{ID: member.ID, Name: member.Name, Role: member.Role, Muted: member.Muted})
	}
	return &SchedulerSnapshot{
		Team: snapshot.Team.Name, Revision: snapshot.Revision, Members: members, Input: &snapshot.Input,
	}
}

func (a *poolSchedulerActions) snapshot(ctx context.Context) (*SchedulerSnapshot, error) {
	pool, err := a.open(ctx)
	if err != nil {
		return nil, err
	}
	snapshot, err := pool.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("read pool scheduler snapshot: %w", err)
	}
	return schedulerSnapshot(&snapshot), nil
}

func (a *poolSchedulerActions) ApplySchedulerAction(ctx context.Context, action *SchedulerAction) (any, error) {
	if action == nil {
		return nil, fmt.Errorf("scheduler action is required")
	}
	pool, err := a.open(ctx)
	if err != nil {
		return nil, err
	}
	snapshot, err := pool.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("recheck scheduler snapshot: %w", err)
	}
	if snapshot.Revision != action.Revision || snapshot.Team.Name != action.Team {
		return nil, fmt.Errorf("scheduler revision changed; request a fresh plan")
	}
	identity := Identity{Team: action.Team, Member: action.Master}
	if err := authorizeScheduler(schedulerSnapshot(&snapshot), identity); err != nil {
		return nil, err
	}
	actor := team.ReconcileActor{Member: action.Master}
	controller := pool.Controller(actor)
	return applyPoolAction(ctx, controller, actor, action, snapshot.Input.Policy.Approval)
}

func applyPoolAction(
	ctx context.Context, controller *team.BoardController, actor team.ReconcileActor,
	action *SchedulerAction, approval string,
) (any, error) {
	switch action.Kind {
	case "assign_task":
		result, err := controller.Assign(ctx, actor, action.Task, action.Worker)
		if err != nil {
			return nil, fmt.Errorf("assign pool task: %w", err)
		}
		return result, nil
	case "retire_worker":
		result, err := controller.Retire(ctx, actor, action.Worker)
		if err != nil {
			return nil, fmt.Errorf("retire pool worker: %w", err)
		}
		return result, nil
	case "propose_workers":
		return proposePoolWorkers(ctx, controller, actor, approval)
	default:
		return nil, fmt.Errorf("unknown pool scheduler action")
	}
}

func proposePoolWorkers(
	ctx context.Context, controller *team.BoardController, actor team.ReconcileActor, approval string,
) (PoolProposalResult, error) {
	result := PoolProposalResult{}
	actions, err := controller.Propose(ctx, actor)
	if err != nil {
		return result, fmt.Errorf("propose pool workers: %w", err)
	}
	for i := range actions {
		action := &actions[i]
		if action.Kind != "create" {
			continue
		}
		if approval != "auto" {
			result.Pending = append(result.Pending, *action)
			continue
		}
		applied, err := controller.Apply(ctx, actor, action.ID)
		if err != nil {
			return result, fmt.Errorf("apply automatic worker proposal: %w", err)
		}
		result.Applied = append(result.Applied, applied)
	}
	return result, nil
}
