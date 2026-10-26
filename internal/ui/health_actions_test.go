package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/state"
)

func TestCompactUsesSessionModelAndRefusesBusyOrWaiting(t *testing.T) {
	busy, waiting, compacted := false, false, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != "/tmp/team" {
			t.Error("unscoped compact")
		}
		switch r.URL.Path {
		case "/permission":
			if waiting {
				fmt.Fprint(w, `[{"sessionID":"session-a"}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
		case "/question":
			fmt.Fprint(w, `[]`)
		case "/session/status":
			if busy {
				fmt.Fprint(w, `{"session-a":{"type":"busy"}}`)
			} else {
				fmt.Fprint(w, `{}`)
			}
		case "/session/session-a/message":
			fmt.Fprint(w, `[{"info":{"role":"assistant","providerID":"provider","modelID":"model"}}]`)
		case "/session/session-a/summarize":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["providerID"] != "provider" || body["modelID"] != "model" {
				t.Error(body)
			}
			compacted++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	m := requestModel(s.URL, false)
	if err := m.compactSession("/tmp/team", "session-a"); err != nil {
		t.Fatal(err)
	}
	busy = true
	if err := m.compactSession("/tmp/team", "session-a"); err == nil {
		t.Fatal("busy compact")
	}
	busy, waiting = false, true
	if err := m.compactSession("/tmp/team", "session-a"); err == nil {
		t.Fatal("waiting compact")
	}
	if compacted != 1 {
		t.Fatal(compacted)
	}
	m.opts.ReadOnly = true
	if m.compact() != nil {
		t.Fatal("read-only compact")
	}
}

func TestDefaultStallBadgeRemainsWithoutMemberControls(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w, m.h = 80, 24
	s := &m.panes[0].State
	s.SetStatus("busy", time.Now().Add(-state.DefaultStallAfter-time.Minute))
	if !strings.Contains(m.View(), "STALLED") {
		t.Fatal("default stall badge disappeared")
	}
	s.UpdateRequests([]api.Permission{{ID: "ask", SessionID: "session-a", Permission: "bash"}}, nil, time.Now())
	view := m.View()
	if !strings.Contains(view, "WAITING") || strings.Contains(view, "STALLED") {
		t.Fatal("permission wait must take precedence over stall")
	}
}
