package team

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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

// ApplyLeases replaces an idle binding rather than appending to OpenCode's existing rules.
func (m *Manager) ApplyLeases(ctx context.Context, index int) error {
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
	if _, err := Options(ctx, &m.Team, member, member.Brief); err != nil {
		return err
	}
	status := m.Observe(ctx, index)
	if status.State != "idle" {
		return fmt.Errorf("apply leases requires idle member %q, currently %s", member.Name, status.State)
	}
	m.Team.LeasePool = true
	rules, err := m.creationRules(ctx, member)
	if err != nil {
		return err
	}
	client := api.New(m.Team.Server, false)
	session, err := client.CreateMember(ctx, member.Dir, member.Name, rules)
	if err != nil {
		return fmt.Errorf("create leased replacement: %w", err)
	}
	previous := member.Session
	previousPolicy := member.LeasePolicy
	member.Session = session.ID
	member.LeasePolicy = m.Team.LeaseFingerprint(member)
	if err := m.Save(ctx); err != nil {
		member.Session = previous
		member.LeasePolicy = previousPolicy
		cleanupErr := client.Delete(ctx, member.Dir, session.ID)
		return fmt.Errorf("save leased replacement: %w", errors.Join(err, cleanupErr))
	}
	text := fmt.Sprintf("Fresh session after explicit lease application. Previous session: %s\nBrief: %s\n"+
		"Read the previous transcript if needed; do not repeat completed work blindly.", previous, member.Brief)
	if err := m.Send(ctx, index, text); err != nil {
		return fmt.Errorf("binding replaced with %q; send continuation using Message (do not reapply): %w", session.ID, err)
	}
	return m.Roster(ctx, "leases applied: "+member.Name)
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
	if !t.LeasePool && !HasLeases(t.Members) {
		return ""
	}
	rules, err := LeaseRules(t.Repo, t.Members, member)
	if err != nil {
		return "invalid"
	}
	b, _ := json.Marshal(rules)
	return fmt.Sprintf("%x", sha256.Sum256(b))
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
