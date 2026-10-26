package team

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/vqivs/agent-deck/internal/fsutil"
)

// Store persists private team files with revision checks and prior-version backups.
type Store struct {
	Root     string
	ReadOnly bool
}

func DefaultRoot() string {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "agent-deck", "teams")
}
func (s Store) Path(name string) string { return filepath.Join(s.Root, Slug(name)+".yaml") }
func (s Store) Load(ctx context.Context, name string) (Team, []byte, error) {
	if err := ctx.Err(); err != nil {
		return Team{}, nil, err
	}
	b, e := os.ReadFile(s.Path(name))
	if e != nil {
		return Team{}, nil, fmt.Errorf("read team %q: %w", name, e)
	}
	t, e := Parse(b)
	return t, b, e
}

func (s Store) List(ctx context.Context) ([]Team, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths, e := filepath.Glob(filepath.Join(s.Root, "*.yaml"))
	if e != nil {
		return nil, fmt.Errorf("list team paths: %w", e)
	}
	var out []Team
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			return nil, fmt.Errorf("read team %q: %w", p, e)
		}
		t, e := Parse(b)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", p, e)
		}
		out = append(out, t)
	}
	return out, nil
}

// Save compares against the version the editor loaded before atomic replacement.
func (s Store) Save(ctx context.Context, t *Team, expected []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.ReadOnly {
		return nil, fmt.Errorf("read-only: team save disabled")
	}
	t.LeasePool = t.LeasePool || HasLeases(t.Members)
	if e := t.Validate(); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(s.Root, 0o700); e != nil {
		return nil, fmt.Errorf("create team directory: %w", e)
	}
	path := s.Path(t.Name)
	old, e := os.ReadFile(path)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, fmt.Errorf("read current team %q: %w", t.Name, e)
	}
	if !bytes.Equal(old, expected) {
		return nil, fmt.Errorf("team changed on disk; reload before saving")
	}
	var previous Team
	if len(old) > 0 {
		if err := yaml.Unmarshal(old, &previous); err != nil {
			return nil, fmt.Errorf("decode previous memberships: %w", err)
		}
	}
	if err := EnsureMembershipIDs(t.Members, previous.Members); err != nil {
		return nil, err
	}
	b, e := yaml.Marshal(t)
	if e != nil {
		return nil, fmt.Errorf("encode team %q: %w", t.Name, e)
	}
	if e := s.backup(ctx, t.Name, old); e != nil {
		return nil, e
	}
	return b, s.replace(ctx, path, b)
}

func (s Store) backup(ctx context.Context, name string, old []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(old) == 0 {
		return nil
	}
	history := filepath.Join(s.Root, ".history", Slug(name))
	if err := os.MkdirAll(history, 0o700); err != nil {
		return fmt.Errorf("create history: %w", err)
	}
	path := filepath.Join(history, fmt.Sprintf("%020d.yaml", time.Now().UnixNano()))
	if err := os.WriteFile(path, old, 0o600); err != nil {
		return fmt.Errorf("back up team %q: %w", name, err)
	}
	files, err := filepath.Glob(filepath.Join(history, "*.yaml"))
	if err != nil {
		return fmt.Errorf("list history: %w", err)
	}
	sort.Strings(files)
	for len(files) > 5 {
		if err := os.Remove(files[0]); err != nil {
			return fmt.Errorf("prune team history: %w", err)
		}
		files = files[1:]
	}
	return nil
}

func (s Store) replace(ctx context.Context, path string, b []byte) error {
	if err := fsutil.WriteAtomic(ctx, path, b, 0o600, ".team-*"); err != nil {
		return fmt.Errorf("replace team file: %w", err)
	}
	return nil
}

func (s Store) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.ReadOnly {
		return fmt.Errorf("read-only: delete disabled")
	}
	if err := os.Remove(s.Path(name)); err != nil {
		return fmt.Errorf("delete team %q: %w", name, err)
	}
	return nil
}
