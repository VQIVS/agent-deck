package team

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func renameFixture(t *testing.T) (*Manager, BoardStore) {
	t.Helper()
	store, board := poolRuntimeFixture(t, "http://127.0.0.1:1")
	current, version, err := store.Load(t.Context(), "pool")
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Store: store, Team: current, Version: version}
	old := current.Members[1].Name
	state := BoardState{Version: 2, Policy: SchedulingPolicy{Approval: "off"}, Board: TaskBoard{Version: 2}}
	for _, stage := range []string{"ready", "in_progress", "review", "done"} {
		task := Task{
			ID: stage, Title: stage, State: stage, Assignee: old, Worker: old, Reviewer: old,
			Paths: Leases{stage + "/**"}, Criteria: []TaskCriterion{{ID: "c1", Text: "works"}},
			History: []TaskChange{{Actor: old, At: time.Now(), Field: "comment", To: "history"}},
		}
		if stage == "done" {
			task.LegacyCompletion = true
		}
		// Distinct assignees are required for simultaneously active tasks.
		if stage == "review" {
			task.Assignee, task.Worker = "another", "another"
		}
		state.Board.Tasks = append(state.Board.Tasks, task)
	}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	return manager, board
}

func TestMemberRenamePartialFailureRetainsSavedRevision(t *testing.T) {
	manager, board := renameFixture(t)
	candidate := manager.Team.PrivateCopy()
	candidate.Members[1].Name = "saved-before-interrupt"
	// Cancel once team persistence has happened: the intent must remain repairable.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := board.Exclusive(ctx, func() error {
		journal := memberRenameJournal{Pending: []memberRename{{
			ID:   manager.Team.Members[1].ID,
			From: manager.Team.Members[1].Name, To: candidate.Members[1].Name,
		}}}
		if err := board.write(ctx, "member-renames.json", &journal); err != nil {
			return err
		}
		manager.Team = candidate
		if err := manager.Save(ctx); err != nil {
			return err
		}
		cancel()
		return &MemberRenamePendingError{Err: repairMemberRenamesUnlocked(ctx, manager, board)}
	})
	var pending *MemberRenamePendingError
	if !errors.As(err, &pending) {
		t.Fatal("partial failure not explicit", err)
	}
	if err := RepairMemberRenames(t.Context(), manager, board); err != nil {
		t.Fatal(err)
	}
	state, err := board.Load(t.Context())
	if err != nil || state.Board.Tasks[0].Assignee != "saved-before-interrupt" {
		t.Fatal("partial failure retry lost ownership", err)
	}
}

func TestMemberRenameOpenTasksAndHistory(t *testing.T) {
	manager, board := renameFixture(t)
	old := manager.Team.Members[1]
	before, err := board.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	candidate := manager.Team.PrivateCopy()
	candidate.Members[1].Name = "renamed"
	if err := SaveMemberChanges(t.Context(), manager, &candidate, board); err != nil {
		t.Fatal(err)
	}
	after, err := board.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := range after.Board.Tasks {
		task := &after.Board.Tasks[i]
		if task.State == "done" {
			if task.Assignee != old.Name || task.Reviewer != old.Name {
				t.Fatal("terminal references rewritten")
			}
		} else {
			if task.Reviewer != "renamed" {
				t.Fatal("reviewer not renamed", task)
			}
			if task.State != "review" && (task.Assignee != "renamed" || task.Worker != "renamed") {
				t.Fatal("owner lost", task)
			}
		}
		if !reflect.DeepEqual(task.History, before.Board.Tasks[i].History) {
			t.Fatal("historical actor rewritten")
		}
	}
	if candidate.Members[1].ID != old.ID || candidate.Members[1].Session != old.Session ||
		!reflect.DeepEqual(candidate.Members[1].Permissions, old.Permissions) {
		t.Fatal("permission/session identity changed")
	}
	revision := after.Revision
	if err := SaveMemberChanges(t.Context(), manager, &candidate, board); err != nil {
		t.Fatal(err)
	}
	after, err = board.Load(t.Context())
	if err != nil || after.Revision != revision {
		t.Fatal("noop rewrote board", err)
	}
}

func TestMemberRenameCollisionStaleAndRestartRepair(t *testing.T) {
	manager, board := renameFixture(t)
	stale := *manager
	candidate := manager.Team.PrivateCopy()
	candidate.Members[1].Name = candidate.Members[0].Name
	if err := SaveMemberChanges(t.Context(), manager, &candidate, board); err == nil {
		t.Fatal("collision accepted")
	}
	candidate = manager.Team.PrivateCopy()
	candidate.Members[1].Name = "renamed"
	// Simulate a crash after intent/team persistence but before board repair.
	journal := memberRenameJournal{Pending: []memberRename{{
		ID:   manager.Team.Members[1].ID,
		From: manager.Team.Members[1].Name, To: "renamed",
	}}}
	if err := board.Exclusive(t.Context(), func() error {
		return board.write(t.Context(), "member-renames.json", &journal)
	}); err != nil {
		t.Fatal(err)
	}
	manager.Team = candidate
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := SaveMemberChanges(t.Context(), &stale, &candidate, board); err == nil {
		t.Fatal("stale rename accepted")
	}
	loaded, version, err := manager.Store.Load(t.Context(), manager.Team.Name)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Manager{Store: manager.Store, Team: loaded, Version: version}
	if err := RepairMemberRenames(t.Context(), restarted, board); err != nil {
		t.Fatal(err)
	}
	if err := RepairMemberRenames(t.Context(), restarted, board); err != nil {
		t.Fatal(err)
	}
	state, err := board.Load(t.Context())
	if err != nil || state.Board.Tasks[0].Assignee != "renamed" {
		t.Fatal("restart repair failed", err)
	}
}

func TestMemberRenameFailedIntentDoesNotRenameUnrelatedIdentity(t *testing.T) {
	manager, board := renameFixture(t)
	old := manager.Team.Members[1]
	journal := memberRenameJournal{Pending: []memberRename{{ID: old.ID, From: old.Name, To: "never-saved"}}}
	if err := board.Exclusive(t.Context(), func() error {
		return board.write(t.Context(), "member-renames.json", &journal)
	}); err != nil {
		t.Fatal(err)
	}
	if err := RepairMemberRenames(t.Context(), manager, board); err != nil {
		t.Fatal(err)
	}
	state, err := board.Load(t.Context())
	if err != nil || state.Board.Tasks[0].Assignee != old.Name {
		t.Fatal("uncommitted intent rewrote owner", err)
	}
	manager.ReadOnly = true
	candidate := manager.Team.PrivateCopy()
	candidate.Members[1].Name = "readonly"
	if err := SaveMemberChanges(t.Context(), manager, &candidate, board); err == nil {
		t.Fatal("readonly rename accepted")
	}
	if err := RepairMemberRenames(t.Context(), manager, board); err != nil {
		t.Fatal(err)
	}
}

func TestMemberRenameRequiresBindings(t *testing.T) {
	if err := SaveMemberChanges(t.Context(), nil, &Team{}, BoardStore{}); err == nil {
		t.Fatal("nil manager accepted")
	}
	if err := SaveMemberChanges(t.Context(), &Manager{}, nil, BoardStore{}); err == nil {
		t.Fatal("nil candidate accepted")
	}
	if err := RepairMemberRenames(t.Context(), nil, BoardStore{}); err == nil {
		t.Fatal("nil repair manager accepted")
	}
}
