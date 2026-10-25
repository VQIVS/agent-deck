package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestWorkflowEditorCancelAndStableRename(t *testing.T) {
	original := WorkflowDraft{Stages: []WorkflowStageDraft{
		{ID: "accepted", Name: "Done", Color: "#89C9A0", Category: "done"},
		{ID: "shipped", Name: "Released", Color: "#7DB6F5", Category: "shipped"},
	}}
	editor := NewWorkflowEditor(original, "revision")
	WorkflowEditorKey(editor, key("e"), false)
	editor.Fields[0].Value = "Accepted"
	WorkflowEditorKey(editor, key("ctrl+s"), false)
	if editor.Draft.Stages[0].ID != "accepted" || editor.Draft.Stages[0].Name != "Accepted" {
		t.Fatal("rename lost identity")
	}
	WorkflowEditorKey(editor, key("J"), false)
	if editor.Draft.Stages[1].ID != "accepted" || original.Stages[0].Name != "Done" {
		t.Fatal("reorder modified source or identity")
	}
	if WorkflowEditorKey(editor, key("esc"), false) != "cancel" {
		t.Fatal("cancel intent missing")
	}
}

func TestWorkflowEditorPersistsOccupiedRenameAndRejectsStaleSave(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	store := team.Store{Root: t.TempDir()}
	tm := team.Team{
		Version: 1, Name: "editor-test", Repo: t.TempDir(), Base: "main", Server: "http://unused", Capacity: 1,
		Members: []team.Member{{
			Name: "lead", Role: "master", Mode: "shared", Dir: t.TempDir(),
			Capability: "coder", Permissions: team.Preset("coder"),
		}},
	}
	version, err := store.Save(t.Context(), &tm, nil)
	if err != nil {
		t.Fatal(err)
	}
	board := team.BoardStore{Root: filepath.Join(root, "agent-deck", "boards"), Team: tm.Name}
	state := team.BoardState{Version: 2, Board: team.TaskBoard{
		Version: 2, Tasks: []team.Task{{ID: "A", Title: "Plan", State: "backlog"}},
	}}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	m := requestModel("http://unused", false)
	m.teams = &teamUI{Manager: &team.Manager{Team: tm, Store: store, Version: version}}
	m.board.State = state
	m.openWorkflowEditor()
	editor := m.teams.WorkflowEditor
	editor.Draft.Stages[0].Name = "Ideas"
	cmd := m.saveWorkflowEditor(editor)
	if cmd == nil {
		t.Fatal("save command missing", editor.Error)
	}
	result := cmd().(workflowSavedMsg)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	loaded, err := board.Load(t.Context())
	if err != nil || loaded.Board.Tasks[0].State != "Ideas" {
		t.Fatal("occupied rename not persisted", err)
	}
	// Keep original revision snapshot; another save must report the conflict without losing the draft.
	editor.Saving = false
	cmd = m.saveWorkflowEditor(editor)
	if cmd == nil {
		t.Fatal("retry command missing")
	}
	result = cmd().(workflowSavedMsg)
	m.applyWorkflowSaved(&result)
	if result.Err == nil || editor.Error == "" || m.teams.WorkflowEditor != editor {
		t.Fatal("stale save lost error/draft")
	}
}

func TestWorkflowEditorTeamNavigationAndMouse(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w, m.h = 100, 30
	m.teams = &teamUI{Manager: &team.Manager{Team: team.Team{Name: "test"}}}
	m.teams.Manager.Team.Workflow = team.DefaultWorkflow()
	m.teams.Screen = "members"
	m.teamKey(key("W"))
	if m.teams.Screen != "workflow" || m.teams.WorkflowEditor == nil {
		t.Fatal("Team Workflow navigation missing")
	}
	m.workflowEditorView()
	m.workflowEditorClick("workflow-select:4")
	m.workflowEditorClick("workflow-e")
	if m.teams.WorkflowEditor.Form != "edit" || m.teams.WorkflowEditor.Fields[0].Value != "done" {
		t.Fatal("mouse stage edit lost selection")
	}
	m.workflowEditorClick("workflow-field:2")
	if m.teams.WorkflowEditor.Field != 2 {
		t.Fatal("mouse field focus missing")
	}
	m.workflowEditorKey(key("esc"))
	m.workflowEditorKey(key("esc"))
	if m.teams.Screen != "members" || m.teams.WorkflowEditor != nil {
		t.Fatal("cancel did not return to Team")
	}
}

func TestWorkflowEditorTransitionsAndReadonly(t *testing.T) {
	editor := NewWorkflowEditor(WorkflowDraft{Stages: []WorkflowStageDraft{{ID: "released", Name: "Released"}}}, "")
	WorkflowEditorKey(editor, key("t"), false)
	editor.Fields[0].Value, editor.Fields[1].Value = "done, accepted", "release-manager, user"
	WorkflowEditorKey(editor, key("ctrl+s"), false)
	if len(editor.Draft.Transitions) != 1 || len(editor.Draft.Transitions[0].From) != 2 {
		t.Fatal("transition form lost sources")
	}
	WorkflowEditorKey(editor, key("d"), true)
	if len(editor.Draft.Stages) != 1 || WorkflowEditorKey(editor, key("ctrl+s"), true) != "" {
		t.Fatal("read-only editor mutation")
	}
	if WorkflowEditorKey(editor, key("ctrl+s"), false) != "save" {
		t.Fatal("save intent missing")
	}
}

func TestWorkflowEditorSmallTerminalAndEmptyForm(t *testing.T) {
	editor := NewWorkflowEditor(WorkflowDraft{}, "")
	WorkflowEditorKey(editor, key("n"), false)
	WorkflowEditorKey(editor, key("ctrl+s"), false)
	if editor.Error == "" || editor.Form == "" {
		t.Fatal("empty name silently accepted")
	}
	for _, size := range [][2]int{{1, 1}, {20, 4}, {80, 24}} {
		view := RenderWorkflowEditor(editor, size[0], size[1])
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("overflow %v: %q", size, view)
		}
	}
	editor.Error = "Workflow changed; reload before saving"
	if !strings.Contains(RenderWorkflowEditor(editor, 100, 24), "Workflow changed") {
		t.Fatal("concurrency feedback hidden")
	}
}
