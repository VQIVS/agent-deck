package mcp

import (
	"context"
	"errors"
	"fmt"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

// restoreWorkflowCredentials reinstalls credentials for members with a workflow identity after a
// runtime restart (shutdown cleanup revokes them) and registers them with the live instance.
func (r *Runtime) restoreWorkflowCredentials(ctx context.Context, manager *team.Manager) error {
	var failures []error
	for i := range manager.Team.Members {
		member := &manager.Team.Members[i]
		if member.ControlDir == "" || member.Session == "" || r.hasCredential(member) {
			continue
		}
		if err := r.InstallCredential(ctx, member.Name); err != nil {
			failures = append(failures, fmt.Errorf("restore %s credential: %w", member.Name, err))
			continue
		}
		if err := r.registerLive(ctx, manager, member); err != nil {
			failures = append(failures, fmt.Errorf("register %s tools: %w", member.Name, err))
		}
	}
	return errors.Join(failures...)
}

func (r *Runtime) hasCredential(member *team.Member) bool {
	r.credentialMu.Lock()
	defer r.credentialMu.Unlock()
	_, _, ok := r.findCredential(member.ID, member.Name)
	return ok
}

func (r *Runtime) registerLive(ctx context.Context, manager *team.Manager, member *team.Member) error {
	r.credentialMu.Lock()
	_, credential, ok := r.findCredential(member.ID, member.Name)
	r.credentialMu.Unlock()
	if !ok {
		return fmt.Errorf("credential missing after install")
	}
	config := map[string]any{
		"type": "remote", "url": r.URL, "oauth": false,
		"headers": map[string]string{"Authorization": "Bearer " + credential.token},
	}
	client := api.New(manager.Team.Server, false)
	// An open connection keeps its old token; drop it so the new credential is used.
	if err := client.DisconnectMCP(ctx, credential.dir, credential.entry); err != nil {
		return fmt.Errorf("drop stale tools connection: %w", err)
	}
	if err := client.RegisterMCP(ctx, credential.dir, credential.entry, config); err != nil {
		return fmt.Errorf("register live tools: %w", err)
	}
	return nil
}
