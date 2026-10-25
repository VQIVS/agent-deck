package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

func workflowDraft(definition team.WorkflowDefinition) WorkflowDraft {
	definition = definition.Resolved()
	draft := WorkflowDraft{}
	for _, stage := range definition.Stages {
		draft.Stages = append(draft.Stages, WorkflowStageDraft{
			ID: stage.Name, Name: stage.Name, Color: stage.Color, Category: stage.Category,
		})
	}
	keys := make([]string, 0, len(definition.Transitions))
	for key := range definition.Transitions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		rule := definition.Transitions[key]
		draft.Transitions = append(draft.Transitions, WorkflowTransitionDraft{To: key, From: rule.From, By: rule.By})
	}
	return draft
}

func workflowDefinition(draft WorkflowDraft) team.WorkflowDefinition {
	definition := team.WorkflowDefinition{Transitions: map[string]team.WorkflowTransition{}}
	for _, stage := range draft.Stages {
		definition.Stages = append(definition.Stages, team.WorkflowStage{
			Name: stage.Name, Color: stage.Color, Category: stage.Category,
		})
	}
	for _, rule := range draft.Transitions {
		definition.Transitions[rule.To] = team.WorkflowTransition{From: rule.From, By: rule.By}
	}
	return definition
}

func (m *Model) openWorkflowEditor() {
	if m.teams == nil || m.teams.Manager == nil {
		return
	}
	definition := m.teams.Manager.Team.Workflow.Resolved()
	baseline, _ := json.Marshal(definition)
	m.teams.WorkflowEditor = NewWorkflowEditor(workflowDraft(definition), string(baseline))
	editor := m.teams.WorkflowEditor
	editor.Revision = m.board.State.Revision
	manager := *m.teams.Manager
	manager.Team.Workflow = manager.Team.Workflow.Clone()
	manager.Version = slices.Clone(manager.Version)
	editor.Manager = &manager
	m.teams.Screen = "workflow"
}

func (m *Model) workflowEditorKey(key tea.KeyMsg) tea.Cmd {
	editor := m.teams.WorkflowEditor
	if editor == nil {
		m.teams.Screen = "members"
		return nil
	}
	intent := WorkflowEditorKey(editor, key, m.opts.ReadOnly)
	switch intent {
	case "cancel":
		m.teams.WorkflowEditor = nil
		m.teams.Screen = "members"
	case "save":
		if len(editor.Draft.Stages) == 0 {
			editor.Error = "Workflow requires stages; add stages before saving"
			return nil
		}
		if err := workflowDefinition(editor.Draft).Validate(); err != nil {
			editor.Error = err.Error()
			return nil
		}
		return m.saveWorkflowEditor(editor)
	}
	return nil
}

func (m *Model) saveWorkflowEditor(editor *WorkflowEditor) tea.Cmd {
	if m.opts.ReadOnly || editor.Saving || editor.Manager == nil {
		return nil
	}
	root, err := server.StateRoot()
	if err != nil {
		editor.Error = err.Error()
		return nil
	}
	definition := workflowDefinition(editor.Draft)
	renames := map[string]string{}
	for _, stage := range editor.Draft.Stages {
		if stage.ID != "" && stage.ID != stage.Name {
			renames[stage.ID] = stage.Name
		}
	}
	editor.Saving = true
	ctx := m.context()
	manager := *editor.Manager
	store := team.BoardStore{Root: filepath.Join(root, "boards"), Team: manager.Team.Name}
	expected := editor.Revision
	return func() tea.Msg {
		err := team.SaveWorkflowRenamed(ctx, &manager, store, definition, renames, expected,
			team.TaskMutation{Actor: team.TaskActor{User: true}, Now: time.Now().UTC()})
		state, loadErr := store.Load(ctx)
		if err == nil {
			err = loadErr
		}
		return workflowSavedMsg{Manager: &manager, State: state, Err: err}
	}
}

type workflowSavedMsg struct {
	Manager *team.Manager
	State   team.BoardState
	Err     error
}

func (m *Model) applyWorkflowSaved(result *workflowSavedMsg) tea.Cmd {
	if m.teams == nil || m.teams.WorkflowEditor == nil {
		return nil
	}
	editor := m.teams.WorkflowEditor
	editor.Saving = false
	if result.Err != nil {
		editor.Error = result.Err.Error()
		m.note = editor.Error
		return nil
	}
	// A poll may have observed newer tasks while the save command was in flight.
	if result.State.Revision >= m.board.State.Revision {
		m.board.State = result.State
	}
	m.teams.Manager = result.Manager
	m.teams.WorkflowEditor = nil
	m.teams.Screen = "members"
	m.note = "Workflow saved"
	return m.readBoard()
}

func (m *Model) workflowEditorView() string {
	editor := m.teams.WorkflowEditor
	if editor == nil {
		return "Workflow not loaded"
	}
	m.zones = nil
	body := RenderWorkflowEditor(editor, m.w, max(1, m.h-1))
	buttons := []button{
		{"Save", "workflow-save", m.opts.ReadOnly || editor.Saving}, {"Cancel", "workflow-cancel", editor.Saving},
	}
	if editor.Form == "" {
		buttons = append(buttons, button{"Add", "workflow-n", m.opts.ReadOnly}, button{"Edit", "workflow-e", m.opts.ReadOnly},
			button{"Transitions", "workflow-t", m.opts.ReadOnly}, button{"Up", "workflow-K", m.opts.ReadOnly},
			button{"Down", "workflow-J", m.opts.ReadOnly}, button{"Remove", "workflow-d", m.opts.ReadOnly})
		start := max(0, editor.Index-max(1, m.h-6)+1)
		for i := start; i < len(editor.Draft.Stages) && i < start+max(1, m.h-6); i++ {
			m.zones = append(m.zones, zone{
				Rect: Rect{X: 0, Y: 2 + i - start, W: m.w, H: 1}, Action: fmt.Sprintf("workflow-select:%d", i), Pane: -1,
			})
		}
	} else {
		for i := range editor.Fields {
			m.zones = append(m.zones, zone{
				Rect: Rect{X: 0, Y: i + 2, W: m.w, H: 1}, Action: fmt.Sprintf("workflow-field:%d", i), Pane: -1,
			})
		}
	}
	return body + "\n" + m.buttonRow(buttons, 0, min(strings.Count(body, "\n")+1, m.h-1), m.w, -1)
}

func (m *Model) workflowEditorClick(action string) tea.Cmd {
	if m.teams == nil || m.teams.WorkflowEditor == nil {
		return nil
	}
	if strings.HasPrefix(action, "workflow-field:") {
		var index int
		_, err := fmt.Sscanf(action, "workflow-field:%d", &index)
		if err == nil && index >= 0 && index < len(m.teams.WorkflowEditor.Fields) {
			m.teams.WorkflowEditor.Field = index
		}
		return nil
	}
	if strings.HasPrefix(action, "workflow-select:") {
		var index int
		_, err := fmt.Sscanf(action, "workflow-select:%d", &index)
		if err == nil && index >= 0 && index < len(m.teams.WorkflowEditor.Draft.Stages) {
			m.teams.WorkflowEditor.Index = index
		}
		return nil
	}
	value := strings.TrimPrefix(action, "workflow-")
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	if value == "save" {
		key.Type = tea.KeyCtrlS
		key.Runes = nil
	}
	if value == "cancel" {
		key.Type = tea.KeyEsc
		key.Runes = nil
	}
	return m.workflowEditorKey(key)
}
