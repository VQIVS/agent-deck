package ui

import (
	"testing"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func TestPaneChromeWrapAndPinnedGeometry(t *testing.T) {
	plan := PlanPaneChrome(Rect{X: 3, Y: 4, W: 24, H: 14},
		[]string{"Message", "Nudge", "Stop", "Attach", "Compact", "Approve once"}, 2)
	if len(plan.Controls) != 4 || plan.ControlY[0] != 6 || plan.FooterY != 16 ||
		plan.Waiting != (Rect{X: 4, Y: 11, W: 22, H: 2}) ||
		plan.Transcript != (Rect{X: 4, Y: 13, W: 22, H: 3}) {
		t.Fatalf("geometry drift: %+v", plan)
	}
	for _, bounds := range []Rect{{W: 1, H: 1}, {W: 12, H: 5}, {W: 20, H: 8}} {
		plan = PlanPaneChrome(bounds, []string{"A very long button", "OK"}, 999)
		if plan.Transcript.H < 0 || plan.Waiting.H < 0 || plan.Transcript.Y+plan.Transcript.H > bounds.H {
			t.Fatalf("out-of-bounds transcript: %+v", plan)
		}
	}
}

func TestPaneRequestCountersScopeAndDedupe(t *testing.T) {
	s := PaneRequestSnapshot{
		Server: "one", Directory: "/repo", Session: "s",
		Permissions: []api.Permission{{ID: "same", SessionID: "s"}, {ID: "foreign", SessionID: "other"}},
		Questions:   []api.Question{{ID: "same", SessionID: "s"}},
	}
	other := s
	other.Server = "two"
	counts := CountPaneRequests([]PaneRequestSnapshot{s, s, other, {Session: ""}})
	if counts != (PaneRequestCounts{Sessions: 2, Permissions: 2, Questions: 2}) {
		t.Fatalf("incorrect counters: %+v", counts)
	}
}
