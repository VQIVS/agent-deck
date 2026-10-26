package ui

import (
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestTaskDefinitionDetailKeepsRequirementsSeparate(t *testing.T) {
	definition := team.DefinitionOfDone{
		{ID: "tests", Text: "Tests pass", Required: true},
		{ID: "docs", Text: "Docs updated", Required: false},
	}
	view := TaskDefinitionDetail(definition, 100)
	for _, text := range []string{
		"Merged definition of done", "tests [required]",
		"docs [optional]", "not evidence of completion",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing separate requirement/provenance %q: %s", text, view)
		}
	}
	if TaskDefinitionDetail(nil, 100) != "" || TaskDefinitionDetail(definition, 0) != "" {
		t.Fatal("absent definition should not render")
	}
}
