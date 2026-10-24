package ui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/config"
	api "github.com/vqivs/agent-deck/internal/opencode"
)

func TestTeamReloadPreservesSessionDisplay(t *testing.T) {
	old := requestModel("http://server", false)
	old.focus, old.zoom = 0, true
	old.panes[0].State.Upsert(&api.Part{ID: "part", Type: "text", Text: "transcript"})
	old.panes[0].View.SetContent("one\ntwo\nthree\nfour")
	old.panes[0].View.Height = 1
	old.panes[0].View.SetYOffset(2)
	old.panes[0].Expanded = map[string]bool{"part": true}
	old.panes[0].Selected, old.panes[0].Content = "part", "cached transcript"
	old.panes[0].Loading = true
	old.panes[0].Pending = []api.Event{{Type: "old"}}
	deck := config.Deck{Server: old.deck.Server, Sessions: []config.Agent{
		{Name: "new", Dir: "/other", Session: "other"},
		{Name: "renamed", Dir: "/tmp/team", Session: "session-a"},
	}}
	next := New(context.Background(), &deck, old.opts)
	ReconcileTeamReload(old, next)
	p := &next.panes[1]
	if next.focus != 1 || !next.zoom || p.Agent.Name != "renamed" || p.State.Tail() != "transcript\n" ||
		p.View.YOffset != 2 || !p.Expanded["part"] || p.Selected != "part" || p.Content != "cached transcript" {
		t.Fatalf("display state lost: %+v", p)
	}
	if p.Loading || len(p.Pending) != 0 || len(next.panes[0].State.Parts) != 0 {
		t.Fatal("retained stale loading state or leaked transcript")
	}
}

func TestTeamReloadNeverCrossesBinding(t *testing.T) {
	old := requestModel("http://server", false)
	old.zoom = true
	old.panes[0].State.Error = "old state"
	old.panes[0].State.Upsert(&api.Part{ID: "old", Type: "text", Text: "private transcript"})
	old.panes[0].View.SetContent("old\nviewport")
	old.panes[0].Expanded = map[string]bool{"old": true}
	old.panes[0].Content, old.panes[0].Selected = "old content", "old"
	for _, change := range []string{"server", "directory", "session", "removed"} {
		deck := old.deck
		deck.Sessions = append([]config.Agent(nil), deck.Sessions...)
		switch change {
		case "server":
			deck.Server = "http://different"
		case "directory":
			deck.Sessions[0].Dir = "/different"
		case "session":
			deck.Sessions[0].Session = "different"
		case "removed":
			deck.Sessions = nil
		}
		next := New(context.Background(), &deck, old.opts)
		ReconcileTeamReload(old, next)
		if next.zoom || (len(next.panes) > 0 && next.panes[0].State.Error != "") {
			t.Fatalf("state reused across %s", change)
		}
		if len(next.panes) > 0 {
			p := &next.panes[0]
			if len(p.State.Parts) != 0 || len(p.Expanded) != 0 || p.Content != "" || p.Selected != "" {
				t.Fatalf("transcript state reused across %s", change)
			}
		}
	}
}

func TestReloadCommandCapturesOldModelAndContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := requestModel("http://old-server", false)
	m.context = func() context.Context { return ctx }
	cmd := m.ReloadCommand(func(captured *Model) tea.Cmd {
		return func() tea.Msg { return captured.deck.Server }
	})
	next := requestModel("http://new-server", false)
	*m = *next
	result := cmd().(TeamReloadResult)
	if result.Message != "http://old-server" || !result.Current() {
		t.Fatal("command followed replacement model")
	}
	cancel()
	if result.Current() || cmd() != nil {
		t.Fatal("old generation result survived cancellation")
	}
}

func TestTeamReloadAsyncGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := false
	cmd := GuardTeamReload(ctx, func() tea.Msg { called = true; return "result" })
	result := cmd().(TeamReloadResult)
	if !called || !result.Current() || result.Message != "result" {
		t.Fatal("current result lost")
	}
	cancel()
	if result.Current() || cmd() != nil {
		t.Fatal("canceled generation accepted")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cmd = GuardTeamReload(ctx, func() tea.Msg { cancel(); return "late" })
	if cmd() != nil || GuardTeamReload(ctx, nil) != nil {
		t.Fatal("late result or nil command mishandled")
	}
}

func TestTeamReloadEmitterWaitPreservesSingleGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan tea.Msg, 2)
	emit := TeamReloadEmitter(ctx, events)
	emit("event")
	result := TeamReloadWait(ctx, events)().(TeamReloadResult)
	if result.Message != "event" || !result.Current() {
		t.Fatal("stream result double-wrapped or lost")
	}
	cancel()
	emit("late")
	if result.Current() || len(events) != 0 || TeamReloadWait(ctx, events)() != nil {
		t.Fatal("old stream survived reload cancellation")
	}
}
