package fsutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicReplacesWithRequestedMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(t.Context(), path, []byte("new"), 0o600, ".state-*"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatal(string(data), err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	assertNoTemps(t, dir)
}

type cancelBeforeRename struct {
	context.Context
	calls int
}

func (c *cancelBeforeRename) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

func TestWriteAtomicFailureAndCancellationPreserveDestination(t *testing.T) {
	for _, scenario := range []string{"canceled", "before-rename", "rename", "missing-parent"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state")
			ctx := t.Context()
			switch scenario {
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "before-rename":
				ctx = &cancelBeforeRename{Context: ctx}
			case "rename":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "child")
			case "missing-parent":
				path = filepath.Join(dir, "missing", "state")
			}
			if scenario != "missing-parent" {
				if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "rename" {
				path = filepath.Dir(path)
			}
			err := WriteAtomic(ctx, path, []byte("new"), 0o600, ".state-*")
			if err == nil {
				t.Fatal("failure accepted")
			}
			if scenario == "canceled" || scenario == "before-rename" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				data, readErr := os.ReadFile(path)
				if readErr != nil || string(data) != "old" {
					t.Fatal("canceled write changed destination", string(data), readErr)
				}
			}
			if scenario == "rename" {
				data, readErr := os.ReadFile(filepath.Join(path, "child"))
				if readErr != nil || string(data) != "old" {
					t.Fatal("rename failure changed destination", string(data), readErr)
				}
			}
			assertNoTemps(t, dir)
		})
	}
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, ".state-*"))
	if err != nil || len(paths) != 0 {
		t.Fatal("temporary files leaked", paths, err)
	}
}
