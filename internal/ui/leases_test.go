package ui

import (
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestLeaseFormAndApplyConfirmation(t *testing.T) {
	m := requestModel("http://unused", false)
	tm := team.Team{Name: "Example", Members: []team.Member{
		{
			Name: "Worker", Role: "worker", Session: "session-a", Dir: "/tmp/team",
			Leases: []string{"internal/a/**", "internal/b/**"},
		},
	}}
	m.OpenTeam(&tm, nil, t.TempDir())
	m.teams.Draft = tm
	m.memberForm()
	found := false
	for _, field := range m.teams.Fields {
		if strings.HasPrefix(field.Label, "Leases") {
			found = field.Value == "internal/a/**\ninternal/b/**"
		}
	}
	if !found || !strings.Contains(m.memberLeases("session-a"), "leases PENDING apply") {
		t.Fatal("lease field or pane summary missing")
	}
	m.teams.Screen = "members"
	if cmd := m.teamAction("member-leases"); cmd != nil || m.teams.Confirm != "leases" {
		t.Fatal("apply bypassed confirmation")
	}
	if view := m.View(); !strings.Contains(view, "permissions reset") {
		t.Fatal("confirmation failed to disclose permission reset")
	}
	m.teamAction("team-no")
	m.opts.ReadOnly = true
	m.teamAction("member-leases")
	if m.teams.Confirm != "" {
		t.Fatal("read-only enabled apply")
	}
}
