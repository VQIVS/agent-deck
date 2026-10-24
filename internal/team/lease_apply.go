package team

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func HasLeases(members []Member) bool {
	for i := range members {
		if len(members[i].Leases) > 0 {
			return true
		}
	}
	return false
}

func leaseInstructions(t *Team) string {
	if !t.LeasePool && !HasLeases(t.Members) {
		return ""
	}
	return "\n\n" + LeasePrompt(t.Members)
}

// ApplyLeases appends a complete reset policy while preserving the session and its context.
func (m *Manager) ApplyLeases(ctx context.Context, index int) error {
	if m.Team.ProjectOverlay {
		policy := m.Team.ProjectPolicy
		m.Team = m.Team.PrivateCopy()
		defer m.refreshProject(policy)
	}
	if m.ReadOnly {
		return fmt.Errorf("read-only: applying leases disabled")
	}
	if index < 0 || index >= len(m.Team.Members) {
		return fmt.Errorf("unknown member")
	}
	member := &m.Team.Members[index]
	if member.Session == "" || member.Capability == "inherited" {
		return fmt.Errorf("apply leases needs an existing session and an explicit creation permission preset")
	}
	if err := m.Team.Validate(); err != nil {
		return err
	}
	status := m.Observe(ctx, index)
	if status.State != "idle" {
		return fmt.Errorf("apply leases requires idle member %q, currently %s", member.Name, status.State)
	}
	m.Team.LeasePool = true
	fingerprint := m.Team.LeaseFingerprint(member)
	if member.LeasePolicy == fingerprint {
		return nil
	}
	rules, err := m.creationRules(ctx, member)
	if err != nil {
		return err
	}
	client := api.New(m.Team.Server, false)
	if err := client.ApplyPermissions(ctx, member.Dir, member.Session, rules); err != nil {
		return fmt.Errorf("apply lease reset: %w", err)
	}
	previousPolicy := member.LeasePolicy
	member.LeasePolicy = fingerprint
	if err := m.Save(ctx); err != nil {
		member.LeasePolicy = previousPolicy
		return fmt.Errorf("leases applied live but fingerprint save failed: %w", err)
	}
	return nil
}

func (m *Manager) creationRules(ctx context.Context, member *Member) ([]api.Rule, error) {
	if !m.Team.LeasePool && !HasLeases(m.Team.Members) {
		return baseLeaseRules(member), nil
	}
	root, err := git(ctx, member.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("resolve lease worktree root: %w", err)
	}
	want := m.Team.Repo
	if member.Mode == "worktree" {
		want = member.Dir
	}
	root, rootErr := filepath.EvalSymlinks(root)
	want, wantErr := filepath.EvalSymlinks(want)
	if err := errors.Join(rootErr, wantErr); err != nil {
		return nil, fmt.Errorf("resolve lease paths: %w", err)
	}
	if root != want {
		return nil, fmt.Errorf("lease repository must be Git worktree root %q, got %q", root, want)
	}
	return LeaseRules(root, m.Team.Members, member)
}

func (t *Team) LeaseFingerprint(member *Member) string {
	return t.PolicyFingerprint(member)
}

func (t *Team) LeaseStatus(member *Member) string {
	if !t.LeasePool && !HasLeases(t.Members) {
		return ""
	}
	if member.LeasePolicy == "" || member.LeasePolicy != t.LeaseFingerprint(member) {
		return "leases PENDING apply"
	}
	return "leases applied"
}
