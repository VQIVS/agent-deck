package team

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"time"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

// LifecycleTransport adapts Manager lifecycle calls under the BoardStore exclusive lock.
// LoadManager must load current private bindings, not a repository policy overlay.
type LifecycleTransport struct {
	Store       BoardStore
	LoadManager func(context.Context) (*Manager, error)
	Gate        func(context.Context, *ReconcileAction) error
	WorkerSetup func(context.Context, *Manager, int) error
}

type lifecycleEffect struct {
	State   string            `json:"state"`
	Result  ReconcileResult   `json:"result"`
	Request *ReconcileRequest `json:"request,omitempty"`
	Handoff string            `json:"handoff,omitempty"`
	Binding *Member           `json:"binding,omitempty"`
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
		if effect.Request != nil && selfHealKind(effect.Request.Action.Kind) {
			return t.resumeHealing(ctx, &effect)
		}
		if effect.State == "started" && effect.Request != nil && t.WorkerSetup != nil &&
			effect.Request.Action.Kind == "create" {
			return t.resumeIdentityCreate(ctx, &effect)
		}
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
	started := lifecycleEffect{State: "started", Request: request}
	if err := t.Store.write(ctx, effectName(request.Action.ID), &started); err != nil {
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
	state, err := t.Store.LoadMigratedUnlocked(ctx)
	if err != nil {
		return err
	}
	manager, err := t.LoadManager(ctx)
	if err != nil {
		return err
	}
	if err := state.Board.bindWorkflow(manager.Team.Workflow); err != nil {
		return err
	}
	if selfHealKind(request.Action.Kind) {
		if err := releaseHealingTasks(&state.Board, request); err != nil {
			return err
		}
	}
	taskID := request.Action.Task
	if request.Action.Proposal != nil {
		taskID = request.Action.Proposal.Task
	}
	if request.Action.Kind != "retire" && taskID != "" {
		for i := range state.Board.Tasks {
			task := &state.Board.Tasks[i]
			if task.ID == taskID {
				if err := recordTaskAssignmentAs(&state.Board, taskID, result.Worker, request.Actor); err != nil {
					return err
				}
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

func recordTaskAssignment(board *TaskBoard, id, worker string) error {
	return recordTaskAssignmentAs(board, id, worker, TaskActor{User: true})
}

func recordTaskAssignmentAs(board *TaskBoard, id, worker string, actor TaskActor) error {
	if board.Version < 2 {
		for i := range board.Tasks {
			if board.Tasks[i].ID == id {
				board.Tasks[i].State, board.Tasks[i].Worker = "assigned", worker
				return nil
			}
		}
		return fmt.Errorf("unknown assigned task %q", id)
	}
	mutation := TaskMutation{Actor: actor, Now: time.Now().UTC()}
	if err := board.SetAssignee(id, worker, mutation); err != nil {
		return err
	}
	return board.MoveTask(id, board.Destination("active"), mutation)
}

func (t *LifecycleTransport) execute(ctx context.Context, manager *Manager,
	request *ReconcileRequest,
) (ReconcileResult, error) {
	switch request.Action.Kind {
	case "compact", "replace", "recover":
		return t.heal(ctx, manager, &lifecycleEffect{State: "started", Request: request})
	case "create":
		return t.create(ctx, manager, request)
	case "assign":
		index, err := manager.MemberIndex(request.Action.Worker)
		if err != nil {
			return ReconcileResult{}, err
		}
		member := &manager.Team.Members[index]
		if member.Session != request.Member.Session || member.Role != "worker" {
			return ReconcileResult{}, fmt.Errorf("assignment binding changed")
		}
		leases, err := assignmentLeasePaths(ctx, manager, index, request.Member.Leases)
		if err != nil {
			return ReconcileResult{}, err
		}
		member.Leases = leases
		// Only pool-created workers get their identity from the pool; user-configured workers set it
		// up explicitly and already have one.
		if t.WorkerSetup != nil && member.PoolCreated && member.ControlDir != "" {
			if err := t.WorkerSetup(ctx, manager, index); err != nil {
				return ReconcileResult{}, fmt.Errorf("prepare worker identity: %w", err)
			}
			member = &manager.Team.Members[index]
		}
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
	if member.Role != "worker" || member.Session != request.Member.Session || member.Owned ||
		!member.PoolCreated || member.Mode != "shared" {
		return ReconcileResult{}, fmt.Errorf("retirement requires unchanged shared worker binding")
	}
	if status := manager.Observe(ctx, index); status.State != "idle" {
		return ReconcileResult{}, fmt.Errorf("retirement requires idle worker")
	}
	result := ReconcileResult{Worker: member.Name, Session: member.Session}
	client := api.New(manager.Team.Server, false)
	if err := client.Abort(ctx, member.SessionDir(), member.Session); err != nil {
		return result, fmt.Errorf("abort idle pool worker: %w", err)
	}
	manager.Team.Members = append(manager.Team.Members[:index], manager.Team.Members[index+1:]...)
	return result, manager.Save(ctx)
}

func (t *LifecycleTransport) create(ctx context.Context, manager *Manager,
	request *ReconcileRequest,
) (ReconcileResult, error) {
	if result, exists, err := t.resumeWorkerSetup(ctx, manager, request); exists || err != nil {
		return result, err
	}
	return t.createFreshWorker(ctx, manager, request)
}

func (t *LifecycleTransport) resumeWorkerSetup(ctx context.Context, manager *Manager,
	request *ReconcileRequest,
) (ReconcileResult, bool, error) {
	for i := range manager.Team.Members {
		member := &manager.Team.Members[i]
		if member.Name != request.Member.Name {
			continue
		}
		if t.WorkerSetup == nil || !member.PoolCreated || member.Mode != "shared" {
			return ReconcileResult{}, true, fmt.Errorf("generated worker already exists")
		}
		if err := t.WorkerSetup(ctx, manager, i); err != nil {
			return ReconcileResult{}, true, fmt.Errorf("resume pool worker setup: %w", err)
		}
		bound := &manager.Team.Members[i]
		if bound.Session == "" {
			return ReconcileResult{}, true, fmt.Errorf("worker setup did not bind a session")
		}
		return ReconcileResult{Worker: bound.Name, Session: bound.Session}, true, nil
	}
	return ReconcileResult{}, false, nil
}

func (t *LifecycleTransport) createFreshWorker(ctx context.Context, manager *Manager,
	request *ReconcileRequest,
) (ReconcileResult, error) {
	member := request.Member
	if member.Mode != "shared" || member.Session != "" || member.Role != "worker" {
		return ReconcileResult{}, fmt.Errorf("pool creation requires a fresh shared worker")
	}
	member.PoolCreated = true
	manager.Team.LeasePool = true
	manager.Team.Members = append(manager.Team.Members, member)
	index := len(manager.Team.Members) - 1
	if err := manager.Team.CanStart(index); err != nil {
		return ReconcileResult{}, err
	}
	if err := manager.Team.Validate(); err != nil {
		return ReconcileResult{}, err
	}
	if _, err := Options(ctx, &manager.Team, &member, member.Brief); err != nil {
		return ReconcileResult{}, err
	}
	if t.WorkerSetup != nil {
		return t.setupFreshWorker(ctx, manager, index)
	}
	rules, err := manager.creationRules(ctx, &member)
	if err != nil {
		return ReconcileResult{}, err
	}
	session, err := api.New(manager.Team.Server, false).CreateMember(ctx, member.Dir,
		member.Name+" [pool action "+request.Action.ID+"]", rules)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("create pool session: %w", err)
	}
	result := ReconcileResult{Worker: member.Name, Session: session.ID}
	effect := lifecycleEffect{State: "session-created", Result: result, Request: request}
	if err := t.Store.write(context.WithoutCancel(ctx), effectName(request.Action.ID), &effect); err != nil {
		return result, fmt.Errorf("record created pool session %q before binding: %w", session.ID, err)
	}
	manager.Team.Members[index].Session = session.ID
	manager.Team.Members[index].LeasePolicy = manager.Team.LeaseFingerprint(&manager.Team.Members[index])
	if err := manager.Save(ctx); err != nil {
		return result, err
	}
	if err := recordFreshTaskLease(ctx, manager, index); err != nil {
		return result, err
	}
	if err := manager.Send(ctx, index, member.Brief); err != nil {
		return result, err
	}
	return result, nil
}

func (t *LifecycleTransport) setupFreshWorker(ctx context.Context, manager *Manager, index int,
) (ReconcileResult, error) {
	if err := manager.Save(ctx); err != nil {
		return ReconcileResult{}, err
	}
	if err := recordFreshTaskLease(ctx, manager, index); err != nil {
		return ReconcileResult{}, err
	}
	if err := t.WorkerSetup(ctx, manager, index); err != nil {
		return ReconcileResult{}, fmt.Errorf("set up pool worker: %w", err)
	}
	bound := &manager.Team.Members[index]
	if bound.Session == "" {
		return ReconcileResult{}, fmt.Errorf("worker setup did not bind a session")
	}
	return ReconcileResult{Worker: bound.Name, Session: bound.Session}, nil
}
