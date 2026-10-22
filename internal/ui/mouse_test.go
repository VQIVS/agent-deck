package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func clickZone(t *testing.T, m *Model, action string, pane int) tea.Cmd {
	t.Helper()
	m.View()
	for _, z := range m.zones {
		if z.Action == action && z.Pane == pane {
			return m.mouse(tea.MouseMsg{X: z.X, Y: z.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		}
	}
	t.Fatalf("missing zone %s pane %d", action, pane)
	return nil
}

func TestHitTesting(t *testing.T) {
	zones := []zone{{Rect{0, 0, 20, 10}, "focus", 0}, {Rect{2, 2, 8, 1}, "m", 0}}
	for _, tc := range []struct {
		x, y int
		want string
	}{{2, 2, "m"}, {9, 2, "m"}, {10, 2, "focus"}, {19, 9, "focus"}, {20, 0, ""}, {-1, 2, ""}} {
		z, ok := hit(zones, tc.x, tc.y)
		if ok != (tc.want != "") || ok && z.Action != tc.want {
			t.Fatalf("%+v -> %+v %t", tc, z, ok)
		}
	}
}

func TestButtonVisibility(t *testing.T) {
	for _, ro := range []bool{false, true} {
		for _, pending := range []bool{false, true} {
			buttons := paneButtons(pending, ro, false)
			want := 5
			if pending {
				want = 8
			}
			if len(buttons) != want {
				t.Fatal(buttons)
			}
			for _, b := range buttons {
				if b.Action != "enter" && b.Disabled != ro {
					t.Fatal(b)
				}
			}
		}
	}
	m := requestModel("http://unused", true)
	m.w = 100
	m.h = 30
	m.panes[0].State.UpdateRequests(
		[]api.Permission{{ID: "per_a", SessionID: "session-a", Permission: "edit"}},
		nil,
		time.Now(),
	)
	view := m.View()
	for _, text := range []string{"[Message]", "[Nudge]", "[Stop]", "[Attach]", "[Approve once]", "[Always]", "[Reject]"} {
		if !strings.Contains(view, text) {
			t.Fatal("button missing", text)
		}
	}
	for _, z := range m.zones {
		if slices.Contains([]string{"m", "n", "s", "a", "y", "Y", "x"}, z.Action) {
			t.Fatal("read-only action zone", z)
		}
	}
}

func TestMouseFocusZoomWheelAndHover(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w = 100
	m.h = 32
	m.add(&m.panes[0].Agent)
	a := m.panes[0].Agent
	a.Session = "session-b"
	a.Name = "Second"
	m.add(&a)
	clickZone(t, m, "focus", 1)
	if m.focus != 1 {
		t.Fatal("focus")
	}
	clickZone(t, m, "enter", 1)
	if !m.zoom {
		t.Fatal("zoom")
	}
	clickZone(t, m, "esc", 1)
	if m.zoom {
		t.Fatal("unzoom")
	}
	m.panes[1].State.Upsert(&api.Part{ID: "text", Type: "text", Text: strings.Repeat("line\n\n", 100)})
	m.View()
	m.panes[1].View.GotoBottom()
	before := m.panes[1].View.YOffset
	for _, z := range m.zones {
		if z.Pane == 1 && z.Action == "focus" {
			m.mouse(tea.MouseMsg{X: z.X + 1, Y: z.Y + 1, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
			break
		}
	}
	if m.panes[1].View.YOffset >= before {
		t.Fatal("wheel did not scroll")
	}
	for _, z := range m.zones {
		if z.Action == "r" {
			m.mouse(tea.MouseMsg{X: z.X, Y: z.Y, Action: tea.MouseActionMotion})
			if m.mouseX != z.X || m.mouseY != z.Y {
				t.Fatal("hover position")
			}
		}
	}
}

func TestMouseModalsAndPicker(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w = 100
	m.h = 30
	clickZone(t, m, "s", 0)
	if !m.confirm {
		t.Fatal("stop confirm")
	}
	clickZone(t, m, "esc", -1)
	if m.confirm {
		t.Fatal("cancel stop")
	}
	clickZone(t, m, "m", 0)
	if !m.editing {
		t.Fatal("composer")
	}
	m.View()
	if _, ok := hit(m.zones, 0, m.h-1); ok {
		t.Fatal("outside occupied")
	}
	m.mouse(tea.MouseMsg{X: 0, Y: m.h - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.editing {
		t.Fatal("outside close")
	}
	clickZone(t, m, "m", 0)
	m.input.SetValue("hello")
	if cmd := clickZone(t, m, "send", -1); cmd == nil || m.editing {
		t.Fatal("send dispatch")
	}
	m.inventory.Sessions = []api.Session{{ID: "session-a", Title: "Worker", Directory: "/tmp/team"}}
	clickZone(t, m, "p", -1)
	if !m.picker {
		t.Fatal("picker")
	}
	clickZone(t, m, "pick:0", -1)
	if !m.hidden["session-a"] {
		t.Fatal("row did not toggle")
	}
	clickZone(t, m, "esc", -1)
	if m.picker {
		t.Fatal("picker cancel")
	}
	delete(m.hidden, "session-a")
	m.panes[0].State.UpdateRequests(
		[]api.Permission{{ID: "per_a", SessionID: "session-a", Permission: "edit"}},
		nil,
		time.Now(),
	)
	clickZone(t, m, "Y", 0)
	if !m.permissionConfirm {
		t.Fatal("always confirm")
	}
	m.View()
	for _, z := range m.zones {
		if z.Action == "m" {
			t.Fatal("background action active in modal")
		}
	}
	clickZone(t, m, "esc", -1)
	clickZone(t, m, "Y", 0)
	if cmd := clickZone(t, m, "y", -1); cmd == nil || m.permissionConfirm {
		t.Fatal("yes dispatch")
	}
}
