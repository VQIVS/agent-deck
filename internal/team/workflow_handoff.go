package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/server"
)

// WorkflowHandoff gathers evidence and open tasks without claiming model-context transfer.
func WorkflowHandoff(ctx context.Context, manager *Manager, index int) (string, error) {
	if manager == nil || index < 0 || index >= len(manager.Team.Members) {
		return "", fmt.Errorf("handoff requires current member")
	}
	member := &manager.Team.Members[index]
	var text strings.Builder
	fmt.Fprintf(&text, "Handoff for %s. This summary does not transfer model conversation context.\n", member.Name)
	record, err := (ReportStore{Root: filepath.Join(manager.Store.Root, ".reports"), ReadOnly: true}).
		Load(ctx, &manager.Team, member.Name)
	if err == nil {
		data, encodeErr := json.Marshal(record)
		if encodeErr != nil {
			return "", fmt.Errorf("encode handoff report: %w", encodeErr)
		}
		text.WriteString("Stored report (worker claims):\n" + string(data) + "\n")
	} else if member.Session != "" {
		messages, readErr := api.New(manager.Team.Server, true).Messages(ctx, member.SessionDir(), member.Session)
		if readErr != nil {
			return "", fmt.Errorf("read handoff transcript: %w", readErr)
		}
		text.WriteString("Latest assistant text (inferred, not verified):\n" + AssistantText(messages) + "\n")
	}
	if err := handoffOpenTasks(ctx, manager, member, &text); err != nil {
		return "", err
	}
	return text.String(), nil
}

func handoffOpenTasks(ctx context.Context, manager *Manager, member *Member, text *strings.Builder) error {
	root, err := server.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve handoff state: %w", err)
	}
	state, err := (BoardStore{Root: filepath.Join(root, "boards"), Team: manager.Team.Name, ReadOnly: true}).Load(ctx)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read handoff tasks: %w", err)
	}
	text.WriteString("Open tasks:\n")
	for i := range state.Board.Tasks {
		task := &state.Board.Tasks[i]
		if task.State == "done" || task.State == TaskCancelled {
			continue
		}
		if member.Role != "master" && task.Assignee != member.Name {
			continue
		}
		fmt.Fprintf(text, "- %s [%s] %s; assignee=%s; reviewer=%s\n",
			task.ID, task.State, task.Title, task.Assignee, task.Reviewer)
	}
	return nil
}
