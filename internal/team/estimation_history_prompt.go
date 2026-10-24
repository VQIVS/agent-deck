package team

import (
	"context"
	"path/filepath"

	"github.com/vqivs/agent-deck/internal/server"
)

func estimationPrompt(ctx context.Context, t *Team, member *Member) string {
	if member.Role != "master" {
		return ""
	}
	root, err := server.StateRoot()
	if err != nil {
		return ""
	}
	return EstimationPlanningSummary(ctx, BoardStore{Root: filepath.Join(root, "boards"), Team: t.Name, ReadOnly: true})
}
