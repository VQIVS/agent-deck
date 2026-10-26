package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/vqivs/agent-deck/internal/team"
)

func PersistRuntimeReport(ctx context.Context, manager *team.Manager, member string,
	report *team.DoneReport,
) (any, error) {
	store := team.ReportStore{Root: filepath.Join(manager.Store.Root, ".reports"), ReadOnly: manager.ReadOnly}
	record, err := store.Submit(ctx, &manager.Team, member, manager.Team.DefinitionOfDone, report, nil)
	if err != nil {
		return nil, fmt.Errorf("persist runtime report: %w", err)
	}
	return record, nil
}
