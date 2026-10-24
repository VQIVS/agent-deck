package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (m *Manager) ReloadProject(ctx context.Context) error {
	policy, err := LoadProject(ctx, filepath.Join(m.Team.Repo, "agent-deck.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reload project policy: %w", err)
	}
	m.refreshProject(&policy)
	return nil
}

// PrivateCopy retains local edits and bindings without repository-derived permissions or prompts.
func (t *Team) PrivateCopy() Team {
	private := *t
	private.Members = append([]Member(nil), t.Members...)
	for i := range private.Members {
		member := &private.Members[i]
		if t.ProjectPrivate != nil {
			for j := range t.ProjectPrivate.Members {
				original := &t.ProjectPrivate.Members[j]
				if original.Name == member.Name {
					member.Permissions = original.Permissions
					break
				}
			}
		}
		member.ProjectSystem = ""
		member.ProjectEditUntrusted = false
	}
	private.ProjectOverlay = false
	private.ProjectRules = ""
	private.ProjectPrivate = nil
	private.ProjectPolicy = nil
	return private
}

func (m *Manager) refreshProject(policy *ProjectPolicy) {
	if policy == nil {
		return
	}
	private := m.Team.PrivateCopy()
	merged, err := policy.Merge(ProjectBindings{Local: &private})
	if err == nil {
		m.Team = merged
	}
}
