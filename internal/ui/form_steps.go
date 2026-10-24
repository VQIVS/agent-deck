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
