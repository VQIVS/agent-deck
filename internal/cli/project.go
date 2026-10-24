package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qivs/agent-deck/internal/team"
)

func mergeProject(ctx context.Context, local *team.Team) error {
	policy, err := team.LoadProject(ctx, filepath.Join(local.Repo, "agent-deck.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load project policy: %w", err)
	}
	bindings := team.ProjectBindings{
		Repo: local.Repo, Server: local.Server, Base: local.Base,
		Members: map[string]team.Member{}, LeasePool: local.LeasePool,
	}
	for i := range local.Members {
		member := local.Members[i]
		bindings.Members[member.Name] = member
	}
	merged, err := policy.Merge(bindings)
	if err != nil {
		return fmt.Errorf("merge project policy: %w", err)
	}
	merged.Name = local.Name
	*local = merged
	return nil
}
