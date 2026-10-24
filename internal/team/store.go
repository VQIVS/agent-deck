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
	if t.ProjectOverlay {
		return nil, fmt.Errorf("project overlay is read-only; edit private configuration without the project overlay")
	}
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
	if err := ctx.Err(); err != nil {
		return err
	}
	f, e := os.CreateTemp(s.Root, ".team-*")
	if e != nil {
		return fmt.Errorf("create temporary team file: %w", e)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if e = f.Chmod(0o600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return fmt.Errorf("write temporary team file: %w", e)
	}
	if e := os.Rename(f.Name(), path); e != nil {
		return fmt.Errorf("replace team file: %w", e)
	}
	if dir, e := os.Open(s.Root); e == nil {
		_ = dir.Sync()
		_ = dir.Close()
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
