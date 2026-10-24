package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/qivs/agent-deck/internal/team"
)

// SchedulerSnapshot binds policy and master authorization to a revisioned team snapshot.
type SchedulerSnapshot struct {
	Team     string
	Revision string
	Members  []Member
	Input    *team.SchedulingInput
}

// SchedulerAction contains only server-validated data; approval is never caller-controlled.
type SchedulerAction struct {
	Team     string
	Kind     string
	Revision string
	Master   string
	Task     string
	Worker   string
	Plan     *team.SchedulingPlan
}

// SchedulerActions atomically compares Revision, revalidates current authorization/policy,
// persists the action and executes only policy-approved operations. Ask proposals must be
// queued for user approval; this boundary never treats the master's tool call as approval.
type SchedulerActions interface {
	ApplySchedulerAction(context.Context, *SchedulerAction) (any, error)
}

// SchedulerTools adapts authenticated calls to pure planning and an injected policy gate.
type SchedulerTools struct {
	Snapshot func(context.Context) (*SchedulerSnapshot, error)
	Actions  SchedulerActions
	mu       sync.Mutex
}

func (s *SchedulerTools) Definitions() []map[string]any {
	text := map[string]any{"type": "string"}
	return []map[string]any{
		tool("propose_workers", "Propose policy-bounded capacity; ask mode queues user approval", nil, nil),
		tool("assign_task", "Assign a schedulable task to an idle matching worker", map[string]any{
			"task": text, "worker": text,
		}, []string{"task", "worker"}),
		tool("retire_worker", "Retire an eligible idle worker within current policy", map[string]any{
			"worker": text,
		}, []string{"worker"}),
	}
}

func (s *SchedulerTools) Call(
	ctx context.Context, identity Identity, name string, raw json.RawMessage,
) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Snapshot == nil || s.Actions == nil {
		return nil, fmt.Errorf("scheduler tools require snapshot and action transport")
	}
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("read scheduler snapshot: %w", err)
	}
	if err := authorizeScheduler(snapshot, identity); err != nil {
		return nil, err
	}
	plan, err := team.PlanSchedule(snapshot.Input)
	if err != nil {
		return nil, fmt.Errorf("plan scheduler action: %w", err)
	}
	action := SchedulerAction{Team: snapshot.Team, Kind: name, Revision: snapshot.Revision, Master: identity.Member}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := schedulerArguments(name, raw, snapshot, &plan, &action); err != nil {
		return nil, err
	}
	result, err := s.Actions.ApplySchedulerAction(ctx, &action)
	if err != nil {
		return nil, fmt.Errorf("apply scheduler action: %w", err)
	}
	return result, nil
}

func authorizeScheduler(snapshot *SchedulerSnapshot, identity Identity) error {
	if snapshot == nil || snapshot.Input == nil || snapshot.Revision == "" || identity.Team != snapshot.Team {
		return fmt.Errorf("scheduler requires a current revisioned team snapshot")
	}
	if snapshot.Input.ReadOnly {
		return fmt.Errorf("read-only: scheduler actions disabled")
	}
	for _, member := range snapshot.Members {
		if member.Name == identity.Member && member.ID != "" && member.Role == "master" && !member.Muted {
			return nil
		}
	}
	return fmt.Errorf("scheduler tools require a current authenticated master")
}

func schedulerArguments(
	name string, raw json.RawMessage, snapshot *SchedulerSnapshot, plan *team.SchedulingPlan, action *SchedulerAction,
) error {
	switch name {
	case "propose_workers":
		if err := decode(bytes.NewReader(raw), &struct{}{}); err != nil {
			return err
		}
		if snapshot.Input.Policy.Approval == "off" {
			return fmt.Errorf("worker scaling is disabled")
		}
		if len(plan.Proposals) == 0 {
			return fmt.Errorf("no worker capacity is currently proposable")
		}
		action.Plan = plan
	case "assign_task":
		var arguments struct {
			Task   string `json:"task"`
			Worker string `json:"worker"`
		}
		if err := decode(bytes.NewReader(raw), &arguments); err != nil {
			return err
		}
		assignment := team.TaskAssignment{Task: arguments.Task, Worker: arguments.Worker}
		if !slices.Contains(plan.Assignments, assignment) {
			return fmt.Errorf("assignment is not currently schedulable")
		}
		action.Task, action.Worker = arguments.Task, arguments.Worker
	case "retire_worker":
		var arguments struct {
			Worker string `json:"worker"`
		}
		if err := decode(bytes.NewReader(raw), &arguments); err != nil {
			return err
		}
		if !team.RetirementApproved(snapshot.Input, arguments.Worker) {
			return fmt.Errorf("worker retirement is not currently permitted")
		}
		action.Worker = arguments.Worker
	default:
		return fmt.Errorf("unknown scheduler tool")
	}
	return nil
}
