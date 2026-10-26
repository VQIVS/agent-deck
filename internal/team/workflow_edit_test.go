package team

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type workflowAfterTeamSaveContext struct {
	context.Context
	path     string
	previous []byte
}

func (c workflowAfterTeamSaveContext) Err() error {
	current, err := os.ReadFile(c.path)
	if err == nil && !bytes.Equal(current, c.previous) {
		return context.Canceled
	}
	return c.Context.Err()
}

func TestWorkflowRenamePartialSaveRepairsWithRefreshedRetry(t *testing.T) {
	store, boardStore := poolRuntimeFixture(t, "http://127.0.0.1:1")
	pool, err := OpenPool(t.Context(), store, "pool", boardStore)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := pool.manager(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	board, mutation := workflowFixture(t)
	state := BoardState{Version: 2, Board: *board, Policy: SchedulingPolicy{Approval: "off"}}
	if err := boardStore.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	boardPath := filepath.Join(boardStore.Root, boardStore.Team, "board.json")
	oldBoard, err := os.ReadFile(boardPath)
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := bytes.Clone(manager.Version)
	evidence, _ := json.Marshal(state.Board.Tasks[0])
	definition := DefaultWorkflow()
	definition.Stages[0].Name = "Ideas"
	renamed := map[string]string{"backlog": "Ideas"}
	ctx := workflowAfterTeamSaveContext{Context: t.Context(), path: store.Path("pool"), previous: oldVersion}
	err = SaveWorkflowRenamed(ctx, manager, boardStore, definition, renamed, state.Revision, mutation)
	if err == nil || !strings.Contains(err.Error(), "team workflow saved but board workflow pending") {
		t.Fatal("missing explicit partial-save error", err)
	}
	configured, version, err := store.Load(t.Context(), "pool")
	if err != nil || configured.Workflow.Category("Ideas") != "todo" ||
		bytes.Equal(oldVersion, manager.Version) || !bytes.Equal(version, manager.Version) {
		t.Fatal("team save or manager revision lost", err)
	}
	unchanged, err := os.ReadFile(boardPath)
	if err != nil || !bytes.Equal(oldBoard, unchanged) {
		t.Fatal("failed board write changed original", err)
	}
	refreshed, err := pool.manager(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := boardStore.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveWorkflowRenamed(t.Context(), refreshed, boardStore, definition, renamed,
		loaded.Revision, mutation); err != nil {
		t.Fatal("repair failed", err)
	}
	repaired, err := boardStore.Load(t.Context())
	if err != nil || repaired.Board.Tasks[0].State != "Ideas" || repaired.Board.Category("Ideas") != "todo" {
		t.Fatal("rename not repaired", err)
	}
	repaired.Board.Tasks[0].State = "backlog"
	actual, _ := json.Marshal(repaired.Board.Tasks[0])
	if !bytes.Equal(evidence, actual) {
		t.Fatal("repair changed task evidence beyond placement")
	}
}

func TestRenameOccupiedStagePreservesEvidence(t *testing.T) {
	board, mutation := workflowFixture(t)
	history, _ := json.Marshal(board.Tasks[0].History)
	if err := board.RenameStage("backlog", "Ideas", mutation); err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].State != "Ideas" || board.Category("Ideas") != "todo" {
		t.Fatal(board.Tasks[0])
	}
	after, _ := json.Marshal(board.Tasks[0].History)
	if !bytes.Equal(history, after) {
		t.Fatal("rename rewrote evidence")
	}
	if err := board.MoveTask("task", "ready", mutation); err != nil {
		t.Fatal(err)
	}
	if err := board.RenameStage("ready", "done", mutation); err == nil {
		t.Fatal("duplicate name accepted")
	}
}

func TestCustomBriefDeliveryKeyStableAcrossHistoryAndRename(t *testing.T) {
	w := customWorkflow()
	board := TaskBoard{Version: 2, Workflow: w}
	mutation := TaskMutation{Actor: TaskActor{User: true}, Now: time.Now().UTC()}
	task := Task{
		ID: "brief", Title: "brief", Assignee: "worker", Paths: Leases{"src/**"},
		Criteria: []TaskCriterion{{ID: "c1", Text: "check"}},
	}
	if err := board.CreateTask(&task, mutation); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"custom-ready", "custom-active"} {
		if err := board.MoveTask(task.ID, stage, mutation); err != nil {
			t.Fatal(err)
		}
	}
	current := Team{Workflow: w, Members: []Member{{Name: "worker", ID: "worker-id", Session: "session"}}}
	key := BriefDeliveryKey(&board.Tasks[0], &current)
	if key == "" {
		t.Fatal("custom active key empty")
	}
	if err := board.CommentTask(task.ID, "progress", mutation); err != nil {
		t.Fatal(err)
	}
	if BriefDeliveryKey(&board.Tasks[0], &current) != key {
		t.Fatal("comment changed delivery key")
	}
	if err := board.RenameStage("custom-active", "Building", mutation); err != nil {
		t.Fatal(err)
	}
	current.Workflow = board.Workflow
	if BriefDeliveryKey(&board.Tasks[0], &current) != key {
		t.Fatal("rename changed delivery key")
	}
}

func TestWorkflowSaveCASPreservesDefinition(t *testing.T) {
	store, boardStore := poolRuntimeFixture(t, "http://127.0.0.1:1")
	pool, err := OpenPool(t.Context(), store, "pool", boardStore)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := pool.manager(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state := BoardState{Version: 2, Board: TaskBoard{Version: 2}, Policy: SchedulingPolicy{Approval: "off"}}
	if err := boardStore.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	mutation := TaskMutation{Actor: TaskActor{User: true}, Now: time.Now().UTC()}
	w := customWorkflow()
	if err := SaveWorkflowRenamed(t.Context(), manager, boardStore, w, nil, state.Revision, mutation); err != nil {
		t.Fatal(err)
	}
	loaded, err := boardStore.Load(t.Context())
	if err != nil || loaded.Board.Category("custom-active") != "active" {
		t.Fatal(loaded, err)
	}
	if err := SaveWorkflowRenamed(t.Context(), manager, boardStore, DefaultWorkflow(), nil,
		state.Revision, mutation); err == nil {
		t.Fatal("stale board revision accepted")
	}
	configured, _, err := store.Load(t.Context(), "pool")
	if err != nil || configured.Workflow.Category("custom-active") != "active" {
		t.Fatal(configured, err)
	}
	current, err := pool.manager(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current.Team.Members[0].Brief = "concurrent policy update"
	if err := current.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(current.Version)
	if err := SaveWorkflowRenamed(t.Context(), manager, boardStore, DefaultWorkflow(), nil,
		loaded.Revision, mutation); err == nil {
		t.Fatal("stale team revision accepted with current board revision")
	}
	_, version, err := store.Load(t.Context(), "pool")
	if err != nil || !bytes.Equal(version, before) {
		t.Fatal("stale editor overwrote concurrent team change", err)
	}
	after, err := boardStore.Load(t.Context())
	if err != nil || after.Revision != loaded.Revision || after.Board.Category("custom-active") != "active" {
		t.Fatal("stale editor changed board", after, err)
	}
}

func TestReleaseNoticesHaveStableEventIdentity(t *testing.T) {
	current := Team{Members: []Member{{Name: "lead", Role: "master"}, {Name: "release-manager", Role: "worker"}}}
	now := time.Now().UTC()
	task := Task{ID: "task", Title: "release", State: "done", CompletedAt: now}
	first := releaseNotices(&task, &current)
	second := releaseNotices(&task, &current)
	if len(first) != 1 || first[0].To != "release-manager" || first[0].Event == "" || first[0].Event != second[0].Event {
		t.Fatal(first, second)
	}
	task.State, task.ReleaseVersion, task.ShippedAt = "released", "v1", now.Add(time.Minute)
	shipped := releaseNotices(&task, &current)
	if len(shipped) != 1 || shipped[0].To != "lead" || shipped[0].Event == first[0].Event {
		t.Fatal(shipped)
	}
}
