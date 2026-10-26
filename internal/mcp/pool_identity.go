package mcp

import (
	"context"
	"fmt"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

func (r *Runtime) bindPoolIdentity() {
	if r.options.Scheduler == nil {
		return
	}
	if actions, ok := r.options.Scheduler.Actions.(*poolSchedulerActions); ok {
		actions.mu.Lock()
		defer actions.mu.Unlock()
		actions.setup = r.SetupPoolWorker
		if actions.pool != nil {
			actions.pool.SetWorkerSetup(actions.setup)
		}
	}
}

// SetupPoolWorker provisions only newly generated pool members. Existing user sessions
// require explicit SetupWorkflow; a saved control binding is reused on every retry.
// The lifecycle caller serializes the team mutation and authenticates its policy gate.
func (r *Runtime) SetupPoolWorker(ctx context.Context, manager *team.Manager, index int) error {
	if err := poolWorkflowAllowed(ctx, manager, index); err != nil {
		return err
	}
	return r.setupPoolWorker(ctx, manager, index)
}

func poolWorkflowAllowed(ctx context.Context, manager *team.Manager, index int) error {
	if manager == nil || index < 0 || index >= len(manager.Team.Members) {
		return fmt.Errorf("pool workflow requires a current member")
	}
	if manager.ReadOnly || manager.Store.ReadOnly {
		return fmt.Errorf("pool workflow setup is disabled")
	}
	member := &manager.Team.Members[index]
	if err := poolMemberAllowed(member); err != nil {
		return err
	}
	if _, err := team.Options(ctx, &manager.Team, member, ""); err != nil {
		return fmt.Errorf("preflight pool workflow options: %w", err)
	}
	return nil
}

func poolMemberAllowed(member *team.Member) error {
	if member.Muted {
		return fmt.Errorf("muted pool worker cannot receive workflow identity")
	}
	if !member.PoolCreated || member.Role != "worker" || member.Mode != "shared" {
		return fmt.Errorf("automatic workflow setup requires a pool-created shared worker")
	}
	if member.Capability == "inherited" || len(member.Permissions) == 0 {
		return fmt.Errorf("pool workflow setup requires explicit permissions")
	}
	if member.ID == "" || member.ControlDir == "" && member.Session != "" {
		return fmt.Errorf("automatic workflow setup requires a fresh stable pool binding")
	}
	return nil
}

func (r *Runtime) setupPoolWorker(ctx context.Context, manager *team.Manager, index int) error {
	member := &manager.Team.Members[index]
	r.credentialMu.Lock()
	defer r.credentialMu.Unlock()
	if err := r.revokePoolBinding(member); err != nil {
		return err
	}
	if member.Session == "" {
		if err := r.createPoolWorkflow(ctx, manager, index); err != nil {
			return err
		}
		member = &manager.Team.Members[index]
	} else if err := r.credentialDirectory(ctx, manager, member); err != nil {
		return err
	}
	_, credential, exists := r.findCredential(member.ID, member.Name)
	if !exists {
		if err := r.installCredential(ctx, manager, member.Name); err != nil {
			return err
		}
		_, credential, _ = r.findCredential(member.ID, member.Name)
	}
	if err := r.registerCredential(ctx, manager, &credential); err != nil {
		return fmt.Errorf("pool workflow binding saved; retry tool registration: %w", err)
	}
	r.mu.Lock()
	r.manager = manager
	r.mu.Unlock()
	return nil
}

func (r *Runtime) revokePoolBinding(member *team.Member) error {
	key, credential, exists := r.findCredential(member.ID, member.Name)
	if !exists || credential.dir == member.ControlDir && credential.session == member.Session {
		return nil
	}
	r.Tokens.Revoke(credential.identity)
	delete(r.credentials, key)
	if err := removeRuntimeConfig(&credential); err != nil {
		return fmt.Errorf("remove previous pool credential: %w", err)
	}
	return nil
}

func (r *Runtime) createPoolWorkflow(ctx context.Context, manager *team.Manager, index int) error {
	member := &manager.Team.Members[index]
	if member.ControlDir == "" {
		identity, err := PrepareWorkflowIdentity(ctx, r.workflowRoot(manager.Team.Name), member.ID, member.Dir)
		if err != nil {
			return err
		}
		member.ControlDir = identity.ControlDir
		if err := manager.Save(ctx); err != nil {
			return fmt.Errorf("save prepared pool workflow: %w", err)
		}
		member = &manager.Team.Members[index]
	}
	if err := r.validateControl(member.ID, member.ControlDir); err != nil {
		return err
	}
	identity := WorkflowIdentity{
		MemberID: member.ID, ControlDir: member.ControlDir, Repo: member.Dir,
		PreviousSession: member.PreviousSession, PreviousDir: member.PreviousDir,
	}
	if recovered, err := recoverPoolSession(ctx, manager, index); err != nil || recovered {
		return err
	}
	candidate := *member
	candidate.ControlDir = identity.ControlDir
	rules, err := manager.CreationRules(ctx, &candidate)
	if err != nil {
		return fmt.Errorf("build pool workflow creation rules: %w", err)
	}
	return r.createWorkflowBinding(ctx, manager, index, &identity, rules)
}

// The prepared directory is persisted before creation, allowing a restart to discover
// a session created just before a lost binding save instead of creating a duplicate.
func recoverPoolSession(ctx context.Context, manager *team.Manager, index int) (bool, error) {
	member := &manager.Team.Members[index]
	sessions, err := api.New(manager.Team.Server, false).Sessions(ctx, member.ControlDir)
	if err != nil {
		return false, fmt.Errorf("inspect partial pool workflow session: %w", err)
	}
	var session string
	for _, candidate := range sessions {
		if candidate.Directory != member.ControlDir || candidate.Title != member.Name || candidate.ParentID != "" {
			continue
		}
		if session != "" {
			return false, fmt.Errorf("multiple partial pool workflow sessions; inspect binding")
		}
		session = candidate.ID
	}
	if session == "" {
		return false, nil
	}
	member.Session = session
	member.LeasePolicy = manager.Team.PolicyFingerprint(member)
	if err := manager.Save(ctx); err != nil {
		return false, fmt.Errorf("save recovered pool session: %w", err)
	}
	return true, nil
}
