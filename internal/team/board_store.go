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
	var state BoardState
	if err := s.read(ctx, "board.json", &state); err != nil {
		return state, err
	}
	err := state.Validate()
	return state, err
}

func (s *BoardState) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("board state requires version 1")
	}
	if err := s.Board.Validate(); err != nil {
		return err
	}
	return s.Policy.Validate()
}

func (s BoardStore) Save(ctx context.Context, state *BoardState, expected uint64) error {
	return s.Exclusive(ctx, func() error {
		current, err := s.Load(ctx)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if current.Revision != expected {
			return fmt.Errorf("board changed; reload before saving")
		}
		next := *state
		next.Revision = expected + 1
		if err := next.Validate(); err != nil {
			return err
		}
		if err := s.write(ctx, "board.json", &next); err != nil {
			return err
		}
		state.Revision = next.Revision
		return nil
	})
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
	return s.write(ctx, "actions.json", entries)
}

func (s BoardStore) Approve(ctx context.Context, actionID string, userConfirmed bool) error {
	if !userConfirmed || actionID == "" {
		return fmt.Errorf("board approval requires privileged confirmed user action")
	}
	state, err := s.Load(ctx)
	if err != nil {
		return err
	}
	if state.Approved == nil {
		state.Approved = map[string]bool{}
	}
	state.Approved[actionID] = true
	// Approval does not change the desired revision encoded in action IDs.
	return s.Exclusive(ctx, func() error {
		current, err := s.Load(ctx)
		if err != nil {
			return err
		}
		if current.Revision != state.Revision {
			return fmt.Errorf("board changed during approval")
		}
		return s.write(ctx, "board.json", &state)
	})
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
