package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/qivs/agent-deck/internal/opencode"
	"github.com/qivs/agent-deck/internal/team"
)

func TestSafeApprovalRequiresVerifiedToolCommand(t *testing.T) {
	m := requestModel("http://unused", false)
	tm := team.Team{
		Approval: "allowlist", SafeCommands: []string{"go test ./internal/team/..."},
		Members: []team.Member{{Session: "session-a", Dir: "/tmp/team", Permissions: map[string]string{"edit": "deny"}}},
	}
	m.OpenTeam(&tm, nil, t.TempDir())
	m.teams.Manager.Team.LeasePool = true
	member := &m.teams.Manager.Team.Members[0]
	member.Name, member.Role = "reviewer", "worker"
	member.LeasePolicy = m.teams.Manager.Team.LeaseFingerprint(member)
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
	part.State.Input = json.RawMessage(`{"command":"go test ./internal/team/...","workdir":"/other/repo"}`)
	m.panes[0].State.Upsert(&part)
	if len(m.safeCommandReplies()) != 0 {
		t.Fatal("foreign workdir auto-approved")
	}
	part.State.Input = json.RawMessage(`{"command":"go test ./internal/team/..."}`)
	m.panes[0].State.Upsert(&part)
	m.teams.Manager.Team.Members[0].Permissions["edit"] = "allow"
	if len(m.safeCommandReplies()) != 0 {
		t.Fatal("editable repo command auto-approved")
	}
	m.teams.Manager.Team.Members[0].Permissions["edit"] = "deny"
	member.LeasePolicy = m.teams.Manager.Team.LeaseFingerprint(member)
	if len(m.safeCommandReplies()) != 1 || len(m.safeCommandReplies()) != 0 {
		t.Fatal("verified match not serialized")
	}
	m.replied(permissionReplyMsg{ID: "permission", Err: fmt.Errorf("failed")})
	if len(m.safeCommandReplies()) != 1 {
		t.Fatal("failed approval never retried")
	}
}

func TestAutoApprovalAudit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := logAutoApproval("/repo", "session", "git status"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-deck", "approvals.jsonl"))
	if err != nil || !strings.Contains(string(data), `"command":"git status"`) ||
		!strings.Contains(string(data), `"session":"session"`) {
		t.Fatal(string(data), err)
	}
}
