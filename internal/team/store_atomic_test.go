package team

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreAtomicMigrationRetainsCASHistoryAndPermissions(t *testing.T) {
	store := Store{Root: t.TempDir()}
	configured := example()
	original, err := store.Save(t.Context(), &configured, nil)
	if err != nil {
		t.Fatal(err)
	}
	configured.Members[0].Brief = "updated"
	updated, err := store.Save(t.Context(), &configured, original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(t.Context(), &configured, original); err == nil {
		t.Fatal("stale revision accepted")
	}
	_, current, err := store.Load(t.Context(), configured.Name)
	if err != nil || !bytes.Equal(current, updated) {
		t.Fatal("stale save changed team", err)
	}
	paths, err := filepath.Glob(filepath.Join(store.Root, ".history", Slug(configured.Name), "*.yaml"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	backup, err := os.ReadFile(paths[0])
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("previous version backup changed", err)
	}
	info, err := os.Stat(store.Path(configured.Name))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Save(ctx, &configured, updated); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	temps, err := filepath.Glob(filepath.Join(store.Root, ".team-*"))
	if err != nil || len(temps) != 0 {
		t.Fatal("temporary files leaked", temps, err)
	}
}
