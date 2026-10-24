package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func boardStoreFixture(t *testing.T) (BoardStore, BoardState) {
	t.Helper()
	store := BoardStore{Root: filepath.Join(t.TempDir(), "boards"), Team: "pool"}
	state := BoardState{
		Version: 1, Board: TaskBoard{Version: 1},
		Policy: SchedulingPolicy{MinWorkers: 1, MaxWorkers: 2, Approval: "ask"},
	}
	if err := store.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	return store, state
}

func TestBoardStoreRevisionApprovalAndPrivacy(t *testing.T) {
	store, state := boardStoreFixture(t)
	if err := store.Save(t.Context(), &state, 0); err == nil {
		t.Fatal("stale board overwritten")
	}
	if err := store.Approve(t.Context(), "action", false); err == nil {
		t.Fatal("unconfirmed approval accepted")
	}
	if err := store.Approve(t.Context(), "action", true); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Approved["action"] || loaded.Revision != state.Revision {
		t.Fatal("approval changed desired revision", err)
	}
	for _, path := range []string{
		store.Root, filepath.Join(store.Root, "pool"), filepath.Join(store.Root, "pool", "board.json"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Fatal("non-private board path", path)
		}
	}
	store.ReadOnly = true
	if err := store.Save(t.Context(), &state, state.Revision); err == nil {
		t.Fatal("read-only board saved")
	}
	if _, err := store.Load(t.Context()); err != nil {
		t.Fatal("read-only load failed", err)
	}
}

func TestBoardJournalPersistsAndLockCancellation(t *testing.T) {
	store, _ := boardStoreFixture(t)
	entry := ReconcileEntry{Action: ReconcileAction{ID: "action"}, State: "intent"}
	if err := store.Exclusive(t.Context(), func() error { return store.Append(t.Context(), &entry) }); err != nil {
		t.Fatal(err)
	}
	entries, err := store.Entries(t.Context())
	if err != nil || len(entries) != 1 || entries[0].Action.ID != "action" {
		t.Fatal("journal intent lost", err)
	}
	if err := store.Exclusive(t.Context(), func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		err := store.Exclusive(ctx, func() error { return nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("lock did not honor context", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
