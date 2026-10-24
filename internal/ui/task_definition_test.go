package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestTaskDefinitionFormRoundTrip(t *testing.T) {
	input := "- id: tests\n  text: 'Tests pass: unit and race'\n  required: true\n" +
		"- id: docs\n  text: Update documentation\n  required: false\n"
	definition, err := ParseTaskDefinition(input)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := SerializeTaskDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ParseTaskDefinition(serialized)
	if err != nil || !reflect.DeepEqual(restored, definition) {
		t.Fatalf("definition did not round trip: %+v %v", restored, err)
	}
	for _, invalid := range []string{
		input + "---\n[]", "- id: x\n  text: Valid\n  extra: ignored",
		"- id: x\n  text: First\n- id: x\n  text: Second", "- id: x\n  text: ''",
	} {
		if _, err := ParseTaskDefinition(invalid); err == nil {
			t.Errorf("invalid definition accepted: %s", invalid)
		}
	}
}

func TestTaskDefinitionDetailKeepsRequirementsSeparate(t *testing.T) {
	task := &team.Task{ID: "T-1", Report: &team.TaskReport{Text: "Worker says complete"}}
	definition := team.DefinitionOfDone{
		{ID: "tests", Text: "Tests pass", Required: true},
		{ID: "docs", Text: "Docs updated", Required: false},
	}
	view := TaskEvidenceWithDefinition(task, nil, nil, definition, 100)
	for _, text := range []string{
		"Worker says complete", "Merged definition of done", "tests [required]",
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
