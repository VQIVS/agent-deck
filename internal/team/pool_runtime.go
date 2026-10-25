package team

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// PoolRuntime builds current scheduler snapshots and authenticated lifecycle controllers.
type PoolRuntime struct {
	store       Store
	name        string
	board       BoardStore
	mu          sync.Mutex
	idle        map[string]time.Time
	workerSetup func(context.Context, *Manager, int) error
}

// SetWorkerSetup installs identity setup before a pool session can receive work.
func (p *PoolRuntime) SetWorkerSetup(setup func(context.Context, *Manager, int) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.workerSetup = setup
}

// OpenPool validates local configuration without creating board files or contacting the server.
func OpenPool(ctx context.Context, store Store, name string, board BoardStore) (*PoolRuntime, error) {
	loaded, _, err := store.Load(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("open pool team: %w", err)
	}
	if board.Team == "" {
		board.Team = loaded.Name
	}
	if board.Team != loaded.Name {
		return nil, fmt.Errorf("pool board belongs to a different team")
	}
	board.ReadOnly = board.ReadOnly || store.ReadOnly
	if _, err := board.directory(); err != nil {
		return nil, err
	}
	runtime := &PoolRuntime{store: store, name: loaded.Name, board: board, idle: map[string]time.Time{}}
	if _, err := runtime.Status(ctx); err != nil {
		return nil, err
	}
	return runtime, nil
}

// Status returns an in-memory disabled default when no private board has been configured.
func (p *PoolRuntime) Status(ctx context.Context) (BoardState, error) {
	state, err := p.board.Load(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return BoardState{
			Version: 2, Board: TaskBoard{Version: 2},
			Policy: SchedulingPolicy{Approval: "off"}, Approved: map[string]bool{},
		}, nil
	}
	return state, err
}

// Configure persists private board policy after authenticating the current master or privileged user.
func (p *PoolRuntime) Configure(ctx context.Context, actor ReconcileActor, state *BoardState, expected uint64) error {
	manager, err := p.manager(ctx)
	if err != nil {
		return err
	}
	snapshot := ReconcileSnapshot{Team: manager.Team, Input: SchedulingInput{ReadOnly: p.board.ReadOnly}}
	if err := authorizeReconcile(&snapshot, actor); err != nil {
		return err
	}
	if !actor.User {
		return fmt.Errorf("whole-board configuration requires privileged user; masters use task mutations")
	}
	return p.board.Save(ctx, state, expected)
}

func (p *PoolRuntime) manager(ctx context.Context) (*Manager, error) {
	t, version, err := p.store.Load(ctx, p.name)
	if err != nil {
		return nil, fmt.Errorf("reload pool team: %w", err)
	}
	return &Manager{Store: p.store, Team: t, Version: version, ReadOnly: p.board.ReadOnly}, nil
}

// Snapshot polls only current bound workers using their scoped directory, never mutating live sessions.
func (p *PoolRuntime) Snapshot(ctx context.Context) (ReconcileSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	manager, err := p.manager(ctx)
	if err != nil {
		return ReconcileSnapshot{}, err
	}
	state, err := p.Status(ctx)
	if err != nil {
		return ReconcileSnapshot{}, err
	}
	now := time.Now().UTC()
	if err := state.Board.SetWorkflow(manager.Team.Workflow,
		TaskMutation{Actor: AutomationActor, Now: now}); err != nil {
		return ReconcileSnapshot{}, err
	}
	var workers []SchedulerWorker
	if state.Revision != 0 {
		workers, err = p.workers(ctx, manager, now, &state.Policy)
		if err != nil {
			return ReconcileSnapshot{}, err
		}
	}
	return ReconcileSnapshot{
		Team: manager.Team, Revision: fmt.Sprintf("%d-%x", state.Revision, sha256.Sum256(manager.Version)),
		Approved: state.Approved,
		Input: SchedulingInput{
			Board: &state.Board, Policy: state.Policy, Workers: workers, Now: now,
			LastScale: state.LastScale, ReadOnly: p.board.ReadOnly,
		},
	}, nil
}

func (p *PoolRuntime) workers(ctx context.Context, manager *Manager, now time.Time,
	policy *SchedulingPolicy,
) ([]SchedulerWorker, error) {
	workers := make([]SchedulerWorker, 0, len(manager.Team.Members))
	for i := range manager.Team.Members {
		member := &manager.Team.Members[i]
		if member.Role != "worker" || member.Session == "" {
			continue
		}
		status := manager.Observe(ctx, i)
		if status.Error != "" && !strings.Contains(status.Error, "is missing") {
			return nil, fmt.Errorf("observe pool worker %q: %s", member.Name, status.Error)
		}
		threshold := member.StallAfter
		if threshold == nil {
			threshold = &policy.StallAfter
		}
		if status.State == "busy" && stateStalled(threshold, &status, now) {
			status.State = "stalled"
		}
		key := member.ID + ":" + member.Session
		if status.State != "idle" {
			delete(p.idle, key)
		} else if p.idle[key].IsZero() {
			p.idle[key] = now
		}
		var compacted compactedWorker
		err := p.board.read(ctx, compactedName(member.Session), &compacted)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		role := member.SchedulingRole
		if role == "" {
			role = "worker"
		}
		workers = append(workers, SchedulerWorker{
			Name: member.Name, Role: role, Skills: slices.Clone(member.Skills), Areas: slices.Clone(member.Areas),
			State: status.State, Leases: slices.Clone(member.Leases), IdleSince: p.idle[key],
			ContextTokens:   status.ContextTokens,
			CompactedTokens: compacted.Tokens,
		})
	}
	return p.maskHealingWorkers(ctx, workers)
}

func (p *PoolRuntime) maskHealingWorkers(ctx context.Context, workers []SchedulerWorker) ([]SchedulerWorker, error) {
	entries, err := p.board.Entries(ctx)
	if err != nil {
		return nil, err
	}
	pending := map[string]bool{}
	for i := range entries {
		entry := &entries[i]
		if selfHealKind(entry.Action.Kind) {
			pending[entry.Action.Worker] = false
			if entry.State != "intent" {
				continue
			}
			// A new intent must remain eligible through prepare and the transport's
			// final policy gate. Only an effect that has actually started needs
			// masking while its durable phase is resumed under the board lock.
			var effect lifecycleEffect
			err := p.board.read(ctx, effectName(entry.Action.ID), &effect)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			pending[entry.Action.Worker] = err == nil
		}
	}
	for i := range workers {
		if pending[workers[i].Name] {
			workers[i].State = "waiting"
		}
	}
	return workers, nil
}

// Controller binds the transport gate to an authenticated actor supplied by trusted integration.
func (p *PoolRuntime) Controller(actor ReconcileActor) *BoardController {
	p.mu.Lock()
	setup := p.workerSetup
	p.mu.Unlock()
	transport := &LifecycleTransport{
		Store: p.board, LoadManager: p.manager, WorkerSetup: setup,
		Gate: func(ctx context.Context, action *ReconcileAction) error {
			rejected, err := p.rejections(ctx)
			if err != nil {
				return err
			}
			if rejected[action.ID] {
				return fmt.Errorf("pool action was rejected by user")
			}
			snapshot, err := p.Snapshot(ctx)
			if err != nil {
				return err
			}
			if err := authorizeReconcile(&snapshot, actor); err != nil {
				return err
			}
			return eligibleReconcile(&snapshot, action)
		},
	}
	reconciler := &Reconciler{Journal: p.board, Transport: transport, Snapshot: p.Snapshot}
	return &BoardController{Store: p.board, Reconciler: reconciler, Snapshot: p.Snapshot}
}

func (p *PoolRuntime) Approve(ctx context.Context, actor ReconcileActor, actionID string) error {
	if !actor.User {
		return fmt.Errorf("pool approval requires privileged confirmed user")
	}
	actions, err := p.Actions(ctx, actor)
	if err != nil {
		return err
	}
	for i := range actions {
		if actions[i].ID == actionID {
			return p.board.Approve(ctx, actionID, true)
		}
	}
	return fmt.Errorf("approval action is not currently desired")
}

// Actions returns current proposals excluding durably rejected action IDs.
func (p *PoolRuntime) Actions(ctx context.Context, actor ReconcileActor) ([]ReconcileAction, error) {
	actions, err := p.Controller(actor).Propose(ctx, actor)
	if err != nil {
		return nil, err
	}
	rejected, err := p.rejections(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]ReconcileAction, 0, len(actions))
	for i := range actions {
		if !rejected[actions[i].ID] {
			filtered = append(filtered, actions[i])
		}
	}
	return filtered, nil
}

// Pending returns the filtered current actions for CLI and UI request lists.
func (p *PoolRuntime) Pending(ctx context.Context, actor ReconcileActor) ([]ReconcileAction, error) {
	return p.Actions(ctx, actor)
}

func (p *PoolRuntime) rejections(ctx context.Context) (map[string]bool, error) {
	rejected := map[string]bool{}
	err := p.board.read(ctx, "rejected.json", &rejected)
	if errors.Is(err, os.ErrNotExist) {
		return rejected, nil
	}
	return rejected, err
}

// Reject records a privileged user's decision for this immutable desired-action ID.
func (p *PoolRuntime) Reject(ctx context.Context, actor ReconcileActor, actionID string) error {
	if !actor.User {
		return fmt.Errorf("pool rejection requires privileged confirmed user")
	}
	return p.board.Exclusive(ctx, func() error {
		actions, err := p.Actions(ctx, actor)
		if err != nil {
			return err
		}
		for i := range actions {
			if actions[i].ID == actionID {
				rejected, err := p.rejections(ctx)
				if err != nil {
					return err
				}
				rejected[actionID] = true
				return p.board.write(ctx, "rejected.json", rejected)
			}
		}
		return fmt.Errorf("rejection action is not currently desired")
	})
}
