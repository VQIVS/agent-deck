package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

// SetupWorkflow explicitly replaces an idle member's session with a private control session.
// The previous session is retained unchanged and referenced in the persisted binding.
func (r *Runtime) SetupWorkflow(ctx context.Context, name string) error {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()
	r.credentialMu.Lock()
	defer r.credentialMu.Unlock()
	manager, err := r.current(ctx)
	if err != nil {
		return err
	}
	index, err := manager.MemberIndex(name)
	if err != nil {
		return fmt.Errorf("find workflow member: %w", err)
	}
	member := &manager.Team.Members[index]
	if err := workflowSetupAllowed(manager, member); err != nil {
		return err
	}
	if _, exists := r.credentials[name]; exists {
		return fmt.Errorf("revoke the existing member credential explicitly before workflow setup")
	}
	if _, err := team.Options(ctx, &manager.Team, member, ""); err != nil {
		return fmt.Errorf("preflight workflow prompt options: %w", err)
	}
	if member.Session != "" {
		busy, err := runtimeBusy(ctx, manager, name)
		if err != nil {
			return err
		}
		if busy {
			return fmt.Errorf("workflow setup requires an idle member")
		}
	}
	identity, err := PrepareWorkflowIdentity(ctx, r.workflowRoot(manager.Team.Name), member.ID, member.Dir)
	if err != nil {
		return err
	}
	identity.PreviousSession, identity.PreviousDir = member.Session, member.SessionDir()
	candidate := *member
	candidate.ControlDir = identity.ControlDir
	rules, err := manager.CreationRules(ctx, &candidate)
	if err != nil {
		return fmt.Errorf("build checked workflow creation rules: %w", err)
	}
	handoff, err := team.WorkflowHandoff(ctx, manager, index)
	if err != nil {
		return fmt.Errorf("gather workflow handoff: %w", err)
	}
	if err := r.createWorkflowBinding(ctx, manager, index, &identity, rules); err != nil {
		return err
	}
	if err := manager.Send(ctx, index, handoff+"\n\n"+identity.Instructions()); err != nil {
		return &WorkflowHandoffError{Session: manager.Team.Members[index].Session, Err: err}
	}
	return nil
}

// WorkflowHandoffError means the new binding is saved; retry prompting, never repeat setup.
type WorkflowHandoffError struct {
	Session string
	Err     error
}

func (err *WorkflowHandoffError) Error() string {
	return fmt.Sprintf("workflow binding saved (session %s), initial handoff failed; "+
		"send a message to retry: %v", err.Session, err.Err)
}

func (err *WorkflowHandoffError) Unwrap() error { return err.Err }

func workflowSetupAllowed(manager *team.Manager, member *team.Member) error {
	if manager.ReadOnly || member.Muted {
		return fmt.Errorf("workflow setup is disabled")
	}
	if member.Mode != "shared" && member.Mode != "adopt" {
		return fmt.Errorf("workflow setup is for shared/adopt members; worktrees use credential installation")
	}
	if member.Capability == "inherited" || len(member.Permissions) == 0 {
		return fmt.Errorf("choose an explicit permission preset before workflow setup")
	}
	if member.ControlDir != "" {
		return fmt.Errorf("workflow identity already configured; use credential installation")
	}
	return nil
}

func (r *Runtime) createWorkflowBinding(
	ctx context.Context, manager *team.Manager, index int, identity *WorkflowIdentity, rules []api.Rule,
) error {
	member := &manager.Team.Members[index]
	auth := Identity{Team: manager.Team.Name, Member: member.Name}
	token, err := r.Tokens.Issue(auth, true)
	if err != nil {
		return err
	}
	credential := runtimeCredential{
		memberID: member.ID,
		identity: auth, dir: identity.ControlDir, role: member.Role,
		config: filepath.Join(identity.ControlDir, "opencode.json"),
		entry:  "agent_deck_" + team.Slug(manager.Team.Name), token: token,
	}
	credential.lockPath = runtimeStatePath(r.options.StateDir, credential.config, "config.lock")
	if err := installRuntimeConfig(&credential, r.URL); err != nil {
		r.Tokens.Revoke(auth)
		return err
	}
	client := api.New(manager.Team.Server, false)
	session, err := client.CreateMember(ctx, identity.ControlDir, member.Name, rules)
	if err != nil {
		r.Tokens.Revoke(auth)
		return errors.Join(fmt.Errorf("create workflow session: %w", err), removeRuntimeConfig(&credential))
	}
	member.PreviousSession, member.PreviousDir = identity.PreviousSession, identity.PreviousDir
	member.ControlDir, member.Session = identity.ControlDir, session.ID
	member.LeasePolicy = manager.Team.PolicyFingerprint(member)
	if err := manager.Save(ctx); err != nil {
		r.Tokens.Revoke(auth)
		cleanup := context.WithoutCancel(ctx)
		return errors.Join(err, client.Delete(cleanup, identity.ControlDir, session.ID), removeRuntimeConfig(&credential))
	}
	credential.session = session.ID
	r.credentials[member.Name] = credential
	r.mu.Lock()
	r.manager = manager
	r.mu.Unlock()
	return nil
}
