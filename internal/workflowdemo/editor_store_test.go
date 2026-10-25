package workflowdemo

import (
	"bytes"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestCoordinatedWorkflowSaveRejectsStaleEditors(t *testing.T) {
	ctx := t.Context()
	configStore := team.Store{Root: t.TempDir()}
	configured := team.Team{
		Version: 1, Name: "editor-fixture", Repo: t.TempDir(), Base: "main", Capacity: 1,
		Server: "http://127.0.0.1:1", Members: []team.Member{{
			Name: "Master", Role: "master", Mode: "shared", Capability: "coder", Permissions: team.Preset("coder"),
		}},
	}
	configured.Members[0].Dir = configured.Repo
	version, err := configStore.Save(ctx, &configured, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := &team.Manager{Store: configStore, Team: configured, Version: version}
	boardStore := team.BoardStore{Root: t.TempDir(), Team: configured.Name}
	state := team.BoardState{Version: 2, Board: team.TaskBoard{Version: 2}}
	if err := boardStore.Save(ctx, &state, 0); err != nil {
		t.Fatal(err)
	}
	mutation := team.TaskMutation{Actor: team.TaskActor{User: true}, Now: time.Now().UTC()}
	if err := team.SaveWorkflow(ctx, manager, boardStore, customWorkflow(), state.Revision, mutation); err != nil {
		t.Fatal(err)
	}
	loaded, err := boardStore.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reopenedTeam, _, err := configStore.Load(ctx, configured.Name)
	if err != nil || !reflect.DeepEqual(reopenedTeam.Workflow, loaded.Board.Workflow) {
		t.Fatalf("team/board definitions diverged after save/reopen: %v", err)
	}
	// Persist an occupied todo stage through the same coordinated editor path.
	task := team.Task{ID: "rename", Title: "Preserve occupied placement", Paths: []string{"rename/**"}}
	if err := loaded.Board.CreateTask(&task, mutation); err != nil {
		t.Fatal(err)
	}
	if err := boardStore.Save(ctx, &loaded, loaded.Revision); err != nil {
		t.Fatal(err)
	}
	renamed := customWorkflow()
	renamed.Stages[0].Name = "Research"
	if err := team.SaveWorkflowRenamed(ctx, manager, boardStore, renamed,
		map[string]string{"Ideas": "Research"}, loaded.Revision, mutation); err != nil {
		t.Fatal(err)
	}
	loaded, err = boardStore.Load(ctx)
	if err != nil || loaded.Board.Tasks[0].State != "Research" || len(loaded.Board.Tasks[0].History) != 1 {
		t.Fatalf("coordinated occupied rename lost placement/history: %v", err)
	}
	reopenedTeam, reopenedVersion, err := configStore.Load(ctx, configured.Name)
	if err != nil || !reflect.DeepEqual(reopenedTeam.Workflow, loaded.Board.Workflow) {
		t.Fatalf("occupied rename diverged persisted definitions: %v", err)
	}
	before, err := os.ReadFile(configStore.Path(configured.Name))
	if err != nil {
		t.Fatal(err)
	}
	changed := renamed.Clone()
	changed.Stages[0].Color = "#112233"
	if err := team.SaveWorkflow(ctx, manager, boardStore, changed, state.Revision, mutation); err == nil {
		t.Fatal("stale board editor overwrote newer definition")
	}
	afterStale, err := os.ReadFile(configStore.Path(configured.Name))
	if err != nil || !bytes.Equal(before, afterStale) {
		t.Fatalf("stale board revision changed team: %v", err)
	}
	// A different editor updates team policy while the first editor retains its
	// old team revision but has the current board revision.
	fresh := &team.Manager{Store: configStore, Team: reopenedTeam, Version: reopenedVersion}
	fresh.Team.Members[0].Brief = "Concurrent policy change"
	if err := fresh.Save(ctx); err != nil {
		t.Fatal(err)
	}
	before, err = os.ReadFile(configStore.Path(configured.Name))
	if err != nil {
		t.Fatal(err)
	}
	if err := team.SaveWorkflow(ctx, manager, boardStore, changed, loaded.Revision, mutation); err == nil {
		t.Fatal("stale team editor overwrote concurrent policy")
	}
	after, err := os.ReadFile(configStore.Path(configured.Name))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("stale save changed persisted team: %v", err)
	}
	afterBoard, err := boardStore.Load(ctx)
	if err != nil || !reflect.DeepEqual(loaded, afterBoard) {
		t.Fatalf("stale save changed board: %v", err)
	}
}
