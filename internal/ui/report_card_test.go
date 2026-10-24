package ui

import (
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestReportCardProvenance(t *testing.T) {
	record := &team.ReportRecord{
		Member: "worker", Source: "structured", ReportedDone: true,
		Definition: team.DefinitionOfDone{{ID: "tests", Text: "Run package tests", Required: true}},
		Reported: &team.DoneReport{
			Summary: "Implemented feature", Checklist: []team.ReportItem{{ID: "tests", Complete: true}},
			Tests: []team.ReportTest{{Command: "go test", Outcome: "pass"}},
		},
		Observed: &team.ReportMetadata{Commit: "observed-commit", Dirty: true},
	}
	card := ReportCard(record, true, 160)
	for _, want := range []string{
		"DONE · worker-reported", "[x] Run package tests (worker claim)",
		"Test reported pass: go test", "Independently observed Git:", "observed-commit", "dirty=true",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q: %s", want, card)
		}
	}
	if card := ReportCard(record, false, 160); !strings.HasPrefix(card, "HISTORICAL") {
		t.Fatal("stale binding not labeled", card)
	}
}

func TestInferredReportCardInventsNoChecklist(t *testing.T) {
	record := &team.ReportRecord{
		Source: "inferred", Member: "worker", InferredText: "Finished working",
		ReportedDone: true, Reported: &team.DoneReport{Tests: []team.ReportTest{{Command: "fake", Outcome: "pass"}}},
	}
	card := ReportCard(record, true, 120)
	if !strings.Contains(card, "INFERRED") || strings.Contains(card, "DONE") ||
		strings.Contains(card, "[x]") || strings.Contains(card, "Test reported") {
		t.Fatal("inferred card manufactured outcomes", card)
	}
	if ReportCard(nil, true, 80) != "" || ReportCard(record, true, 0) != "" {
		t.Fatal("empty card handling failed")
	}
}
