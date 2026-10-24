package ui

import (
	"context"
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qivs/agent-deck/internal/mcp"
	"github.com/qivs/agent-deck/internal/team"
)

type channelStarted struct {
	Connection *mcp.RuntimeConnection
	Team       string
	Err        error
}

func (m *Model) startChannel() tea.Cmd {
	if m.opts.ReadOnly || m.teams == nil || m.teams.Manager == nil || m.channel.Runtime != nil {
		return nil
	}
	store, name := m.teams.Store, m.teams.Manager.Team.Name
	readOnly := m.opts.ReadOnly
	ctx := m.teams.RootContext()
	events := m.events
	return func() tea.Msg {
		connection, err := mcp.OpenRuntime(ctx, &mcp.RuntimeOptions{
			OnError: func(err error) {
				select {
				case events <- actionMsg{Err: fmt.Errorf("channel runtime: %w", err)}:
				case <-ctx.Done():
				}
			},
			SubmitReport: mcp.PersistRuntimeReport,
			JournalPath:  filepath.Join(store.Root, team.Slug(name)+".channel.jsonl"),
			Provider: func(ctx context.Context) (*team.Manager, error) {
				t, revision, err := store.LoadRuntimeTeam(ctx, name)
				if err != nil {
					return nil, fmt.Errorf("load channel team: %w", err)
				}
				return &team.Manager{Team: t, Store: store, Version: revision, ReadOnly: readOnly}, nil
			},
		})
		return channelStarted{Connection: connection, Team: name, Err: err}
	}
}
