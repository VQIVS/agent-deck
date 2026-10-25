package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCtrlJInsertsNewline(t *testing.T) {
	m := requestModel("http://127.0.0.1:1", false)
	m.editing = true
	m.input.Focus()
	m.input.SetValue("one")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("two")})
	if !m.editing || m.input.Value() != "one\ntwo" {
		t.Fatalf("editing=%v value=%q", m.editing, m.input.Value())
	}
}

func TestEnterSendsComposedMessage(t *testing.T) {
	sent := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/session/session-a/prompt_async" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		sent = true
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	m := requestModel(server.URL, false)
	m.openComposer(0)
	m.input.SetValue("Hello worker")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.editing {
		t.Fatal("Enter did not submit message")
	}
	result := cmd().(messageSent)
	if result.Err != nil || !sent {
		t.Fatalf("send failed: %v", result.Err)
	}
	m.Update(result)
	if m.editing || !strings.Contains(m.note, "transport accepted") {
		t.Fatalf("editing=%v note=%q", m.editing, m.note)
	}
}
