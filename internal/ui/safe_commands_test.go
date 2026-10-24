package ui

import (
	"encoding/json"
	"testing"
	"time"

	api "github.com/qivs/agent-deck/internal/opencode"
	"github.com/qivs/agent-deck/internal/team"
)

func TestSafeApprovalRequiresVerifiedToolCommand(t *testing.T) {
	m := requestModel("http://unused", false)
	tm := team.Team{
		Approval: "allowlist", SafeCommands: []string{"go test ./internal/team/..."},
		Members: []team.Member{{Session: "session-a", Dir: "/tmp/team"}},
	}
	m.OpenTeam(&tm, nil, t.TempDir())
	r := api.Permission{ID: "permission", SessionID: "session-a", Permission: "bash", Patterns: []string{"go test *"}}
	r.Tool.CallID = "call"
	m.panes[0].State.UpdateRequests([]api.Permission{r}, nil, time.Now())
	if len(m.safeCommandReplies()) != 0 {
		t.Fatal("approved abbreviated pattern")
	}
	part := api.Part{ID: "tool", CallID: "call", Type: "tool", Tool: "bash"}
	part.State.Input = json.RawMessage(`{"command":"go test ./internal/team/..."}`)
	m.panes[0].State.Upsert(&part)
	m.opts.ReadOnly = true
	if len(m.safeCommandReplies()) != 0 {
		t.Fatal("read-only approval")
	}
	m.opts.ReadOnly = false
	if len(m.safeCommandReplies()) != 1 || len(m.safeCommandReplies()) != 0 {
		t.Fatal("verified match not serialized")
	}
}
