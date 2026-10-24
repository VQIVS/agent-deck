package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ConnectRuntime attaches to an existing authenticated owner without creating any state.
func ConnectRuntime(
	ctx context.Context, teamName, journalPath, stateDir string, readOnly bool,
) (*RuntimeConnection, error) {
	if teamName == "" || journalPath == "" {
		return nil, fmt.Errorf("runtime attachment requires team and journal path")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stateDir == "" {
		root := os.Getenv("XDG_STATE_HOME")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("resolve runtime attachment home: %w", err)
			}
			root = filepath.Join(home, ".local", "state")
		}
		stateDir = filepath.Join(root, "agent-deck", "runtime")
	}
	absoluteState, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime attachment state path: %w", err)
	}
	options := RuntimeOptions{JournalPath: journalPath, StateDir: absoluteState}
	state, err := readDiscovery(discoveryPath(&options))
	if err != nil {
		return nil, fmt.Errorf("discover existing runtime: %w", err)
	}
	if state.Team != teamName {
		return nil, fmt.Errorf("runtime attachment team mismatch")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	connection := &RuntimeConnection{state: state, readOnly: readOnly, client: &http.Client{
		Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	var pong struct {
		Team string `json:"team"`
	}
	if err := connection.call(ctx, &userRequest{Action: "ping"}, &pong); err != nil {
		connection.client.CloseIdleConnections()
		return nil, fmt.Errorf("authenticate existing runtime: %w", err)
	}
	if pong.Team != teamName {
		connection.client.CloseIdleConnections()
		return nil, fmt.Errorf("authenticated runtime reports different team")
	}
	return connection, nil
}
