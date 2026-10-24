package team

import (
	"context"
	"fmt"
)

// LoadRuntimeTeam migrates only explicitly runtime-enabled teams using revision-checked saves.
func (s Store) LoadRuntimeTeam(ctx context.Context, name string) (Team, []byte, error) {
	t, revision, err := s.Load(ctx, name)
	if err != nil {
		return t, revision, err
	}
	if !t.LeasePool || s.ReadOnly {
		return t, revision, nil
	}
	missing := false
	for i := range t.Members {
		missing = missing || t.Members[i].ID == ""
	}
	if !missing {
		return t, revision, nil
	}
	updated, err := s.Save(ctx, &t, revision)
	if err != nil {
		return t, revision, fmt.Errorf("migrate runtime memberships: %w", err)
	}
	return t, updated, nil
}
