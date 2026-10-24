package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) renderFormSteps() string {
	u := m.teams
	if u == nil || len(u.Fields) == 0 {
		return m.theme.dim("No form fields available")
	}
	count := min(len(u.Fields), u.fieldCount())
	if count == 0 {
		return m.theme.dim("No form fields available")
	}
	focus := max(0, min(u.Field, count-1))
	fields := make([]FormStepField, count)
	steps := make([]FormStep, count)
	for i := range fields {
		field := &u.Fields[i]
		fields[i] = FormStepField{Label: field.Label, Input: field.Value}
		steps[i] = FormStep{Name: field.Label, First: i, Count: 1}
	}
	fields[focus].Input = m.input.View()
	fields[focus].Hint = formStepFieldHint(u.Fields[focus].Label)
	if m.note != "" {
		fields[focus].Hint = strings.TrimSpace(fields[focus].Hint + " " + m.note)
	}
	if u.Screen == "member-form" {
		fields[focus].Hint = "Permissions apply only to newly created sessions. " + fields[focus].Hint
	}
	body := m.theme.RenderFormStep(&FormStepView{
		Title: "Edit " + strings.TrimSuffix(u.Screen, "-form"), Steps: steps, Fields: fields,
		Focus: focus, Width: max(1, m.w), ReadOnly: m.opts.ReadOnly,
	})
	inputHeight := lipgloss.Height(fields[focus].Input)
	if !m.opts.ReadOnly {
		m.zones = append(m.zones, zone{Rect: Rect{0, 4, max(1, m.w), inputHeight}, Action: "input", Pane: -1})
	}
	buttons := []button{
		{"Previous", "form-prev", focus == 0},
		{"Next", "form-next", focus == count-1},
		{m.formSaveLabel(), "form-save", m.opts.ReadOnly},
		{"Cancel", "form-cancel", false},
	}
	if len(u.Fields[focus].Choices) != 0 {
		buttons = append(buttons, button{"Choose / preset", "form-choice", m.opts.ReadOnly})
	}
	y := lipgloss.Height(body)
	return body + "\n" + m.buttonRow(buttons, 0, y, max(1, m.w), -1)
}

func formStepFieldHint(label string) string {
	switch label {
	case "Existing session ID (adopt)":
		return "Leave empty to create and save a new session."
	case "Model", "Default model (provider/model)":
		return "Use provider/model; leave empty to use the default."
	case "Permission preset":
		return "Permissions apply to newly created sessions."
	default:
		return ""
	}
}
