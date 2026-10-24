package mcp

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/vqivs/agent-deck/internal/team"
)

type poolIdentityBinding struct {
	Team    team.Team `json:"team"`
	Version []byte    `json:"version"`
}

// SetupPoolWorker mediates UI lifecycle setup through the authenticated runtime owner.
// Only persisted current bindings are accepted; credentials never cross this API.
func (c *RuntimeConnection) SetupPoolWorker(ctx context.Context, manager *team.Manager, index int) error {
	if c.readOnly {
		return fmt.Errorf("read-only: pool workflow setup disabled")
	}
	if err := poolWorkflowAllowed(ctx, manager, index); err != nil {
		return err
	}
	member := &manager.Team.Members[index]
	request := &userRequest{
		Action: "setup_pool_worker", Member: member.Name,
		Text: member.ID, Since: hex.EncodeToString(manager.Version),
	}
	var binding poolIdentityBinding
	if c.owner != nil {
		result, err := c.owner.setupPoolWorkerUser(ctx, request)
		if err != nil {
			return err
		}
		binding = result
	} else if err := c.call(ctx, request, &binding); err != nil {
		return err
	}
	if binding.Team.Name != manager.Team.Name {
		return fmt.Errorf("pool workflow response team changed")
	}
	manager.Team, manager.Version = binding.Team, binding.Version
	return nil
}

// No stepMu: the UI lifecycle holds the board lock, which Step may be awaiting.
// Store revision checks and credentialMu serialize binding effects without lock inversion.
func (r *Runtime) setupPoolWorkerUser(ctx context.Context, input *userRequest) (poolIdentityBinding, error) {
	manager, err := r.current(ctx)
	if err != nil {
		return poolIdentityBinding{}, err
	}
	if hex.EncodeToString(manager.Version) != input.Since {
		return poolIdentityBinding{}, fmt.Errorf("pool workflow binding revision changed; retry current lifecycle")
	}
	index, err := manager.MemberIndex(input.Member)
	if err != nil || manager.Team.Members[index].ID != input.Text {
		return poolIdentityBinding{}, fmt.Errorf("pool workflow member identity changed")
	}
	if err := r.SetupPoolWorker(ctx, manager, index); err != nil {
		return poolIdentityBinding{}, err
	}
	return poolIdentityBinding{Team: manager.Team, Version: manager.Version}, nil
}
