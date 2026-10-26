package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// FormStep describes one named step and its contiguous range of form fields.
type FormStep struct {
	Name  string
	First int
	Count int
}

// FormStepField is already-rendered input with inline validation and optional guidance.
type FormStepField struct {
	Label string
	Input string
	Hint  string
	Error string
}

// FormStepView is a pure rendering snapshot; Focus is a global field index.
type FormStepView struct {
	Title    string
	Steps    []FormStep
	Fields   []FormStepField
	Focus    int
	Width    int
	ReadOnly bool
}

func FormStepIndex(steps []FormStep, focus int) int {
	for i, step := range steps {
		if focus >= step.First && focus < step.First+step.Count {
			return i
		}
	}
	return -1
}

func (t *Theme) RenderFormStep(view *FormStepView) string {
	if view == nil || len(view.Steps) == 0 {
		return ""
	}
	index := FormStepIndex(view.Steps, view.Focus)
	if index < 0 {
		index = 0
	}
	step := &view.Steps[index]
	width := max(1, view.Width)
	lines := []string{t.title(view.Title), t.dim(fmt.Sprintf("Step %d/%d · %s", index+1, len(view.Steps), step.Name))}
	for i := max(0, step.First); i < min(len(view.Fields), step.First+step.Count); i++ {
		field := &view.Fields[i]
		label := field.Label
		if i == view.Focus {
			label = "> " + label
		} else {
			label = "  " + label
		}
		lines = append(lines, "", t.title(label), field.Input)
		if field.Error != "" {
			lines = append(lines, lipgloss.NewStyle().Foreground(t.Danger).Render("  Error: "+field.Error))
		} else if field.Hint != "" {
			lines = append(lines, t.dim("  "+field.Hint))
		}
	}
	lines = append(lines, "", t.dim(formStepKeys(index == len(view.Steps)-1, view.ReadOnly)))
	return fitFormStep(strings.Join(lines, "\n"), width)
}

func formStepKeys(last, readOnly bool) string {
	if readOnly {
		return "Read-only · Esc close"
	}
	enter := "Enter next"
	if last {
		enter = "Enter confirm"
	}
	return enter + " · Ctrl+J newline · Ctrl+S save · Shift+Tab back · Esc cancel"
}

func fitFormStep(text string, width int) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return strings.Join(lines, "\n")
}

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
