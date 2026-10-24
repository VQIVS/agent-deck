package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/mcp"
	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestTaskNoticesUseExistingOwnerAndDoNotStopIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root, err := server.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	manager := &team.Manager{Store: team.Store{Root: t.TempDir()}, Team: team.Team{
		Name: "notices", Server: "http://127.0.0.1:1",
		Members: []team.Member{{ID: "member-id", Name: "Worker", Role: "worker"}},
	}}
	runtime, err := mcp.StartRuntime(t.Context(), &mcp.RuntimeOptions{
		JournalPath: filepath.Join(manager.Store.Root, "notices.channel.jsonl"), StateDir: filepath.Join(root, "runtime"),
		Interval: time.Hour, Provider: func(context.Context) (*team.Manager, error) { return manager, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	notices := []team.TaskNotice{{To: "Worker", Task: "T-1", Text: "CLI task brief"}}
	if warning := notifyTaskNotices(t.Context(), manager, notices); warning != "" {
		t.Fatal(warning)
	}
	if !runtime.Alive() {
		t.Fatal("CLI attachment stopped runtime owner")
	}
	records, err := runtime.ReadUser(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		found = found || strings.Contains(record.Text, "CLI task brief")
	}
	if !found {
		t.Fatal("notice not queued", records)
	}
	readOnlyManager := *manager
	readOnlyManager.ReadOnly = true
	if warning := notifyTaskNotices(t.Context(), &readOnlyManager, notices); !strings.Contains(warning, "read-only") {
		t.Fatal(warning)
	}
}
