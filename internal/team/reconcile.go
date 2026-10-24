package team

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

// ReconcileActor comes from authenticated integration, never caller-supplied role claims.
type ReconcileActor struct {
	Member string
	User   bool
}

// ReconcileSnapshot is an owned current policy snapshot; revision changes with desired state.
// Approved action IDs must originate from the user's confirmed approval path.
type ReconcileSnapshot struct {
	Team     Team
	Input    SchedulingInput
	Revision string
	Approved map[string]bool
}

// ReconcileAction binds an immutable desired operation to its approval and idempotency ID.
type ReconcileAction struct {
	ID         string          `json:"id"`
	Team       string          `json:"team"`
	Revision   string          `json:"revision"`
	Kind       string          `json:"kind"`
	Worker     string          `json:"worker,omitempty"`
	Task       string          `json:"task,omitempty"`
	Proposal   *WorkerProposal `json:"proposal,omitempty"`
	Generation int             `json:"generation"`
}

// ReconcileRequest carries native lease permissions for session creation or assignment.
type ReconcileRequest struct {
	Action ReconcileAction
	Member Member
	Rules  []api.Rule
}

// ReconcileResult identifies completed transport effects without claiming task completion.
type ReconcileResult struct {
	Worker  string `json:"worker,omitempty"`
	Session string `json:"session,omitempty"`
}

// ReconcileEntry is a durable intent or completed action, retained across restarts.
type ReconcileEntry struct {
	Action ReconcileAction `json:"action"`
	State  string          `json:"state"`
	Result ReconcileResult `json:"result"`
}

// ReconcileJournal must serialize all reconcilers across processes for the callback lifetime.
// Append must fsync before success; Entries must return durable state in append order.
type ReconcileJournal interface {
	Exclusive(context.Context, func() error) error
	Entries(context.Context) ([]ReconcileEntry, error)
	Append(context.Context, *ReconcileEntry) error
}

// ReconcileTransport owns all session, lease, binding and board mutations.
// Execute must be idempotent by action ID and persist its result before success.
// Lookup must recover committed effects after a crash, including partially committed operations.
type ReconcileTransport interface {
	Lookup(context.Context, string) (ReconcileResult, bool, error)
	Execute(context.Context, *ReconcileRequest) (ReconcileResult, error)
}

// Reconciler applies only currently schedulable, authenticated and policy-approved actions.
type Reconciler struct {
	Journal   ReconcileJournal
	Transport ReconcileTransport
	Snapshot  func(context.Context) (ReconcileSnapshot, error)
	mu        sync.Mutex
}

func DesiredReconcileActions(snapshot *ReconcileSnapshot) ([]ReconcileAction, error) {
	if snapshot.Revision == "" || snapshot.Team.Name == "" {
		return nil, fmt.Errorf("reconcile requires team and stable desired-state revision")
	}
	if !filepath.IsAbs(snapshot.Team.Repo) {
		return nil, fmt.Errorf("reconcile requires absolute team repository root")
	}
	plan, err := PlanSchedule(&snapshot.Input)
	if err != nil {
		return nil, err
	}
	actions := make([]ReconcileAction, 0, len(plan.Proposals)+len(plan.Assignments)+len(plan.Retire))
	for i := range plan.Proposals {
		proposal := plan.Proposals[i]
		actions = append(actions, ReconcileAction{Kind: "create", Proposal: &proposal, Generation: i})
	}
	for _, assignment := range plan.Assignments {
		actions = append(actions, ReconcileAction{Kind: "assign", Worker: assignment.Worker, Task: assignment.Task})
	}
	for _, worker := range plan.Retire {
		actions = append(actions, ReconcileAction{Kind: "retire", Worker: worker})
	}
	for i := range actions {
		actions[i].Team, actions[i].Revision = snapshot.Team.Name, snapshot.Revision
		data, _ := json.Marshal(actions[i])
		actions[i].ID = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return actions, nil
}

func (r *Reconciler) Apply(ctx context.Context, actor ReconcileActor,
	action *ReconcileAction,
) (ReconcileResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ReconcileResult{}, err
	}
	if r.Journal == nil || r.Transport == nil || r.Snapshot == nil || action == nil || action.ID == "" {
		return ReconcileResult{}, fmt.Errorf("reconcile requires journal, transport, snapshot and action ID")
	}
	var result ReconcileResult
	err := r.Journal.Exclusive(ctx, func() error {
		var err error
		result, err = r.apply(ctx, actor, action)
		return err
	})
	if err != nil {
		return result, fmt.Errorf("reconcile action %q: %w", action.ID, err)
	}
	return result, nil
}

func (r *Reconciler) apply(ctx context.Context, actor ReconcileActor,
	action *ReconcileAction,
) (ReconcileResult, error) {
	snapshot, err := r.Snapshot(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("read reconcile snapshot: %w", err)
	}
	if err := authorizeReconcile(&snapshot, actor); err != nil {
		return ReconcileResult{}, err
	}
	entry, exists, err := r.previous(ctx, action)
	if err != nil {
		return ReconcileResult{}, err
	}
	if exists {
		return r.resume(ctx, actor, action, &entry)
	}
	if err := eligibleReconcile(&snapshot, action); err != nil {
		return ReconcileResult{}, err
	}
	entry = ReconcileEntry{Action: *action, State: "intent"}
	if err := r.Journal.Append(ctx, &entry); err != nil {
		return ReconcileResult{}, fmt.Errorf("persist reconcile intent: %w", err)
	}
	return r.resume(ctx, actor, action, &entry)
}

func authorizeReconcile(snapshot *ReconcileSnapshot, actor ReconcileActor) error {
	if snapshot.Input.ReadOnly {
		return fmt.Errorf("read-only: reconcile disabled")
	}
	if actor.User {
		return nil
	}
	for i := range snapshot.Team.Members {
		member := &snapshot.Team.Members[i]
		if member.Name == actor.Member && member.Role == "master" && member.Session != "" {
			return nil
		}
	}
	return fmt.Errorf("reconcile requires authenticated current master or privileged user")
}

func eligibleReconcile(snapshot *ReconcileSnapshot, action *ReconcileAction) error {
	actions, err := DesiredReconcileActions(snapshot)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(actions, func(current ReconcileAction) bool { return sameReconcileAction(&current, action) }) {
		return fmt.Errorf("action is no longer desired by current scheduler")
	}
	if action.Kind == "create" && snapshot.Input.Policy.Approval == "off" {
		return fmt.Errorf("reconcile policy is off")
	}
	if action.Kind == "create" && snapshot.Input.Policy.Approval != "auto" && !snapshot.Approved[action.ID] {
		return fmt.Errorf("action requires explicit user approval")
	}
	return nil
}

func sameReconcileAction(left, right *ReconcileAction) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}
