package team

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
)

type memberRename struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

type memberRenameJournal struct {
	Pending []memberRename `json:"pending"`
}

// MemberRenamePendingError means team persistence succeeded and replay is required.
// Callers must retain the updated Manager.Version instead of retrying a stale save.
type MemberRenamePendingError struct{ Err error }

func (e *MemberRenamePendingError) Error() string {
	return "member edit saved; task references pending repair: " + e.Err.Error()
}
func (e *MemberRenamePendingError) Unwrap() error { return e.Err }

// SaveMemberChanges coordinates existing-team edits with current open-task names.
// A durable intent precedes the team CAS; replay checks immutable identity before
// touching the board. An error after team persistence explicitly requires repair.
func SaveMemberChanges(ctx context.Context, manager *Manager, candidate *Team, board BoardStore) error {
	if manager == nil || candidate == nil {
		return fmt.Errorf("member changes require current manager and candidate")
	}
	if manager.ReadOnly || manager.Store.ReadOnly || board.ReadOnly {
		return fmt.Errorf("read-only: member changes disabled")
	}
	if candidate.Name != manager.Team.Name {
		return fmt.Errorf("member edit cannot rename team")
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	return board.Exclusive(ctx, func() error {
		return saveMemberChangesUnlocked(ctx, manager, candidate, board)
	})
}

func saveMemberChangesUnlocked(ctx context.Context, manager *Manager, candidate *Team, board BoardStore) error {
	_, version, err := manager.Store.Load(ctx, manager.Team.Name)
	if err != nil {
		return err
	}
	if !bytes.Equal(version, manager.Version) {
		return fmt.Errorf("member edit is stale; reload team")
	}
	if err := repairMemberRenamesUnlocked(ctx, manager, board); err != nil {
		return err
	}
	intents, err := memberRenameIntents(&manager.Team, candidate)
	if err != nil {
		return err
	}
	journal := memberRenameJournal{Pending: intents}
	if len(intents) > 0 {
		if err := board.write(ctx, "member-renames.json", &journal); err != nil {
			return err
		}
	}
	previous := manager.Team
	manager.Team = *candidate
	if err := manager.Save(ctx); err != nil {
		manager.Team = previous
		return fmt.Errorf("save member edit: %w", err)
	}
	if err := repairMemberRenamesUnlocked(ctx, manager, board); err != nil {
		return &MemberRenamePendingError{Err: err}
	}
	*candidate = manager.Team
	return nil
}

func memberRenameIntents(before, after *Team) ([]memberRename, error) {
	var changes []memberRename
	for i := range before.Members {
		old := &before.Members[i]
		for j := range after.Members {
			next := &after.Members[j]
			if old.ID == "" || old.ID != next.ID || old.Name == next.Name {
				continue
			}
			for k := range before.Members {
				if before.Members[k].ID != old.ID && before.Members[k].Name == next.Name {
					return nil, fmt.Errorf("member rename target was owned by another identity")
				}
			}
			changes = append(changes, memberRename{ID: old.ID, From: old.Name, To: next.Name})
		}
	}
	return changes, nil
}

// RepairMemberRenames retries saved cross-store intents after restart. Read-only
// observation never repairs; callers must provide current private team bindings.
func RepairMemberRenames(ctx context.Context, manager *Manager, board BoardStore) error {
	if manager == nil {
		return fmt.Errorf("member rename repair requires current manager")
	}
	if manager.ReadOnly || manager.Store.ReadOnly || board.ReadOnly {
		return nil
	}
	var journal memberRenameJournal
	if err := board.read(ctx, "member-renames.json", &journal); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(journal.Pending) == 0 {
		return nil
	}
	return board.Exclusive(ctx, func() error {
		current, version, err := manager.Store.Load(ctx, manager.Team.Name)
		if err != nil {
			return err
		}
		checked := *manager
		checked.Team, checked.Version = current, version
		return repairMemberRenamesUnlocked(ctx, &checked, board)
	})
}

func repairMemberRenamesUnlocked(ctx context.Context, manager *Manager, store BoardStore) error {
	var journal memberRenameJournal
	if err := store.read(ctx, "member-renames.json", &journal); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(journal.Pending) == 0 {
		return nil
	}
	state, err := store.LoadUnlocked(ctx)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	changed := false
	for _, rename := range journal.Pending {
		if !renameBindingCurrent(&manager.Team, rename) {
			continue
		}
		changed = remapOpenTaskNames(&state.Board, rename) || changed
	}
	if changed {
		if err := state.Validate(); err != nil {
			return err
		}
		state.Revision++
		if err := store.write(ctx, "board.json", &state); err != nil {
			return err
		}
	}
	return store.write(ctx, "member-renames.json", &memberRenameJournal{})
}

func remapOpenTaskNames(board *TaskBoard, rename memberRename) bool {
	changed := false
	for i := range board.Tasks {
		task := &board.Tasks[i]
		category := board.Category(task.State)
		if category == "done" || category == "shipped" || category == TaskCancelled {
			continue
		}
		for _, field := range []*string{&task.Assignee, &task.Worker, &task.Reviewer} {
			if *field == rename.From {
				*field = rename.To
				changed = true
			}
		}
	}
	return changed
}

func renameBindingCurrent(current *Team, rename memberRename) bool {
	found := false
	for i := range current.Members {
		member := &current.Members[i]
		if member.ID == rename.ID && member.Name == rename.To {
			found = true
		}
		if member.ID != rename.ID && (member.Name == rename.From || member.Name == rename.To) {
			return false
		}
	}
	return found
}
