package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestKanbanCustomWorkflowOrderAndMoves(t *testing.T) {
	definition := team.DefaultWorkflow()
	definition.Stages[0].Name = "Ideas"
	definition.Stages[1].Name = "Queued"
	definition.Stages[4].Name = "Accepted"
	definition.Stages[5].Name = "Published"
	definition.Transitions = map[string]team.WorkflowTransition{
		"Published": {From: []string{"Accepted"}, By: []string{"user"}},
	}
	screen := &KanbanScreen{Workflow: definition, Column: "Ideas"}
	tasks := []team.Task{{ID: "T-1", Title: "Custom card", State: "Ideas"}}
	theme, _ := ResolveTheme("dark")
	view, _ := RenderKanban(tasks, screen, &KanbanOptions{Workflow: definition, Width: 200, Height: 24, Theme: theme})
	for _, name := range []string{"Ideas", "Queued", "Accepted", "Published"} {
		if !strings.Contains(view, name) {
			t.Fatalf("missing custom stage %q: %s", name, view)
		}
	}
	KanbanKey(tasks, screen, key("m"), false)
	for range 5 {
		KanbanKey(tasks, screen, key("j"), false)
	}
	intent := KanbanKey(tasks, screen, key("enter"), false)
	if intent == nil || intent.State != "Published" || intent.TaskID != "T-1" {
		t.Fatalf("bad move %+v", intent)
	}
	if lipgloss.Width(view) > 200 {
		t.Fatal("custom board overflow")
	}
}

func TestKanbanReleaseFormRequiresVersion(t *testing.T) {
	form := NewKanbanForm("release", &team.Task{ID: "T-1", State: "done"}, 1)
	form.TargetStage = "released"
	if _, err := form.Build(); err == nil {
		t.Fatal("missing release version accepted")
	}
	form.Fields[0].Value = "v0.4.0"
	edit, err := form.Build()
	if err != nil || edit.Intent.Action != "move" || edit.Intent.State != "released" || edit.Release.Version != "v0.4.0" {
		t.Fatalf("release form %+v %v", edit, err)
	}
}

func TestCustomWorkflowGolden(t *testing.T) {
	definition := team.DefaultWorkflow()
	definition.Stages[0].Name = "Ideas"
	definition.Stages[4].Name = "Accepted"
	definition.Stages[5].Name = "Published"
	definition.Transitions = map[string]team.WorkflowTransition{
		"Published": {From: []string{"Accepted"}, By: []string{"user"}},
	}
	theme, _ := ResolveTheme("dark")
	tasks := []team.Task{
		{ID: "C-1", Title: "Accepted implementation", State: "Accepted"},
		{ID: "C-2", Title: "Delivered version", State: "Published", ReleaseVersion: "v0.4.0"},
	}
	screen := &KanbanScreen{Selected: "C-1", Workflow: definition}
	view, _ := RenderKanban(tasks, screen, &KanbanOptions{Workflow: definition, Width: 160, Height: 24, Theme: theme})
	assertScreenGolden(t, "kanban_custom_160x24", view)
}

func TestKanbanReleaseKeepsCoreEvidenceGuards(t *testing.T) {
	executor, state, now := kanbanExecutorFixture(t)
	edit := &KanbanEdit{
		Intent: KanbanIntent{Action: "move", TaskID: "A", State: "released"}, Revision: state.Revision,
		Release: &team.TaskRelease{Version: "v0.4.0"},
	}
	if _, err := executor.Apply(t.Context(), edit, now); err == nil {
		t.Fatal("UI bypassed accepted completion requirement")
	}
	loaded, err := executor.Store.Load(t.Context())
	if err != nil || loaded.Board.Tasks[0].State != "ready" || loaded.Board.Tasks[0].ReleaseVersion != "" {
		t.Fatal("denied shipment mutated board", err)
	}
}
