package opencode

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
)

// Inventory collects sessions, scoped activity, directories, and partial-discovery warnings.
type Inventory struct {
	Sessions []Session
	Active   map[string]bool
	Dirs     []string
	Warnings []string
}

// Discover follows project worktrees, sandboxes, and session directories. Session
// lists may be project-wide even with ?directory, so each returned directory gets
// its own status request and SSE subscription. It uses HTTP exclusively.
func (c *Client) Discover(ctx context.Context) (Inventory, error) {
	v := Inventory{Active: map[string]bool{}}
	projects, err := c.Projects(ctx)
	if err != nil {
		return v, err
	}
	queue := []string{}
	queued := map[string]bool{}
	add := func(dir string) {
		if filepath.IsAbs(dir) && !queued[dir] {
			queued[dir] = true
			queue = append(queue, dir)
		}
	}
	for _, p := range projects {
		add(p.Worktree)
		for _, dir := range p.Sandboxes {
			add(dir)
		}
	}
	seen := map[string]bool{}
	for i := 0; i < len(queue); i++ {
		dir := queue[i]
		sessions, e := c.Sessions(ctx, dir)
		if e != nil {
			v.Warnings = append(v.Warnings, fmt.Sprintf("%s: %v", dir, e))
			continue
		}
		v.Dirs = append(v.Dirs, dir)
		for _, s := range sessions {
			add(s.Directory)
			key := s.Directory + "\x00" + s.ID
			if !seen[key] {
				seen[key] = true
				v.Sessions = append(v.Sessions, s)
			}
		}
		c.discoverStatus(ctx, dir, &v)
	}
	sort.Slice(v.Sessions, func(i, j int) bool {
		a, b := v.Sessions[i], v.Sessions[j]
		if a.Directory != b.Directory {
			return a.Directory < b.Directory
		}
		return a.ID < b.ID
	})
	return v, nil
}

func (c *Client) discoverStatus(ctx context.Context, dir string, inventory *Inventory) {
	statuses, err := c.Status(ctx, dir)
	if err != nil {
		inventory.Warnings = append(inventory.Warnings, err.Error())
		return
	}
	for id, status := range statuses {
		if status.Type != "idle" {
			inventory.Active[dir+"\x00"+id] = true
		}
	}
}
