package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const estimationHistoryLimit = 100

// EstimationSample separates planner predictions from observed coverage. Nil usage
// means unavailable, never a zero-cost or zero-token claim.
type EstimationSample struct {
	Key            string         `json:"key"`
	Task           string         `json:"task"`
	Estimate       TaskEstimate   `json:"estimate"`
	EstimateSource string         `json:"estimate_source"`
	CompletedAt    time.Time      `json:"completed_at"`
	Elapsed        *time.Duration `json:"elapsed_ns"`
	Tokens         *int64         `json:"tokens"`
	Cost           *float64       `json:"cost"`
	UsageSource    string         `json:"usage_source"`
}

type estimationHistory struct {
	Version  int                     `json:"version"`
	Samples  []EstimationSample      `json:"samples"`
	Original map[string]TaskEstimate `json:"original,omitempty"`
}

func loadEstimationHistory(ctx context.Context, store BoardStore) (estimationHistory, error) {
	history := estimationHistory{Version: 1, Original: map[string]TaskEstimate{}}
	err := store.read(ctx, "estimation-history.json", &history)
	if errors.Is(err, os.ErrNotExist) {
		return history, nil
	}
	if err != nil {
		return history, fmt.Errorf("load estimation history: %w", err)
	}
	if history.Original == nil {
		history.Original = map[string]TaskEstimate{}
	}
	return history, nil
}

// RecordEstimationHistory snapshots original estimates on first observation and
// captures done tasks idempotently. Call after durable board mutations; loads never write.
func RecordEstimationHistory(ctx context.Context, store BoardStore, board *TaskBoard) error {
	if store.ReadOnly {
		return nil
	}
	return store.Exclusive(ctx, func() error {
		history, err := loadEstimationHistory(ctx, store)
		if err != nil {
			return err
		}
		updateEstimationHistory(&history, board)
		return store.write(ctx, "estimation-history.json", &history)
	})
}

func updateEstimationHistory(history *estimationHistory, board *TaskBoard) {
	seen := map[string]bool{}
	for i := range history.Samples {
		seen[history.Samples[i].Key] = true
	}
	original := map[string]TaskEstimate{}
	for i := range board.Tasks {
		task := &board.Tasks[i]
		estimate, exists := history.Original[task.ID]
		if !exists {
			estimate = task.Estimate
		}
		category := board.Category(task.State)
		if category != "done" && category != "shipped" {
			if len(original) < 1000 {
				original[task.ID] = estimate
			}
			continue
		}
		key := task.ID + ":" + task.CompletedAt.UTC().Format(time.RFC3339Nano)
		if seen[key] {
			continue
		}
		sample := estimationSample(task, estimate)
		sample.Key = key
		if exists {
			sample.EstimateSource = "first observed estimate"
		}
		history.Samples = append(history.Samples, sample)
		seen[key] = true
	}
	history.Original = original
	sort.Slice(history.Samples, func(i, j int) bool {
		return history.Samples[i].CompletedAt.Before(history.Samples[j].CompletedAt)
	})
	if len(history.Samples) > estimationHistoryLimit {
		history.Samples = history.Samples[len(history.Samples)-estimationHistoryLimit:]
	}
}

func estimationSample(task *Task, estimate TaskEstimate) EstimationSample {
	s := EstimationSample{
		Task: task.ID, Estimate: estimate, EstimateSource: "completion snapshot; original unavailable",
		CompletedAt: task.CompletedAt, UsageSource: "unavailable",
	}
	if !task.StartedAt.IsZero() && !task.CompletedAt.Before(task.StartedAt) {
		elapsed := task.CompletedAt.Sub(task.StartedAt)
		s.Elapsed = &elapsed
	}
	if task.Measured != nil && task.Measured.Source != "" {
		s.UsageSource = task.Measured.Source
		if task.Measured.Tokens > 0 {
			tokens := task.Measured.Tokens
			s.Tokens = &tokens
		}
		if task.Measured.Cost > 0 {
			cost := task.Measured.Cost
			s.Cost = &cost
		}
	}
	return s
}

// EstimationPlanningSummary is bounded, read-only and explicit about coverage.
func EstimationPlanningSummary(ctx context.Context, store BoardStore) string {
	history, err := loadEstimationHistory(ctx, store)
	if err != nil || len(history.Samples) == 0 {
		return ""
	}
	var lines []string
	for i := max(0, len(history.Samples)-5); i < len(history.Samples); i++ {
		sample := &history.Samples[i]
		actual := "elapsed unavailable"
		if sample.Elapsed != nil {
			actual = "elapsed " + sample.Elapsed.Round(time.Second).String()
		}
		usage := "tokens/cost unavailable"
		if sample.Tokens != nil {
			usage = fmt.Sprintf("tokens %d; cost unavailable", *sample.Tokens)
		}
		if sample.Cost != nil {
			if sample.Tokens != nil {
				usage = fmt.Sprintf("tokens %d; cost %.4f", *sample.Tokens, *sample.Cost)
			} else {
				usage = fmt.Sprintf("tokens unavailable; cost %.4f", *sample.Cost)
			}
		}
		id := strings.Map(func(r rune) rune {
			if r < ' ' || r == 127 {
				return -1
			}
			return r
		}, sample.Task)
		if len(id) > 100 {
			id = id[:100]
		}
		lines = append(lines, fmt.Sprintf("%s: estimate %s; %s; %s", id, sample.Estimate.Duration, actual, usage))
	}
	return "Team sizing history (last 5 of at most 100 completions; elapsed includes waiting/review; " +
		"usage only when measured; legacy estimates may be completion snapshots):\n" + strings.Join(lines, "\n")
}
