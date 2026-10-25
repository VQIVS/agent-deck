package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/team"
)

// WorkflowStageDraft keeps editable presentation separate from stable stage identity.
// Only the core workflow adapter validates execution semantics or persists changes.
type WorkflowStageDraft struct{ ID, Name, Color, Category string }

// WorkflowTransitionDraft edits explicit source and actor constraints for a destination.
type WorkflowTransitionDraft struct {
	To       string
	From, By []string
}

// WorkflowDraft is an isolated editable snapshot, never the live engine definition.
type WorkflowDraft struct {
	Stages      []WorkflowStageDraft
	Transitions []WorkflowTransitionDraft
}

// WorkflowEditor holds unsaved stage/transition edits and revision feedback.
type WorkflowEditor struct {
	Draft    WorkflowDraft
	Index    int
	Form     string
	Fields   []formField
	Field    int
	Error    string
	Saving   bool
	Baseline string
	Revision uint64
	Manager  *team.Manager
}

func NewWorkflowEditor(draft WorkflowDraft, baseline string) *WorkflowEditor {
	cloned := WorkflowDraft{Stages: slices.Clone(draft.Stages)}
	for _, rule := range draft.Transitions {
		cloned.Transitions = append(cloned.Transitions, WorkflowTransitionDraft{
			To: rule.To, From: slices.Clone(rule.From), By: slices.Clone(rule.By),
		})
	}
	return &WorkflowEditor{Draft: cloned, Baseline: baseline}
}

// WorkflowEditorKey returns save/cancel intents; mutations remain integration-owned.
func WorkflowEditorKey(editor *WorkflowEditor, key tea.KeyMsg, readOnly bool) string {
	if editor.Saving {
		return ""
	}
	if editor.Form != "" {
		return workflowFormKey(editor, key, readOnly)
	}
	if key.String() == "esc" {
		return "cancel"
	}
	if readOnly {
		return ""
	}
	return workflowListKey(editor, key.String())
}

func workflowListKey(e *WorkflowEditor, key string) string {
	e.Error = ""
	switch key {
	case "ctrl+s":
		return "save"
	case "up", "k":
		e.Index = max(0, e.Index-1)
	case "down", "j":
		e.Index = min(max(0, len(e.Draft.Stages)-1), e.Index+1)
	case "n":
		e.Form = "add"
		e.Fields = []formField{{Label: "Name"}, {Label: "Color", Value: "#7DB6F5"}, {Label: "Category", Value: "todo"}}
		e.Field = 0
	case "e", "t", "d", "J", "K":
		workflowStageAction(e, key)
	}
	return ""
}

func workflowStageAction(e *WorkflowEditor, action string) {
	if e.Index < 0 || e.Index >= len(e.Draft.Stages) {
		return
	}
	stage := e.Draft.Stages[e.Index]
	switch action {
	case "e":
		e.Form = "edit"
		e.Fields = []formField{
			{Label: "Name", Value: stage.Name},
			{Label: "Color", Value: stage.Color},
			{Label: "Category", Value: stage.Category},
		}
		e.Field = 0
	case "t":
		e.Form = "transition"
		e.Fields = []formField{{Label: "From stages (comma separated)"}, {Label: "By members/user (comma separated)"}}
		for _, rule := range e.Draft.Transitions {
			if rule.To == stage.Name {
				e.Fields[0].Value, e.Fields[1].Value = strings.Join(rule.From, ", "), strings.Join(rule.By, ", ")
			}
		}
		e.Field = 0
	case "d":
		e.Draft.Stages = slices.Delete(e.Draft.Stages, e.Index, e.Index+1)
		e.Index = min(e.Index, max(0, len(e.Draft.Stages)-1))
	case "J", "K":
		delta := 1
		if action == "K" {
			delta = -1
		}
		other := max(0, min(len(e.Draft.Stages)-1, e.Index+delta))
		e.Draft.Stages[e.Index], e.Draft.Stages[other] = e.Draft.Stages[other], e.Draft.Stages[e.Index]
		e.Index = other
	}
}

func workflowFormKey(e *WorkflowEditor, key tea.KeyMsg, readOnly bool) string {
	switch key.String() {
	case "esc":
		e.Form = ""
		e.Fields = nil
		e.Error = ""
	case "tab":
		e.Field = (e.Field + 1) % len(e.Fields)
	case "shift+tab":
		e.Field = (e.Field + len(e.Fields) - 1) % len(e.Fields)
	case "ctrl+s":
		if !readOnly {
			workflowApplyForm(e)
		}
	default:
		if !readOnly {
			editInboxText(&e.Fields[e.Field].Value, key)
		}
	}
	return ""
}

func workflowApplyForm(e *WorkflowEditor) {
	if e.Form == "transition" {
		id := e.Draft.Stages[e.Index].Name
		rule := WorkflowTransitionDraft{To: id, From: workflowCSV(e.Fields[0].Value), By: workflowCSV(e.Fields[1].Value)}
		e.Draft.Transitions = slices.DeleteFunc(e.Draft.Transitions, func(r WorkflowTransitionDraft) bool {
			return r.To == id
		})
		if len(rule.From) > 0 || len(rule.By) > 0 {
			e.Draft.Transitions = append(e.Draft.Transitions, rule)
		}
	} else {
		stage := WorkflowStageDraft{
			Name: strings.TrimSpace(e.Fields[0].Value), Color: strings.TrimSpace(e.Fields[1].Value),
			Category: strings.TrimSpace(e.Fields[2].Value),
		}
		if stage.Name == "" {
			e.Error = "Stage name is required"
			return
		}
		if e.Form == "edit" {
			stage.ID = e.Draft.Stages[e.Index].ID
			workflowRenameReferences(e, e.Draft.Stages[e.Index].Name, stage.Name)
			e.Draft.Stages[e.Index] = stage
		} else {
			// Empty identity marks a newly added stage, never an existing rename.
			stage.ID = ""
			e.Draft.Stages = append(e.Draft.Stages, stage)
			e.Index = len(e.Draft.Stages) - 1
		}
	}
	e.Form = ""
	e.Fields = nil
	e.Error = ""
}

func workflowCSV(text string) []string {
	var values []string
	for _, value := range strings.Split(text, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func workflowRenameReferences(e *WorkflowEditor, oldName, newName string) {
	for i := range e.Draft.Transitions {
		rule := &e.Draft.Transitions[i]
		if rule.To == oldName {
			rule.To = newName
		}
		for j := range rule.From {
			if rule.From[j] == oldName {
				rule.From[j] = newName
			}
		}
	}
}

func RenderWorkflowEditor(e *WorkflowEditor, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := []string{"Team → Workflow · Ctrl+S save · Esc cancel"}
	if e.Form != "" {
		lines = append(lines, "Stage "+e.Form+" · Tab field · Ctrl+S apply · Esc back")
		for i, field := range e.Fields {
			prefix := "  "
			if i == e.Field {
				prefix = "> "
			}
			lines = append(lines, prefix+field.Label+": "+field.Value)
		}
	} else {
		lines = append(lines, "j/k select · n add · e edit · J/K reorder · d remove · t transitions")
		capacity := max(1, height-5)
		start := max(0, e.Index-capacity+1)
		for i := start; i < len(e.Draft.Stages) && i < start+capacity; i++ {
			s := e.Draft.Stages[i]
			prefix := "  "
			if i == e.Index {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s%s [%s] %s · id:%s", prefix, s.Name, s.Category, s.Color, s.ID))
		}
		if len(e.Draft.Stages) == 0 {
			lines = append(lines, "No stages · n adds the first stage")
		}
	}
	// Keep the core category's British spelling in user-visible schema guidance.
	lines = append(lines, "Categories: todo, ready, active, review, done, shipped, blocked, cancelled") //nolint:misspell
	if e.Saving {
		lines = append(lines, "Saving…")
	} else {
		lines = append(lines, e.Error)
	}
	return kanbanBound(strings.Join(lines, "\n"), width, height)
}
