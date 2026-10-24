package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/qivs/agent-deck/internal/config"
)

func TestFormStepsAdapterInputAndButtonZones(t *testing.T) {
	m := New(t.Context(), &config.Deck{}, Options{NoGit: true})
	m.EnableTeams(t.TempDir())
	m.teamAction("team-new")
	m.w, m.h = 160, 48
	m.input.SetValue("focused value")
	m.zones = nil
	text := ansi.Strip(m.renderFormSteps())
	if !strings.Contains(text, "Team name") || !strings.Contains(text, "focused value") {
		t.Fatal("adapter lost label or current input", text)
	}
	lines := strings.Split(text, "\n")
	for _, target := range m.zones {
		if target.Action == "input" {
			if target.Y != 4 || !strings.Contains(lines[target.Y], "focused value") {
				t.Fatal("input zone does not match rendered input", target)
			}
		}
		if target.Action == "form-save" && !strings.Contains(lines[target.Y], m.formSaveLabel()) {
			t.Fatal("save zone does not match button row", target)
		}
	}
	m.opts.ReadOnly = true
	m.zones = nil
	m.renderFormSteps()
	for _, target := range m.zones {
		if target.Action == "input" || target.Action == "form-save" {
			t.Fatal("read-only adapter exposed mutation zone", target)
		}
	}
}
