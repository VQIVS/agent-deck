package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/vqivs/agent-deck/internal/mcp"
	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

func notifyTaskNotices(ctx context.Context, manager *team.Manager, notices []team.TaskNotice) string {
	if len(notices) == 0 {
		return ""
	}
	if manager.ReadOnly {
		return "task notices skipped: read-only"
	}
	root, err := server.StateRoot()
	if err != nil {
		return "task saved; notices skipped: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	journal := filepath.Join(manager.Store.Root, team.Slug(manager.Team.Name)+".channel.jsonl")
	connection, err := mcp.ConnectRuntime(ctx, manager.Team.Name, journal, filepath.Join(root, "runtime"), false)
	if err != nil {
		return fmt.Sprintf("task saved; %d notice(s) skipped: runtime owner unavailable (%v)", len(notices), err)
	}
	defer func() { _ = connection.Close() }()
	if err := connection.Notify(ctx, notices); err != nil {
		return "task saved; notice delivery warning: " + err.Error()
	}
	return ""
}
