package ui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/config"
)

func messageServer(t *testing.T) (url string, sessions func() []string) {
	t.Helper()
	var mu sync.Mutex
	var hits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/prompt_async") {
			mu.Lock()
			hits = append(hits, strings.Split(r.URL.Path, "/")[2])
			mu.Unlock()
		}
		fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), hits...)
	}
}

func twoSessionModel(t *testing.T, server string, readOnly bool) *Model {
	t.Helper()
	m := New(t.Context(), &config.Deck{Server: server, Sessions: []config.Agent{
		{Name: "core-dev", Session: "ses_a", Dir: "/tmp/team"},
		{Name: "ui-dev", Session: "ses_b", Dir: "/tmp/team"},
	}}, Options{NoGit: true, ReadOnly: readOnly, Theme: "dark"})
	m.w, m.h = 80, 24
	return m
}

func typeText(m *Model, text string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func TestComposerStaysPinnedWhenPanesReorder(t *testing.T) {
	url, sessions := messageServer(t)
	m := twoSessionModel(t, url, false)
	m.focus = 1
	m.Update(key("m"))
	typeText(m, "for ui-dev")
	m.panes[0], m.panes[1] = m.panes[1], m.panes[0]
	m.focus = 1
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("send not dispatched")
	}
	m.Update(cmd())
	if got := sessions(); len(got) != 1 || got[0] != "ses_b" {
		t.Fatalf("message reached %v, want ses_b", got)
	}
}

func TestComposerRefusesReplacedRecipientAndKeepsDraft(t *testing.T) {
	url, sessions := messageServer(t)
	m := twoSessionModel(t, url, false)
	m.Update(key("m"))
	typeText(m, "hello")
	m.panes[0].Agent.Session = "ses_new"
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("sent to a replaced recipient")
	}
	original := messageTarget{Server: url, Dir: "/tmp/team", Session: "ses_a", Member: "core-dev"}
	if !strings.HasPrefix(m.composer.Status, "Recipient changed") || m.composer.Drafts[original] != "hello" {
		t.Fatalf("status=%q drafts=%v", m.composer.Status, m.composer.Drafts)
	}
	if len(sessions()) != 0 {
		t.Fatal("request reached the server")
	}
}

func TestComposerDraftsArePerRecipient(t *testing.T) {
	m := twoSessionModel(t, "http://127.0.0.1:1", false)
	m.Update(key("m"))
	typeText(m, "draft for core-dev mfpx")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.editing || !strings.Contains(m.note, "Draft saved for core-dev") {
		t.Fatalf("editing=%v note=%q", m.editing, m.note)
	}
	m.focus = 1
	m.Update(key("m"))
	if m.input.Value() != "" {
		t.Fatalf("draft leaked to another recipient: %q", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.focus = 0
	m.Update(key("m"))
	if m.input.Value() != "draft for core-dev mfpx" {
		t.Fatalf("draft not restored, shortcut letters not typed literally: %q", m.input.Value())
	}
}

func TestComposerSendingBlocksDuplicatesAndFailureKeepsDraft(t *testing.T) {
	m := twoSessionModel(t, "http://127.0.0.1:1", false)
	m.Update(key("m"))
	typeText(m, "once")
	_, first := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if _, second := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); first == nil || second != nil {
		t.Fatal("duplicate dispatch while sending")
	}
	typeText(m, "x")
	if m.input.Value() != "once" {
		t.Fatalf("draft edited while sending: %q", m.input.Value())
	}
	m.Update(messageSent{Target: m.composer.Target, Text: "once", Err: errors.New("connection refused")})
	if !m.editing || !strings.Contains(m.composer.Status, "Outcome unknown; draft kept") ||
		m.composer.Drafts[m.composer.Target] != "once" {
		t.Fatalf("failure state: editing=%v status=%q", m.editing, m.composer.Status)
	}
	if _, retry := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); retry != nil {
		t.Fatal("uncertain send allowed immediate duplicate retry")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if _, retry := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); retry == nil {
		t.Fatal("explicit retry not possible after failure")
	}
}

func TestComposerIdentityWrapsAndReturnsToDraft(t *testing.T) {
	m := twoSessionModel(t, "http://127.0.0.1:1", false)
	m.w, m.h = 80, 24
	m.panes[0].Agent.Dir = "/work/" + strings.Repeat("nested/", 50) + "target-end"
	m.Update(key("m"))
	typeText(m, "keep me")
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !m.composer.Identity {
		t.Fatal("actual terminal Tab did not open full identity")
	}
	view := m.composerIdentityView()
	if !strings.Contains(view, "API directory:") || !strings.Contains(view, "target-end") {
		t.Fatal("full wrapped identity unavailable")
	}
	m.composeKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.composer.Identity || m.input.Value() != "keep me" {
		t.Fatal("identity view lost draft")
	}
}

func TestComposerEmptyDiscardAndReadOnly(t *testing.T) {
	m := twoSessionModel(t, "http://127.0.0.1:1", false)
	m.Update(key("m"))
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || m.composer.Status != "Type a message" {
		t.Fatalf("blank send: %q", m.composer.Status)
	}
	typeText(m, "throw away")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if !m.composer.Discarding || m.input.Value() == "" {
		t.Fatal("discard did not ask first")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.input.Value() != "" || len(m.composer.Drafts) != 0 || !m.editing {
		t.Fatalf("discard: value=%q drafts=%v", m.input.Value(), m.composer.Drafts)
	}
	ro := twoSessionModel(t, "http://127.0.0.1:1", true)
	ro.Update(key("m"))
	if ro.editing || !strings.Contains(ro.note, "Read-only") {
		t.Fatalf("read-only opened composer: note=%q", ro.note)
	}
}

func TestComposerRendersPinnedFrame(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {160, 48}} {
		m := twoSessionModel(t, "http://127.0.0.1:4096", false)
		m.w, m.h = size[0], size[1]
		m.focus = 1
		m.Update(key("m"))
		typeText(m, "Please include the terminal restoration checks.")
		view := m.View()
		if lines := strings.Split(view, "\n"); len(lines) > size[1] {
			t.Fatalf("%dx%d rendered %d rows", size[0], size[1], len(lines))
		}
		for i, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > size[0] {
				t.Fatalf("row %d is %d cells wide", i+1, w)
			}
		}
		wants := []string{"pinned recipient: ui-dev", "session ses_b", "Enter Send to ui-dev", "Esc Cancel, keep draft"}
		for _, want := range wants {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %q:\n%s", want, view)
			}
		}
		assertScreenGolden(t, fmt.Sprintf("composer_%dx%d", size[0], size[1]), view)
	}
}
