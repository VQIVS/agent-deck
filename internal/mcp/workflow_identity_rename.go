package mcp

import (
	"context"
	"fmt"

	"github.com/vqivs/agent-deck/internal/team"
)

// repairMemberTaskNames runs before credential locking/reconciliation. The board
// lock must never be acquired while credentialMu is held.
func (r *Runtime) repairMemberTaskNames(ctx context.Context, manager *team.Manager) error {
	if manager.ReadOnly {
		return nil
	}
	_, store, err := RuntimePool(r.options.Provider)(ctx)
	if err != nil {
		return err
	}
	if err := team.RepairMemberRenames(ctx, manager, store); err != nil {
		return fmt.Errorf("repair member task references: %w", err)
	}
	return nil
}
