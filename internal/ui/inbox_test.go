package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/state"
)

func TestInboxProjectionIdentityAndRefresh(t *testing.T) {
	now := time.Unix(1000, 0)
	s := state.NewWithClock(func() time.Time { return now }, time.Minute)
	s.SetStatus("busy", now.Add(-time.Minute))
	identity := InboxIdentity{Agent: "worker", Directory: "/repo", Session: "session"}
	s.UpdateRequests([]api.Permission{
		{ID: "request", SessionID: "session", Permission: "bash", Patterns: []string{"go test ./..."}},
		{ID: "foreign", SessionID: "other"},
	}, nil, now)
	var question api.Question
	if err := json.Unmarshal([]byte(`{"id":"question","sessionID":"session",
		"questions":[{"header":"Choice","question":"Which target?"}]}`), &question); err != nil {
		t.Fatal(err)
	}
	s.Questions = []api.Question{question}
	source := InboxSource{
		Identity: identity, State: &s,
		Reports: []InboxReport{{ID: "report", Text: "finished", At: now, Inferred: true}},
	}
	entries := ProjectInbox([]InboxSource{source, source}, now)
	if len(entries) != 3 {
		t.Fatalf("expected permission/question/report, got %+v", entries)
	}
	ids := map[string]bool{}
	for _, entry := range entries {
		if entry.Identity != identity || ids[entry.ID] || entry.Kind == "stall" {
			t.Fatalf("identity, dedupe or waiting precedence failure: %+v", entry)
		}
		ids[entry.ID] = true
		if entry.Kind == "done" && (!entry.Inferred || !strings.Contains(entry.Title, "inferred")) {
			t.Fatal("inferred report not labeled")
		}
	}
	for _, entry := range ProjectInbox([]InboxSource{source}, now.Add(time.Hour)) {
		if !ids[entry.ID] {
			t.Fatal("refresh changed entry identity")
		}
	}
	other := source
	other.Identity.Directory = "/another"
	if len(ProjectInbox([]InboxSource{source, other}, now)) != 6 {
		t.Fatal("same IDs in different directories collided")
	}
}

func TestInboxStallAndErrorEpisodes(t *testing.T) {
	now := time.Unix(1000, 0)
	s := state.NewWithClock(func() time.Time { return now }, time.Minute)
	s.SetStatus("busy", now.Add(-2*time.Minute))
	source := InboxSource{Identity: InboxIdentity{Agent: "worker", Directory: "/repo", Session: "s"}, State: &s}
	first := ProjectInbox([]InboxSource{source}, now)
	if len(first) != 1 || first[0].Kind != "stall" || !first[0].At.Equal(s.Since.Add(time.Minute)) {
		t.Fatalf("missing stall: %+v", first)
	}
	s.SetStatus("idle", now)
	if len(ProjectInbox([]InboxSource{source}, now)) != 0 {
		t.Fatal("resolved stall remained active")
	}
	s.SetStatus("busy", now)
	next := ProjectInbox([]InboxSource{source}, now.Add(time.Minute))
	if len(next) != 1 || next[0].ID == first[0].ID {
		t.Fatal("new stall episode reused old identity")
	}
	s.SetStatus("error", now)
	s.Error = "connection failed"
	errors := ProjectInbox([]InboxSource{source}, now)
	if len(errors) != 1 || errors[0].Kind != "error" || errors[0].Detail != s.Error {
		t.Fatalf("missing error: %+v", errors)
	}
	if len(ProjectInbox([]InboxSource{{Reports: []InboxReport{{Text: "no stable ID"}}}}, now)) != 0 {
		t.Fatal("report without stable ID accepted")
	}
}

func TestInboxSelectionAndFilter(t *testing.T) {
	entries := []InboxEntry{
		{ID: "a", Kind: "permission", Identity: InboxIdentity{Agent: "alice", Directory: "/Repo"}, Detail: "BASH"},
		{ID: "b", Kind: "done", Identity: InboxIdentity{Agent: "bob"}},
		{ID: "c", Kind: "error", Identity: InboxIdentity{Agent: "alice"}},
	}
	s := InboxSelection{ID: "b"}
	s.Reconcile(entries)
	if s.Index != 1 {
		t.Fatal("selection not restored by ID")
	}
	entries = append([]InboxEntry{{ID: "new"}}, entries...)
	s.Reconcile(entries)
	if s.Index != 2 || s.ID != "b" {
		t.Fatal("insertion moved selection")
	}
	s.Move(entries, 10)
	if s.ID != "c" {
		t.Fatal("move did not clamp")
	}
	filtered := FilterInbox(entries, InboxFilter{Kind: "permission", Agent: "alice", Query: "bash"})
	selected, ok := s.Selected(filtered)
	if !ok || selected.ID != "a" {
		t.Fatal("filter did not reconcile selection")
	}
	if len(FilterInbox(entries, InboxFilter{Query: "/repo"})) != 1 {
		t.Fatal("directory search failed")
	}
	if _, ok = s.Selected(nil); ok || s.ID != "" || s.Index != 0 {
		t.Fatal("empty selection not reset")
	}
	s.Move(nil, -1)
}

func TestInboxRenderBoundedAndSanitized(t *testing.T) {
	entries := []InboxEntry{
		{ID: "a", Kind: "error", Identity: InboxIdentity{Agent: "one"}, Title: "error"},
		{ID: "b", Kind: "done", Identity: InboxIdentity{Agent: "two"}, Title: "done"},
		{
			ID: "c", Kind: "permission", Identity: InboxIdentity{Agent: "three"},
			Title: "\x1b[31munsafe\x1b[0m\nnew\rline", Detail: "\x1b]0;title\a",
		},
	}
	for _, width := range []int{1, 20, 80} {
		for _, height := range []int{1, 2, 3, 8} {
			rendered := RenderInbox(entries, "c", width, height)
			if len(strings.Split(rendered, "\n")) > height || ansi.Strip(rendered) != rendered {
				t.Fatalf("unbounded or unsafe output: %q", rendered)
			}
			for _, line := range strings.Split(rendered, "\n") {
				if lipgloss.Width(line) > width || strings.ContainsAny(line, "\r\x1b\a") {
					t.Fatalf("bad rendered line: %q", line)
				}
			}
		}
	}
	if !strings.Contains(RenderInbox(entries, "c", 80, 2), "three") {
		t.Fatal("selected entry not in viewport")
	}
	if RenderInbox(nil, "", 80, 2) != "Inbox empty" || RenderInbox(entries, "c", 0, 2) != "" {
		t.Fatal("empty or zero-size rendering failed")
	}
}
