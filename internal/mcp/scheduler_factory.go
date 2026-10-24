package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/qivs/agent-deck/internal/server"
	"github.com/qivs/agent-deck/internal/team"
)

func RuntimeScheduler(provider ManagerProvider) (*SchedulerTools, error) {
	return NewPoolScheduler(func(ctx context.Context) (*team.Manager, team.BoardStore, error) {
		manager, err := provider(ctx)
		if err != nil {
			return nil, team.BoardStore{}, err
		}
		root, err := server.StateRoot()
		if err != nil {
			return nil, team.BoardStore{}, fmt.Errorf("resolve pool state: %w", err)
		}
		board := team.BoardStore{Root: filepath.Join(root, "boards"), Team: manager.Team.Name, ReadOnly: manager.ReadOnly}
		return manager, board, nil
	})
}

func configureScheduler(o *RuntimeOptions) error {
	if o.Scheduler != nil {
		return nil
	}
	var err error
	o.Scheduler, err = RuntimeScheduler(o.Provider)
	return err
}
