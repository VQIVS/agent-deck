package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func inboxScreenModel(server string, readOnly bool) (*Model, *InboxScreen) {
	m := requestModel(server, readOnly)
	m.w, m.h = 100, 20
	m.panes[0].State.UpdateRequests([]api.Permission{
		{ID: "first", SessionID: "session-a", Permission: "bash"},
		{ID: "chosen", SessionID: "session-a", Permission: "edit"},
	}, nil, time.Unix(1000, 0))
	screen := &InboxScreen{Open: true}
	entries := m.inboxEntries(screen)
	for i := range entries {
		if entries[i].RequestID == "chosen" {
			screen.Selection.ID = entries[i].ID
		}
	}
	return m, screen
}

func TestInboxScreenRepliesToStoredRequest(t *testing.T) {
	var path, reply, reason string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.URL.Query().Get("directory") != "/tmp/team" {
			t.Error("unscoped inbox reply")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		reply, reason = body["reply"], body["message"]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	m, screen := inboxScreenModel(server.URL, false)
	if cmd := m.inboxKey(screen, key("Y")); cmd != nil || screen.pending == nil {
		t.Fatal("always approval did not require confirmation")
	}
	screen.Selection.ID = m.inboxEntries(screen)[0].ID
	cmd := m.inboxKey(screen, key("y"))
	if cmd == nil {
		t.Fatal("confirmation did not submit")
	}
	if duplicate := m.inboxKey(screen, key("y")); duplicate != nil {
		t.Fatal("duplicate approval accepted while first reply is in flight")
	}
	if result := cmd().(permissionReplyMsg); result.Err != nil {
		t.Fatal(result.Err)
	}
	if path != "/permission/chosen/reply" || reply != "always" {
		t.Fatalf("wrong request: %s %s", path, reply)
	}
	_, screen = inboxScreenModel(server.URL, false)
	m.inboxKey(screen, key("x"))
	m.inboxKey(screen, key("not allowed"))
	cmd = m.inboxKey(screen, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("reject did not submit")
	}
	cmd()
	if reply != "reject" || reason != "not allowed" || path != "/permission/chosen/reply" {
		t.Fatalf("wrong reject: %s %s %s", path, reply, reason)
	}
}

func TestInboxScreenStaleAndReadOnlyGuards(t *testing.T) {
	m, screen := inboxScreenModel("http://unused.invalid", false)
	m.inboxKey(screen, key("Y"))
	m.panes[0].State.Permissions = []api.Permission{{ID: "replacement", SessionID: "session-a"}}
	if cmd := m.inboxKey(screen, key("y")); cmd != nil || !strings.Contains(m.note, "no longer pending") {
		t.Fatal("stale approval was retargeted")
	}
	m, screen = inboxScreenModel("http://unused.invalid", false)
	m.inboxKey(screen, key("Y"))
	m.panes[0].Agent.Session = "replacement-session"
	if cmd := m.inboxKey(screen, key("y")); cmd != nil {
		t.Fatal("session replacement bypassed identity guard")
	}
	m, screen = inboxScreenModel("http://unused.invalid", true)
	for _, action := range []string{"y", "Y", "x", "n", "a"} {
		if cmd := m.inboxKey(screen, key(action)); cmd != nil || screen.pending != nil {
			t.Fatalf("read-only action allowed: %s", action)
		}
	}
	m.inboxKey(screen, tea.KeyMsg{Type: tea.KeyEnter})
	if screen.Open || !m.zoom {
		t.Fatal("read-only pane opening should work")
	}
}

func TestInboxScreenNavigationSearchAndMouse(t *testing.T) {
	m, screen := inboxScreenModel("http://unused.invalid", false)
	m.inboxKey(screen, key("/"))
	m.inboxKey(screen, key("edit"))
	m.inboxKey(screen, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.inboxEntries(screen)) != 1 || screen.searching {
		t.Fatal("search did not filter")
	}
	view := m.inboxView(screen)
	if !strings.Contains(view, "Permission: edit") || len(m.zones) != 1 {
		t.Fatalf("screen or targets missing: %s", view)
	}
	screen.Filter.Query = ""
	m.inboxView(screen)
	last := m.zones[len(m.zones)-1]
	m.inboxClick(screen, last, true)
	if screen.Selection.ID != strings.TrimPrefix(last.Action, "inbox-select:") {
		t.Fatal("row click did not select identity")
	}
	m.inboxKey(screen, tea.KeyMsg{Type: tea.KeyTab})
	if screen.Filter.Kind != "permission" {
		t.Fatal("kind filter did not advance")
	}
	m.inboxKey(screen, tea.KeyMsg{Type: tea.KeyEsc})
	if screen.Open {
		t.Fatal("escape did not close")
	}
}
