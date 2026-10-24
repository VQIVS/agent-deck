package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
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

func recordReportEstimation(ctx context.Context, manager *team.Manager) error {
	_, store, err := RuntimePool(func(context.Context) (*team.Manager, error) { return manager, nil })(ctx)
	if err != nil {
		return err
	}
	state, err := store.Load(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load estimation board: %w", err)
	}
	if err := team.RecordEstimationHistory(ctx, store, &state.Board); err != nil {
		return fmt.Errorf("report saved; sizing history repair failed: %w", err)
	}
	return nil
}
