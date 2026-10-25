package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

type taskLeaseOrigin struct {
	Manual  []string `json:"manual"`
	Derived []string `json:"derived"`
}

func leaseOriginStore(manager *Manager) BoardStore {
	return BoardStore{
		Root: filepath.Join(manager.Store.Root, ".lease-origins"), Team: Slug(manager.Team.Name),
		ReadOnly: manager.ReadOnly || manager.Store.ReadOnly,
	}
}

func loadLeaseOrigins(ctx context.Context, manager *Manager) (map[string]taskLeaseOrigin, error) {
	origins := map[string]taskLeaseOrigin{}
	err := leaseOriginStore(manager).read(ctx, "origins.json", &origins)
	if errors.Is(err, os.ErrNotExist) {
		return origins, nil
	}
	return origins, err
}

// RecordTaskLeaseGrant records manual ownership before a scheduler changes effective task paths.
func RecordTaskLeaseGrant(ctx context.Context, manager *Manager, index int, paths []string) error {
	if manager == nil || manager.ReadOnly || manager.Store.ReadOnly {
		return fmt.Errorf("read-only: lease grant disabled")
	}
	if index < 0 || index >= len(manager.Team.Members) {
		return fmt.Errorf("unknown lease member")
	}
	member := &manager.Team.Members[index]
	if member.ID == "" {
		return fmt.Errorf("lease origin requires stable member ID")
	}
	origins, err := loadLeaseOrigins(ctx, manager)
	if err != nil {
		return err
	}
	origin, exists := origins[member.ID]
	if !exists {
		origin.Manual = slices.Clone(member.Leases)
	}
	origin.Derived = append(origin.Derived, paths...)
	slices.Sort(origin.Derived)
	origin.Derived = slices.Compact(origin.Derived)
	origins[member.ID] = origin
	store := leaseOriginStore(manager)
	dir, err := store.directory()
	if err != nil {
		return err
	}
	if err := privateReportDir(dir); err != nil {
		return err
	}
	return store.write(ctx, "origins.json", origins)
}

func recordFreshTaskLease(ctx context.Context, manager *Manager, index int) error {
	paths := manager.Team.Members[index].Leases
	manager.Team.Members[index].Leases = nil
	err := RecordTaskLeaseGrant(ctx, manager, index, paths)
	manager.Team.Members[index].Leases = paths
	return err
}

func recordReconciledGrants(ctx context.Context, manager *Manager, board *TaskBoard) error {
	for i := range manager.Team.Members {
		member := &manager.Team.Members[i]
		_, active := taskLeasePaths(member.Name, board)
		if member.Role == "worker" && len(active) > 0 {
			if err := RecordTaskLeaseGrant(ctx, manager, i, active); err != nil {
				return fmt.Errorf("record reconciled task lease grant: %w", err)
			}
		}
	}
	return nil
}

func assignmentLeasePaths(ctx context.Context, manager *Manager, index int, paths []string) ([]string, error) {
	if err := RecordTaskLeaseGrant(ctx, manager, index, paths); err != nil {
		return nil, err
	}
	member := &manager.Team.Members[index]
	origins, err := loadLeaseOrigins(ctx, manager)
	if err != nil {
		return nil, err
	}
	origin := origins[member.ID]
	leases := slices.Clone(member.Leases)
	leases = slices.DeleteFunc(leases, func(path string) bool {
		return slices.Contains(origin.Derived, path) && !slices.Contains(origin.Manual, path)
	})
	leases = append(leases, origin.Manual...)
	leases = append(leases, paths...)
	slices.Sort(leases)
	return slices.Compact(leases), nil
}

func projectOriginLeases(members []Member, board *TaskBoard, origins map[string]taskLeaseOrigin) ([]Member, error) {
	projected := slices.Clone(members)
	for i := range projected {
		member := &projected[i]
		if member.Role != "worker" {
			continue
		}
		_, active := taskLeasePaths(member.Name, board)
		origin, known := origins[member.ID]
		leases := slices.Clone(member.Leases)
		if known {
			leases = slices.DeleteFunc(leases, func(path string) bool {
				return slices.Contains(origin.Derived, path) && !slices.Contains(origin.Manual, path)
			})
			leases = append(leases, origin.Manual...)
		}
		leases = append(leases, active...)
		slices.Sort(leases)
		member.Leases = slices.Compact(leases)
	}
	return projected, ValidateLeases(projected)
}
