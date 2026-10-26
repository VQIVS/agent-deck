package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestTeamOverviewRows(t *testing.T) {
	m := requestModel("http://server", false)
	m.panes[0].State.UpdateRequests([]api.Permission{{ID: "pending"}}, nil, time.Now())
	configured := team.Team{Server: "http://server", Members: []team.Member{
		{Name: "Worker", Role: "worker", Dir: "/tmp/team", Session: "session-a", Leases: []string{"internal/ui/**"}},
		{Name: "master", Role: "master"},
		{Name: "offline", Role: "worker", Dir: "/else", Session: "not-found"},
	}}
	rows := m.teamOverviewRows(&configured)
	if len(rows) != 3 || rows[0].Status != "WAITING" || rows[0].Permissions != 1 ||
		rows[1].Role != "master" || rows[1].Status != "NOT STARTED" || rows[2].Status != "OFFLINE" {
		t.Fatalf("inaccurate rows: %+v", rows)
	}
	text := RenderTeamOverviewRow(&rows[0], true, 120)
	if !strings.Contains(text, "NEEDS PERMISSION") || !strings.Contains(text, "internal/ui/**") ||
		len(strings.Split(text, "\n")) != 3 {
		t.Fatalf("missing alert or leases: %s", text)
	}
	for _, line := range strings.Split(RenderTeamOverviewRow(&rows[0], false, 12), "\n") {
		if lipgloss.Width(line) > 12 {
			t.Fatal("row overflow")
		}
	}
	rows[0].Leases[0] = "changed"
	if configured.Members[0].Leases[0] != "internal/ui/**" {
		t.Fatal("projection aliases leases")
	}
	configured.Server = "http://other-server"
	changed := m.teamOverviewRows(&configured)[0]
	if changed.Status != "OFFLINE" || changed.Permissions != 0 || changed.Questions != 0 {
		t.Fatal("cross-server status reused")
	}
	rows[0].Identity.Agent = strings.Repeat("long name", 30)
	if !strings.Contains(RenderTeamOverviewRow(&rows[0], false, 30), "NEEDS PERMISSION") {
		t.Fatal("long member name hid permission alert")
	}
}

func TestTeamOverviewGeometry(t *testing.T) {
	rects := TeamOverviewRects(2, 7, 60, 8, 3, 10)
	if len(rects) != 2 || rects[0] != (Rect{X: 2, Y: 7, W: 60, H: 3}) ||
		rects[1] != (Rect{X: 2, Y: 10, W: 60, H: 3}) {
		t.Fatalf("bad geometry: %+v", rects)
	}
	zones := []zone{{Rect: rects[0], Pane: 3}, {Rect: rects[1], Pane: 4}}
	if z, ok := hit(zones, 3, 10); !ok || z.Pane != 4 {
		t.Fatal("second row points at wrong member")
	}
	if _, ok := hit(zones, 3, 13); ok {
		t.Fatal("blank trailing lines have a target")
	}
	if len(TeamOverviewRects(0, 0, 10, 2, 0, 2)) != 0 ||
		len(TeamOverviewRects(0, 0, 10, 20, 2, 2)) != 0 {
		t.Fatal("invalid viewport has rows")
	}
}

func TestTeamOverviewRetainsPermissionSummary(t *testing.T) {
	m := requestModel("http://server", false)
	configured := team.Team{Members: []team.Member{{
		Name: "adopted", Role: "master", Mode: "adopt",
		Capability: "inherited", Session: "s",
	}}}
	rows := m.teamOverviewRows(&configured)
	if !strings.Contains(RenderTeamOverviewRow(&rows[0], false, 120), "inherited from session") {
		t.Fatal("inherited policy summary missing")
	}
	configured.Members[0].Capability = "coder"
	rows = m.teamOverviewRows(&configured)
	if !strings.Contains(RenderTeamOverviewRow(&rows[0], false, 160), "applies only to newly created sessions") {
		t.Fatal("adopted creation-only policy summary missing")
	}
}
