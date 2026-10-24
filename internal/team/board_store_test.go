package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestBoardConcurrentApprovalsMergeInsideLock(t *testing.T) {
	store, state := boardStoreFixture(t)
	var workers sync.WaitGroup
	for i := range 20 {
		workers.Go(func() {
			if err := store.Approve(t.Context(), fmt.Sprintf("action-%d", i), true); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	loaded, err := store.Load(t.Context())
	if err != nil || len(loaded.Approved) != 20 || loaded.Revision != state.Revision {
		t.Fatalf("concurrent approvals lost: %+v, %v", loaded, err)
	}
}

func TestBoardActionCompactionBeforeSizeCap(t *testing.T) {
	store, _ := boardStoreFixture(t)
	entries := []ReconcileEntry{
		{Action: ReconcileAction{ID: "old", Revision: "old"}, State: "intent"},
		{Action: ReconcileAction{ID: "old", Revision: "old"}, State: "done"},
		{Action: ReconcileAction{ID: "unfinished", Revision: "old"}, State: "intent"},
		{Action: ReconcileAction{ID: "current", Revision: "current"}, State: "intent"},
	}
	// JSON escapes the NUL payload, keeping the fixture below the old read cap.
	entries[0].Action.Worker = string(make([]byte, 500000))
	entries[1].Action.Worker = entries[0].Action.Worker
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := store.directory()
	if err := os.WriteFile(filepath.Join(dir, "actions.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := ReconcileEntry{Action: ReconcileAction{ID: "current", Revision: "current"}, State: "done"}
	if err := store.Exclusive(t.Context(), func() error { return store.Append(t.Context(), &entry) }); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Entries(t.Context())
	if err != nil || len(loaded) != 3 {
		t.Fatalf("compaction: %+v, %v", loaded, err)
	}
	for i := range loaded {
		if loaded[i].Action.ID == "old" {
			t.Fatal("obsolete completed action retained")
		}
	}
	if loaded[0].Action.ID != "unfinished" {
		t.Fatal("unresolved intent lost")
	}
	tooLarge := ReconcileEntry{
		Action: ReconcileAction{ID: "big", Revision: "current", Worker: string(make([]byte, 2<<20))}, State: "done",
	}
	if err := store.Exclusive(t.Context(), func() error { return store.Append(t.Context(), &tooLarge) }); err == nil {
		t.Fatal("current revision exceeded size cap")
	}
	if loaded, err := store.Entries(t.Context()); err != nil || len(loaded) != 3 {
		t.Fatal("oversize append corrupted history", err)
	}
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
