package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/config"
	api "github.com/vqivs/agent-deck/internal/opencode"
)

func requestModel(server string, ro bool) *Model {
	return New(
		context.Background(),
		&config.Deck{
			Server:   server,
			Sessions: []config.Agent{{Name: "Worker", Session: "session-a", Dir: "/tmp/team"}},
		},
		Options{NoGit: true, ReadOnly: ro},
	)
}
func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestPermissionReplyTargetRejectsReboundSession(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	m.panes[0].State.UpdateRequests([]api.Permission{{ID: "request", SessionID: "session-a"}}, nil, time.Now())
	m.permissionKey("Y")
	m.panes[0].Agent.Session = "replacement"
	m.panes[0].State.UpdateRequests([]api.Permission{{ID: "request", SessionID: "replacement"}}, nil, time.Now())
	if cmd := m.replyPermissionTarget(m.permissionTarget, "always", ""); cmd != nil {
		t.Fatal("confirmation followed a replacement session")
	}
}

func TestWaitingPollAndReplyUI(t *testing.T) {
	var replies []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != "/tmp/team" {
			t.Error("unscoped request")
		}
		switch r.URL.Path {
		case "/permission":
			fmt.Fprint(w, `[{"id":"per_a","sessionID":"session-a","permission":"bash","patterns":["go test *"]}]`)
		case "/question":
			fmt.Fprint(w, `[]`)
		case "/permission/per_a/reply":
			var v struct{ Reply, Message string }
			_ = json.NewDecoder(r.Body).Decode(&v)
			replies = append(replies, v.Reply+":"+v.Message)
			fmt.Fprint(w, "true")
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	m := requestModel(s.URL, false)
	msg := m.requests("/tmp/team")()
	m.Update(msg)
	if view := m.View(); !strings.Contains(view, "WAITING 1") || !strings.Contains(view, "go test *") {
		t.Fatal(view)
	}
	_, cmd := m.Update(key("Y"))
	if cmd != nil || !m.permissionConfirm || len(replies) != 0 {
		t.Fatal("always skipped confirmation")
	}
	m.Update(key("n"))
	if len(replies) != 0 {
		t.Fatal("cancel replied")
	}
	m.Update(key("Y"))
	_, cmd = m.Update(key("y"))
	if cmd == nil {
		t.Fatal("no reply")
	}
	m.Update(cmd())
	if replies[0] != "always:" || m.panes[0].State.Waiting() {
		t.Fatal(replies)
	}
	// Restore a pending request and approve once.
	m.Update(msg)
	_, cmd = m.Update(key("y"))
	m.Update(cmd())
	if replies[1] != "once:" {
		t.Fatal(replies)
	}
	m.Update(msg)
	m.Update(key("x"))
	m.input.SetValue("use local tests")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.Update(cmd())
	if replies[2] != "reject:use local tests" {
		t.Fatal(replies)
	}
	m.Update(msg)
	m.Update(key("x"))
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.Update(cmd())
	if replies[3] != "reject:" {
		t.Fatal(replies)
	}
	m.Update(msg)
	m.Update(key("x"))
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.rejecting || len(replies) != 4 {
		t.Fatal("reject cancel")
	}
}

func TestReadOnlyPermissionsAndPollRace(t *testing.T) {
	m := requestModel("http://unused", true)
	p := api.Permission{ID: "per_a", SessionID: "session-a", Permission: "edit", Patterns: []string{"*.go"}}
	m.Update(requestsMsg{Dir: "/tmp/team", Permissions: []api.Permission{p}})
	for _, k := range []string{"y", "Y", "x"} {
		_, cmd := m.Update(key(k))
		if cmd != nil || m.permissionConfirm || m.rejecting {
			t.Fatal("read-only reply")
		}
	}
	// A reply arriving after a poll starts must not be undone by its stale result.
	_ = m.requests("/tmp/team")
	e := api.Event{
		Type:       "permission.replied",
		Properties: json.RawMessage(`{"sessionID":"session-a","requestID":"per_a"}`),
	}
	m.Update(streamMsg{Dir: "/tmp/team", Event: e})
	m.Update(requestsMsg{Dir: "/tmp/team", Permissions: []api.Permission{p}})
	if m.panes[0].State.Waiting() {
		t.Fatal("poll resurrected resolved permission")
	}
	// A failed poll must retain known pending requests.
	m.panes[0].State.UpdateRequests([]api.Permission{p}, nil, time.Now())
	m.Update(requestsMsg{Dir: "/tmp/team", PermissionErr: fmt.Errorf("offline")})
	if !m.panes[0].State.Waiting() {
		t.Fatal("failed poll hid pending request")
	}
	// A pending request that disappears while confirming must never target another ID.
	m.opts.ReadOnly = false
	m.Update(key("Y"))
	m.panes[0].State.UpdateRequests(nil, nil, time.Now())
	_, cmd := m.Update(key("y"))
	if cmd != nil {
		t.Fatal("replied to stale permission")
	}
	m.panes[0].State.UpdateRequests([]api.Permission{p}, nil, time.Now())
	m.Update(permissionReplyMsg{Dir: "/tmp/team", ID: p.ID, Err: fmt.Errorf("reply failed")})
	if !m.panes[0].State.Waiting() || !strings.Contains(m.note, "reply failed") {
		t.Fatal("failed reply cleared request")
	}
}

func TestQuestionsLayoutAndWaitingNotifications(t *testing.T) {
	m := requestModel("http://unused", true)
	m.opts.Notify = true
	var q api.Question
	question := `{"id":"que_a","sessionID":"session-a","questions":[{"header":"Deploy","question":"Which target?",
"options":[{"label":"Local","description":"Local machine"}]}]}`
	if err := json.Unmarshal([]byte(question), &q); err != nil {
		t.Fatal(err)
	}
	cmds := m.applyRequests(&requestsMsg{Dir: "/tmp/team", Questions: []api.Question{q}})
	if runtime.GOOS == "darwin" && (len(cmds) != 1 || cmds[0] == nil) {
		t.Fatal("missing waiting notification")
	}
	cmds = m.applyRequests(&requestsMsg{Dir: "/tmp/team", Questions: []api.Question{q}})
	if len(cmds) != 0 {
		t.Fatal("repeated notification on poll")
	}
	for _, size := range [][2]int{{100, 30}, {44, 12}, {20, 8}} {
		m.w, m.h = size[0], size[1]
		view := m.View()
		if !strings.Contains(view, "WAITING") || lipgloss.Width(view) > m.w || lipgloss.Height(view) > m.h {
			t.Fatalf("invalid waiting layout %dx%d: %s", m.w, m.h, view)
		}
	}
	m.w, m.h = 100, 30
	if view := m.View(); !strings.Contains(view, "Which target?") || !strings.Contains(view, "Local machine") {
		t.Fatal(view)
	}
	// Updating underlying BUSY/IDLE status while blocked must not notify WAITING again.
	m.Update(
		streamMsg{
			Dir: "/tmp/team",
			Event: api.Event{
				Type:       "session.status",
				Properties: json.RawMessage(`{"sessionID":"session-a","status":{"type":"busy"}}`),
			},
		},
	)
	if !m.panes[0].State.Waiting() {
		t.Fatal("status hid question")
	}
}
