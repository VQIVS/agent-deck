package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFormStepIndex(t *testing.T) {
	steps := []FormStep{{Name: "Identity", First: 0, Count: 2}, {Name: "Policy", First: 2, Count: 2}}
	for focus, expected := range map[int]int{-1: -1, 0: 0, 1: 0, 2: 1, 3: 1, 4: -1} {
		if got := FormStepIndex(steps, focus); got != expected {
			t.Fatalf("focus %d: got %d, want %d", focus, got, expected)
		}
	}
}

func TestRenderFormStepValidationAndPurity(t *testing.T) {
	theme, err := ResolveTheme("dark")
	if err != nil {
		t.Fatal(err)
	}
	view := &FormStepView{
		Title: "Add worker", Width: 160, Focus: 2,
		Steps: []FormStep{{Name: "Identity", First: 0, Count: 2}, {Name: "Policy", First: 2, Count: 1}},
		Fields: []FormStepField{
			{Label: "Name", Input: "hidden identity"},
			{Label: "Model", Input: "hidden model"},
			{Label: "Leases", Input: "internal/ui/**", Hint: "hidden hint", Error: "overlapping lease"},
		},
	}
	before := *view
	before.Fields = append([]FormStepField(nil), view.Fields...)
	before.Steps = append([]FormStep(nil), view.Steps...)
	text := ansi.Strip(theme.RenderFormStep(view))
	for _, expected := range []string{
		"Add worker", "Step 2/2 · Policy", "> Leases", "Error: overlapping lease",
		"Enter confirm", "Ctrl+J newline", "Ctrl+S save",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %q", expected, text)
		}
	}
	for _, hidden := range []string{"hidden identity", "hidden model", "hidden hint"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("other step or superseded hint visible: %q", hidden)
		}
	}
	if !reflect.DeepEqual(before, *view) {
		t.Fatal("render mutated form snapshot")
	}
}

func TestRenderFormStepWidthsAndReadOnly(t *testing.T) {
	for _, mode := range []string{"dark", "light"} {
		theme, err := ResolveTheme(mode)
		if err != nil {
			t.Fatal(err)
		}
		for _, width := range []int{1, 20, 80, 160} {
			view := &FormStepView{
				Title: "Worker 界界 configuration", Width: width, Focus: 0, ReadOnly: true,
				Steps:  []FormStep{{Name: "Identity", Count: 1}},
				Fields: []FormStepField{{Label: "Name", Input: "a long worker name", Hint: "short guidance"}},
			}
			text := theme.RenderFormStep(view)
			for _, line := range strings.Split(text, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("width %d overflow: %q", width, line)
				}
			}
			if width >= 80 && !strings.Contains(ansi.Strip(text), "Read-only · Esc close") {
				t.Fatal("missing read-only hint")
			}
			if strings.Contains(ansi.Strip(text), "Ctrl+S save") {
				t.Fatal("read-only form advertises save")
			}
		}
	}
	theme, _ := ResolveTheme("dark")
	if theme.RenderFormStep(nil) != "" || theme.RenderFormStep(&FormStepView{}) != "" {
		t.Fatal("empty form rendered content")
	}
}
