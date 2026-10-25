package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

// taskDetail deliberately contains board evidence, never manager/session configs
// or credentials. Summaries remain small; this explicit read returns full history.
type taskDetail struct {
	taskSummary
	Kind        string            `json:"kind,omitempty"`
	Role        string            `json:"role,omitempty"`
	Skills      []string          `json:"skills,omitempty"`
	Branch      string            `json:"branch,omitempty"`
	Commits     []string          `json:"commits,omitempty"`
	History     []team.TaskChange `json:"history"`
	Labels      []string          `json:"labels"`
	Estimate    team.TaskEstimate `json:"estimate"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt time.Time         `json:"completed_at"`
	ShippedAt   time.Time         `json:"shipped_at"`
	Review      *team.TaskReview  `json:"review,omitempty"`
}

func showTaskDefinition() map[string]any {
	return tool("show_task", "Read full task requirements, ownership, comments and history; use after list_tasks",
		map[string]any{"id": map[string]any{"type": "string"}}, []string{"id"})
}

// readShowTask is reached only after TaskTools.Call authenticates current team
// membership. Force read-only loading even when the caller can mutate other tasks.
func readShowTask(ctx context.Context, store team.BoardStore, id string) (any, error) {
	if strings.TrimSpace(id) == "" || len(id) > 200 {
		return nil, fmt.Errorf("show_task requires a bounded nonempty task ID")
	}
	store.ReadOnly = true
	state, err := store.Load(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("unknown task %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("read task details: %w", err)
	}
	for i := range state.Board.Tasks {
		task := &state.Board.Tasks[i]
		if task.ID != id {
			continue
		}
		summary := summarize(task, true)
		summary.Comments = task.Comments
		summary.Category = state.Board.Category(task.State)
		return taskDetail{
			Kind: task.Kind, Role: task.Role, Skills: task.Skills, Branch: task.Branch, Commits: task.Commits,
			taskSummary: summary, History: task.History, Labels: task.Labels, Estimate: task.Estimate,
			CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, StartedAt: task.StartedAt,
			CompletedAt: task.CompletedAt, ShippedAt: task.ShippedAt, Review: task.Review,
		}, nil
	}
	return nil, fmt.Errorf("unknown task %q", id)
}
