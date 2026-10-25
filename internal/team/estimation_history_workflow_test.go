package team

import (
	"strings"
	"testing"
	"time"
)

func TestConfiguredCompletionMetricsNotDuplicatedAtShipment(t *testing.T) {
	definition := DefaultWorkflow()
	for i := range definition.Stages {
		switch definition.Stages[i].Category {
		case "done":
			definition.Stages[i].Name = "Accepted"
		case "shipped":
			definition.Stages[i].Name = "Published"
		}
	}
	board := TaskBoard{Version: 2, Workflow: definition, Tasks: []Task{{
		ID: "T-1", State: "Accepted",
		StartedAt: time.Now().Add(-time.Hour), CompletedAt: time.Now(), Estimate: TaskEstimate{Duration: time.Hour},
	}}}
	store := BoardStore{Root: t.TempDir(), Team: "metrics"}
	if err := RecordEstimationHistory(t.Context(), store, &board); err != nil {
		t.Fatal(err)
	}
	completed := board.Tasks[0].CompletedAt
	board.Tasks[0].State = "Published"
	board.Tasks[0].ReleaseVersion = "v1.0"
	if err := RecordEstimationHistory(t.Context(), store, &board); err != nil {
		t.Fatal(err)
	}
	history, err := loadEstimationHistory(t.Context(), store)
	if err != nil || len(history.Samples) != 1 || !history.Samples[0].CompletedAt.Equal(completed) {
		t.Fatal("shipment duplicated implementation completion", history, err)
	}
}

func TestConfiguredPromptUsesNamedStages(t *testing.T) {
	definition := DefaultWorkflow()
	for i := range definition.Stages {
		if definition.Stages[i].Category == "review" {
			definition.Stages[i].Name = "Quality gate"
		}
	}
	current := Team{Workflow: definition}
	options, err := Options(t.Context(), &current, &Member{Role: "worker"}, "continue")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Quality gate": review`, "task_workflow", "pending_release", "explicit version"} {
		if !strings.Contains(options.System, want) {
			t.Fatalf("missing configured handoff %q", want)
		}
	}
}
