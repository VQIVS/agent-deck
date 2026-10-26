package team

import (
	"context"
	"errors"
	"fmt"
	"slices"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func taskLeasePaths(name string, board *TaskBoard) (owned, active []string) {
	for i := range board.Tasks {
		task := &board.Tasks[i]
		assigned := task.Assignee == name || task.Worker == name
		historical := assigned
		for j := range task.History {
			change := &task.History[j]
			if change.Field == "assignee" && (change.From == name || change.To == name) {
				historical = true
			}
		}
		if historical {
			owned = append(owned, task.Paths...)
		}
		if assigned && (board.Category(task.State) == "active" || board.Version == 1 && task.State == "assigned") {
			active = append(active, task.Paths...)
		}
	}
	owned = append(owned, boardHistoricalLeasePaths(name, board)...)
	return owned, active
}

func boardHistoricalLeasePaths(name string, board *TaskBoard) []string {
	var paths []string
	for i := range board.History {
		change := &board.History[i]
		if change.Field != "assignee" || change.From != name && change.To != name {
			continue
		}
		for j := range board.Tasks {
			if board.Tasks[j].ID == change.Task {
				paths = append(paths, board.Tasks[j].Paths...)
			}
		}
	}
	return paths
}

// ReconcileTaskLeases persists the current projection before refreshing complete native policies.
// Fingerprints are saved only after PATCH succeeds, making retries and restore repair failed effects.
// Callers serialize this with board mutation; busy workers are never aborted or prompted.
func ReconcileTaskLeases(ctx context.Context, manager *Manager, board *TaskBoard) error {
	if manager == nil || manager.ReadOnly || manager.Store.ReadOnly {
		return fmt.Errorf("read-only: task lease reconciliation disabled")
	}
	if !manager.Team.LeasePool {
		return nil
	}
	if err := board.Validate(); err != nil {
		return err
	}
	// A reconciliation can itself grant paths, including after restore. Record
	// their origin before persisting or applying the new effective permissions.
	if err := recordReconciledGrants(ctx, manager, board); err != nil {
		return err
	}
	origins, err := loadLeaseOrigins(ctx, manager)
	if err != nil {
		return err
	}
	projected, err := projectOriginLeases(manager.Team.Members, board, origins)
	if err != nil {
		return err
	}
	changed := false
	for i := range projected {
		if !slices.Equal(projected[i].Leases, manager.Team.Members[i].Leases) {
			changed = true
		}
	}
	if changed {
		manager.Team.Members = projected
		if err := manager.Save(ctx); err != nil {
			return fmt.Errorf("persist task lease projection: %w", err)
		}
	}
	for i := range manager.Team.Members {
		if err := refreshTaskLeasePolicy(ctx, manager, i); err != nil {
			return err
		}
	}
	return legacyLeaseDiagnostic(manager.Team.Members, board, origins)
}

func legacyLeaseDiagnostic(members []Member, board *TaskBoard, origins map[string]taskLeaseOrigin) error {
	var diagnostics []error
	for i := range members {
		member := &members[i]
		if _, known := origins[member.ID]; known || member.Role != "worker" {
			continue
		}
		owned, active := taskLeasePaths(member.Name, board)
		for _, path := range member.Leases {
			if slices.Contains(owned, path) && !slices.Contains(active, path) {
				diagnostics = append(diagnostics, fmt.Errorf(
					"legacy lease %q for %q retained: manual/task origin unknown; confirm migration", path, member.Name))
			}
		}
	}
	return errors.Join(diagnostics...)
}

func refreshTaskLeasePolicy(ctx context.Context, manager *Manager, index int) error {
	member := &manager.Team.Members[index]
	if member.Session == "" || member.Capability == "inherited" {
		return nil
	}
	fingerprint := manager.Team.LeaseFingerprint(member)
	if member.LeasePolicy == fingerprint {
		return nil
	}
	rules, err := manager.CreationRules(ctx, member)
	if err != nil {
		return err
	}
	client := api.New(manager.Team.Server, false)
	if err := client.ApplyPermissions(ctx, member.SessionDir(), member.Session, rules); err != nil {
		return fmt.Errorf("refresh task lease policy for %q: %w", member.Name, err)
	}
	member.LeasePolicy = fingerprint
	if err := manager.Save(ctx); err != nil {
		return fmt.Errorf("save refreshed task lease policy: %w", err)
	}
	return nil
}

// ReconcileTaskLeases reloads persisted task ownership for runtime startup and periodic repair.
func (p *PoolRuntime) ReconcileTaskLeases(ctx context.Context) error {
	if p.board.ReadOnly {
		return fmt.Errorf("read-only: task lease reconciliation disabled")
	}
	return p.board.Exclusive(ctx, func() error {
		state, err := p.board.LoadMigratedUnlocked(ctx)
		if err != nil {
			return err
		}
		manager, err := p.manager(ctx)
		if err != nil {
			return err
		}
		if err := state.Board.bindWorkflow(manager.Team.Workflow); err != nil {
			return err
		}
		return ReconcileTaskLeases(ctx, manager, &state.Board)
	})
}
