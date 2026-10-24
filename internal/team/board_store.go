package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"
)

// BoardState stores private scheduler intent and approval separately from project policy.
type BoardState struct {
	Version   int              `json:"version"`
	Revision  uint64           `json:"revision"`
	Board     TaskBoard        `json:"board"`
	Policy    SchedulingPolicy `json:"policy"`
	Approved  map[string]bool  `json:"approved"`
	LastScale time.Time        `json:"last_scale,omitempty"`
}

// BoardStore serializes durable board changes and reconcile effects under one private team directory.
type BoardStore struct {
	Root     string
	Team     string
	ReadOnly bool
}

func (s BoardStore) directory() (string, error) {
	if s.Root == "" || s.Team == "" || Slug(s.Team) != s.Team {
		return "", fmt.Errorf("board store requires root and slug team name")
	}
	return filepath.Join(s.Root, s.Team), nil
}

func (s BoardStore) Exclusive(ctx context.Context, fn func() error) error {
	if s.ReadOnly {
		return fmt.Errorf("read-only: board changes disabled")
	}
	dir, err := s.directory()
	if err != nil {
		return err
	}
	for _, path := range []string{s.Root, dir} {
		if err := privateReportDir(path); err != nil {
			return err
		}
	}
	fd, err := syscall.Open(filepath.Join(dir, ".lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open board lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), "board-lock")
	defer func() { _ = file.Close() }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("lock board: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()
	return fn()
}

func (s BoardStore) Load(ctx context.Context) (BoardState, error) {
	var raw BoardState
	if err := s.read(ctx, "board.json", &raw); err != nil {
		return raw, err
	}
	if s.ReadOnly || (raw.Version == 2 && raw.Board.Version == 2) {
		return s.loadBoard(ctx)
	}
	var result BoardState
	err := s.Exclusive(ctx, func() error {
		var err error
		result, err = s.LoadMigratedUnlocked(ctx)
		return err
	})
	return result, err
}

// LoadMigratedUnlocked persists migration and its exact backup without acquiring a lock.
// Writable callers must already hold Exclusive; read-only callers receive only an in-memory migration.
func (s BoardStore) LoadMigratedUnlocked(ctx context.Context) (BoardState, error) {
	var current BoardState
	if err := s.read(ctx, "board.json", &current); err != nil {
		return current, err
	}
	legacy := current.Version != 2 || current.Board.Version != 2
	if err := migrateBoardState(&current); err != nil {
		return current, err
	}
	if !legacy || s.ReadOnly {
		return current, nil
	}
	if err := s.backupBoard(ctx, &current); err != nil {
		return current, err
	}
	if err := s.write(ctx, "board.json", &current); err != nil {
		return current, err
	}
	return current, s.pruneBoardHistory()
}

// LoadUnlocked reads and migrates in memory without acquiring a lock or writing.
// Use inside Exclusive callbacks; public Load performs durable migration when writable.
func (s BoardStore) LoadUnlocked(ctx context.Context) (BoardState, error) {
	var state BoardState
	if err := s.read(ctx, "board.json", &state); err != nil {
		return state, err
	}
	if err := migrateBoardState(&state); err != nil {
		return state, err
	}
	err := state.Validate()
	return state, err
}

func (s BoardStore) loadBoard(ctx context.Context) (BoardState, error) { return s.LoadUnlocked(ctx) }

func (s *BoardState) Validate() error {
	if s.Version != 1 && s.Version != 2 {
		return fmt.Errorf("board state requires version 1 or 2")
	}
	if err := s.Board.Validate(); err != nil {
		return err
	}
	return s.Policy.Validate()
}

// Save is a trusted persistence boundary, not an authorization API. Call workflow mutations before saving.
// Integration must never pass an untrusted replacement board or report payload directly to Save.
func (s BoardStore) Save(ctx context.Context, state *BoardState, expected uint64) error {
	return s.Exclusive(ctx, func() error {
		current, err := s.loadBoard(ctx)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if current.Revision != expected {
			return fmt.Errorf("board changed; reload before saving")
		}
		next := *state
		next.Board = TaskBoard{
			Version: state.Board.Version, History: slices.Clone(state.Board.History),
			Tasks: make([]Task, len(state.Board.Tasks)),
		}
		for i := range state.Board.Tasks {
			next.Board.Tasks[i] = cloneTask(&state.Board.Tasks[i])
		}
		if err := migrateBoardState(&next); err != nil {
			return err
		}
		next.Revision = expected + 1
		if err := next.Validate(); err != nil {
			return err
		}
		if current.Revision != 0 {
			if err := s.backupBoard(ctx, &current); err != nil {
				return err
			}
		}
		if err := s.write(ctx, "board.json", &next); err != nil {
			return err
		}
		if err := s.pruneBoardHistory(); err != nil {
			return err
		}
		*state = next
		return nil
	})
}

func (s BoardStore) pruneBoardHistory() error {
	dir, err := s.directory()
	if err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(dir, "history", "*.json"))
	if err != nil {
		return fmt.Errorf("list board history: %w", err)
	}
	for len(paths) > 50 {
		if err := os.Remove(paths[0]); err != nil {
			return fmt.Errorf("prune board history: %w", err)
		}
		paths = paths[1:]
	}
	return nil
}

func (s BoardStore) backupBoard(ctx context.Context, state *BoardState) error {
	dir, err := s.directory()
	if err != nil {
		return err
	}
	history := filepath.Join(dir, "history")
	if err := privateReportDir(history); err != nil {
		return err
	}
	path := filepath.Join(history, fmt.Sprintf("%020d.json", state.Revision))
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect board backup: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "board.json"))
	if err != nil {
		return fmt.Errorf("read original board history: %w", err)
	}
	return replaceReport(ctx, path, data)
}

func (s BoardStore) read(ctx context.Context, name string, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := s.directory()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("read board state: %w", err)
	}
	if err := decodePolicyJSON(data, out); err != nil {
		return err
	}
	return nil
}

func decodePolicyJSON(data []byte, out any) error {
	if len(data) > 8<<20 {
		return fmt.Errorf("board state exceeds 8 MiB")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode board state: %w", err)
	}
	return nil
}

func (s BoardStore) write(ctx context.Context, name string, value any) error {
	if s.ReadOnly {
		return fmt.Errorf("read-only: board changes disabled")
	}
	dir, err := s.directory()
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode board state: %w", err)
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("board state exceeds 8 MiB")
	}
	return replaceReport(ctx, filepath.Join(dir, name), data)
}

func (s BoardStore) Entries(ctx context.Context) ([]ReconcileEntry, error) {
	var entries []ReconcileEntry
	err := s.read(ctx, "actions.json", &entries)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

// Append is called only while Exclusive is held, including by Reconciler.
func (s BoardStore) Append(ctx context.Context, entry *ReconcileEntry) error {
	entries, err := s.Entries(ctx)
	if err != nil {
		return err
	}
	entries = append(entries, *entry)
	compacted := compactBoardActions(entries, entry.Action.Revision)
	if err := s.write(ctx, "actions.json", compacted); err != nil {
		return err
	}
	return s.removeCompactedEffects(ctx, entries, compacted)
}

func (s BoardStore) removeCompactedEffects(ctx context.Context, previous, retained []ReconcileEntry) error {
	keep := make(map[string]bool, len(retained))
	for i := range retained {
		keep[retained[i].Action.ID] = true
	}
	latest := make(map[string]string, len(previous))
	for i := range previous {
		latest[previous[i].Action.ID] = previous[i].State
	}
	dir, err := s.directory()
	if err != nil {
		return err
	}
	for id, state := range latest {
		if keep[id] || state != "done" {
			continue
		}
		var effect lifecycleEffect
		if err := s.read(ctx, effectName(id), &effect); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if effect.State != "done" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, effectName(id))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove compacted lifecycle effect: %w", err)
		}
	}
	return nil
}

func (s BoardStore) Approve(ctx context.Context, actionID string, userConfirmed bool) error {
	if !userConfirmed || actionID == "" {
		return fmt.Errorf("board approval requires privileged confirmed user action")
	}
	// Approval does not change the desired revision encoded in action IDs.
	return s.Exclusive(ctx, func() error {
		state, err := s.LoadMigratedUnlocked(ctx)
		if err != nil {
			return err
		}
		if state.Approved == nil {
			state.Approved = make(map[string]bool)
		}
		state.Approved[actionID] = true
		return s.write(ctx, "board.json", &state)
	})
}

func compactBoardActions(entries []ReconcileEntry, revision string) []ReconcileEntry {
	latest := make(map[string]int, len(entries))
	for i := range entries {
		latest[entries[i].Action.ID] = i
	}
	result := make([]ReconcileEntry, 0, len(entries))
	for i := range entries {
		entry := &entries[i]
		if entry.Action.Revision == revision || (latest[entry.Action.ID] == i && entry.State == "intent") {
			result = append(result, *entry)
		}
	}
	return result
}

func (s BoardStore) Snapshot(ctx context.Context, manager *Manager, workers []SchedulerWorker,
	now time.Time,
) (ReconcileSnapshot, error) {
	state, err := s.Load(ctx)
	if err != nil {
		return ReconcileSnapshot{}, err
	}
	return ReconcileSnapshot{
		Team: manager.Team, Revision: fmt.Sprintf("%d", state.Revision), Approved: state.Approved,
		Input: SchedulingInput{
			Board: &state.Board, Policy: state.Policy, Workers: slices.Clone(workers), Now: now,
			LastScale: state.LastScale, ReadOnly: s.ReadOnly || manager.ReadOnly,
		},
	}, nil
}
