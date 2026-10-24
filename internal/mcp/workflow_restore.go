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
		if member.ControlDir == "" || member.Session == "" {
			continue
		}
		if !r.hasCredential(member) {
			if err := r.InstallCredential(ctx, member.Name); err != nil {
				failures = append(failures, fmt.Errorf("restore %s credential: %w", member.Name, err))
				continue
			}
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
	defer r.credentialMu.Unlock()
	_, credential, ok := r.findCredential(member.ID, member.Name)
	if !ok {
		return fmt.Errorf("credential missing after install")
	}
	return r.registerCredential(ctx, manager, &credential)
}

func (r *Runtime) registerCredential(ctx context.Context, manager *team.Manager, credential *runtimeCredential) error {
	if credential.live {
		return nil
	}
	config := map[string]any{
		"type": "remote", "url": r.URL, "oauth": false,
		"headers": map[string]string{"Authorization": "Bearer " + credential.token},
	}
	client := api.New(manager.Team.Server, false)
	if err := client.RegisterMCP(ctx, credential.dir, credential.entry, config); err != nil {
		return fmt.Errorf("register live tools: %w", err)
	}
	credential.live = true
	r.credentials[credential.identity.Member] = *credential
	return nil
}
