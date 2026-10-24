package team

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/server"
)

func TestEstimationHistoryOriginalReloadMissingUsageAndDuplicate(t *testing.T) {
	store := BoardStore{Root: t.TempDir(), Team: "history"}
	now := time.Now().UTC()
	board := TaskBoard{Version: 2, Tasks: []Task{{ID: "T-1", State: "ready", Estimate: TaskEstimate{Duration: time.Hour}}}}
	if err := RecordEstimationHistory(t.Context(), store, &board); err != nil {
		t.Fatal(err)
	}
	task := &board.Tasks[0]
	task.State, task.StartedAt, task.CompletedAt = "done", now, now.Add(30*time.Minute)
	task.Estimate.Duration = 2 * time.Hour
	for range 2 {
		if err := RecordEstimationHistory(t.Context(), store, &board); err != nil {
			t.Fatal(err)
		}
	}
	history, err := loadEstimationHistory(t.Context(), store)
	if err != nil || len(history.Samples) != 1 {
		t.Fatal(history, err)
	}
	sample := history.Samples[0]
	if sample.Estimate.Duration != time.Hour || sample.Elapsed == nil || *sample.Elapsed != 30*time.Minute {
		t.Fatal("original or elapsed lost", sample)
	}
	if sample.Tokens != nil || sample.Cost != nil || sample.UsageSource != "unavailable" {
		t.Fatal("invented usage", sample)
	}
	if !strings.Contains(EstimationPlanningSummary(t.Context(), store), "tokens/cost unavailable") {
		t.Fatal("coverage omitted")
	}
}

func TestEstimationLegacyAndFailureNotice(t *testing.T) {
	store := BoardStore{Root: t.TempDir(), Team: "legacy-history"}
	board := TaskBoard{Version: 1, Tasks: []Task{{ID: "old", State: "done"}}}
	if err := RecordEstimationHistory(t.Context(), store, &board); err != nil {
		t.Fatal(err)
	}
	history, err := loadEstimationHistory(t.Context(), store)
	if err != nil || len(history.Samples) != 1 || history.Samples[0].Elapsed != nil {
		t.Fatal("legacy unavailable elapsed misrepresented", history, err)
	}
	if !strings.Contains(history.Samples[0].EstimateSource, "original unavailable") {
		t.Fatal("legacy original invented")
	}
	directory, err := store.directory()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "estimation-history.json"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	team := Team{Members: []Member{{Name: "lead", Role: "master"}}}
	notices := EstimationHistoryNotices(t.Context(), store, &board, &team)
	if len(notices) != 1 || notices[0].To != "lead" || !strings.Contains(notices[0].Text, "Task change saved") {
		t.Fatal("history failure not observable", notices)
	}
}

func TestEstimationMasterPromptReadOnlyCoverage(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root, err := server.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	store := BoardStore{Root: filepath.Join(root, "boards"), Team: "sizing"}
	now := time.Now().UTC()
	board := TaskBoard{Version: 2, Tasks: []Task{{
		ID: "T-1", State: "done", StartedAt: now, CompletedAt: now.Add(time.Minute),
	}}}
	if err := RecordEstimationHistory(t.Context(), store, &board); err != nil {
		t.Fatal(err)
	}
	team := Team{Name: "sizing"}
	options, err := Options(t.Context(), &team, &Member{Role: "master"}, "plan")
	if err != nil || !strings.Contains(options.System, "Team sizing history") {
		t.Fatal("master history missing", err)
	}
	options, err = Options(t.Context(), &team, &Member{Role: "worker"}, "work")
	if err != nil || strings.Contains(options.System, "Team sizing history") {
		t.Fatal("worker prompt polluted", err)
	}
}

func TestEstimationHistoryBoundedReadOnlyAndMeasured(t *testing.T) {
	store := BoardStore{Root: t.TempDir(), Team: "history"}
	now := time.Now().UTC()
	board := TaskBoard{Version: 2}
	for i := range 110 {
		board.Tasks = append(board.Tasks, Task{
			ID: fmt.Sprintf("T-%d", i), State: "done", StartedAt: now,
			CompletedAt: now.Add(time.Duration(i+1) * time.Minute),
			Measured:    &TaskUsage{Tokens: 10, Cost: 0.1, Source: "server"},
		})
	}
	if err := RecordEstimationHistory(t.Context(), store, &board); err != nil {
		t.Fatal(err)
	}
	history, err := loadEstimationHistory(t.Context(), store)
	if err != nil || len(history.Samples) != 100 {
		t.Fatal(len(history.Samples), err)
	}
	if history.Samples[0].Tokens == nil || history.Samples[0].Cost == nil {
		t.Fatal("measured usage lost")
	}
	summary := EstimationPlanningSummary(t.Context(), store)
	if len(summary) > 1500 || strings.Count(summary, "\nT-") != 5 {
		t.Fatal("unbounded summary", summary)
	}
	directory, err := store.directory()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "estimation-history.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store.ReadOnly = true
	board.Tasks = nil
	if err := RecordEstimationHistory(t.Context(), store, &board); err != nil {
		t.Fatal(err)
	}
	EstimationPlanningSummary(t.Context(), store)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only load changed history", err)
	}
}
