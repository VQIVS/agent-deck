package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ResetWorkflow explicitly restores a retained previous binding without deleting either transcript.
func (r *Runtime) ResetWorkflow(ctx context.Context, name string) error {
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
		return fmt.Errorf("find workflow reset member: %w", err)
	}
	member := &manager.Team.Members[index]
	if manager.ReadOnly || member.ControlDir == "" {
		return fmt.Errorf("reset requires a writable workflow identity")
	}
	if err := r.validateControl(member.ID, member.ControlDir); err != nil {
		return err
	}
	busy, err := runtimeBusy(ctx, manager, name)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("workflow reset requires idle member")
	}
	control, session := member.ControlDir, member.Session
	oldDir, oldSession := member.PreviousDir, member.PreviousSession
	member.ControlDir = ""
	if oldDir != "" && oldDir != member.Dir {
		return fmt.Errorf("previous session directory needs explicit migration")
	}
	member.Session = oldSession
	member.PreviousDir, member.PreviousSession = control, session
	member.LeasePolicy = ""
	if err := manager.Save(ctx); err != nil {
		return fmt.Errorf("save workflow reset: %w", err)
	}
	key, credential, exists := r.findCredential(member.ID, name)
	if exists {
		r.Tokens.Revoke(credential.identity)
		delete(r.credentials, key)
		if err := removeRuntimeConfig(&credential); err != nil {
			return err
		}
		r.removeEmptyWorkflowControl(&credential)
	}
	// Remove only empty control directories; never recursively delete user files or the checkout.
	_ = os.Remove(control)
	_ = os.Remove(filepath.Dir(control))
	r.mu.Lock()
	r.manager = manager
	r.mu.Unlock()
	return nil
}

func (c *RuntimeConnection) ResetWorkflow(ctx context.Context, member string) error {
	if c.owner != nil {
		return c.owner.ResetWorkflow(ctx, member)
	}
	return c.call(ctx, &userRequest{Action: "reset_workflow", Member: member}, nil)
}
