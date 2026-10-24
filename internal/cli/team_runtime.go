package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/qivs/agent-deck/internal/mcp"
	"github.com/qivs/agent-deck/internal/team"
)

func superviseTeams(ctx context.Context) {
	store := team.Store{Root: team.DefaultRoot()}
	connections := map[string]*mcp.Runtime{}
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		startTeamRuntimes(ctx, store, connections)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func startTeamRuntimes(ctx context.Context, store team.Store, connections map[string]*mcp.Runtime) {
	teams, err := store.List(ctx)
	if err != nil {
		return
	}
	for i := range teams {
		if !teams[i].LeasePool {
			continue
		}
		name := teams[i].Name
		if connections[name] != nil {
			if connections[name].Alive() {
				continue
			}
			_ = connections[name].Close()
			delete(connections, name)
		}
		connection, err := mcp.StartRuntime(ctx, &mcp.RuntimeOptions{
			OnError:      func(err error) { fmt.Fprintln(os.Stderr, "team runtime:", name, err) },
			JournalPath:  filepath.Join(store.Root, team.Slug(name)+".channel.jsonl"),
			SubmitReport: mcp.PersistRuntimeReport,
			Provider: func(ctx context.Context) (*team.Manager, error) {
				t, revision, err := store.Load(ctx, name)
				if err != nil {
					return nil, fmt.Errorf("load supervised team: %w", err)
				}
				return &team.Manager{Team: t, Store: store, Version: revision}, nil
			},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "team runtime:", name, err)
		} else {
			connections[name] = connection
		}
	}
}
