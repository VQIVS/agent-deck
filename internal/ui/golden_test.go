package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	api "github.com/qivs/agent-deck/internal/opencode"
	"github.com/qivs/agent-deck/internal/state"
	"github.com/qivs/agent-deck/internal/team"
)

func TestScreenGoldens(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {160, 48}} {
		for _, screen := range []string{"dashboard", "team", "inbox", "palette"} {
			name := fmt.Sprintf("%s_%dx%d", screen, size[0], size[1])
			t.Run(name, func(t *testing.T) {
				m := goldenScreenModel(t, size[0], size[1])
				view := goldenScreenView(m, screen)
				assertScreenGolden(t, name, view)
			})
		}
	}
}

func goldenScreenModel(t *testing.T, width, height int) *Model {
	t.Helper()
	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	configured := team.Team{
		Version: 1, Name: "review-pool", Server: "http://127.0.0.1:4096",
		Repo: "/workspace/project", Base: "main", Capacity: 3, LeasePool: true,
		Members: []team.Member{
			{
				Name: "Sol", Role: "master", Mode: "shared", Dir: "/workspace/project", Session: "session-sol",
				Capability: "coder", Permissions: team.Preset("coder"),
			},
			{
				Name: "health", Role: "worker", Mode: "shared", Dir: "/workspace/project", Session: "session-health",
				Capability: "coder", Permissions: team.Preset("coder"), Leases: []string{"internal/state/**"},
			},
			{
				Name: "reviewer", Role: "worker", Mode: "adopt", Dir: "/workspace/review", Session: "session-review",
				Capability: "inherited",
			},
		},
	}
	deck := configured.Deck()
	m := New(context.Background(), &deck, Options{NoGit: true, Theme: "dark"})
	m.w, m.h, m.frame = width, height, 0
	m.connections = map[string]string{"/workspace/project": "live", "/workspace/review": "live"}
	m.teams = &teamUI{Manager: &team.Manager{Team: configured}, MemberIndex: 1}
	for i := range m.panes {
		p := &m.panes[i]
		p.State = state.NewWithClock(func() time.Time { return now }, time.Minute)
		p.State.SetStatus("idle", now.Add(-time.Minute))
		p.Git = "main · clean"
		p.ContextLimit = 128000
		p.State.ContextTokens = int64(24000 + i*16000)
	}
	m.panes[0].State.Upsert(&api.Part{
		ID: "plan", MessageID: "message-sol", Type: "text", Role: "assistant",
		Text: "## Review plan\nVerify health signals, then integrate the worker changes.\n\n" +
			"- Run package tests\n- Review pending permissions",
	})
	m.panes[1].State.SetStatus("busy", now.Add(-2*time.Minute))
	m.panes[1].State.Upsert(&api.Part{
		ID: "work", MessageID: "message-health", Type: "text", Role: "assistant",
		Text: "The reducer tests pass. Waiting for permission to run the race checks.",
	})
	m.panes[1].State.UpdateRequests([]api.Permission{{
		ID: "permission-race", SessionID: "session-health",
		Permission: "bash", Patterns: []string{"go test -race ./internal/state/..."},
	}}, nil, now.Add(-30*time.Second))
	var question api.Question
	if err := json.Unmarshal([]byte(`{"id":"question-scope","sessionID":"session-review","questions":[
		{"header":"Scope","question":"Review all changes or only health?","options":[
		{"label":"Health","description":"Review reducer and nudge logic"}]}]}`), &question); err != nil {
		t.Fatal(err)
	}
	m.panes[2].State.UpdateRequests(nil, []api.Question{question}, now.Add(-20*time.Second))
	m.note = "Worker reports ready · 2 members need input"
	m.inbox.Reports = map[InboxIdentity][]InboxReport{
		{Agent: "Sol", Directory: "/workspace/project", Session: "session-sol"}: {
			{ID: "report-health", Text: "Package tests passed; integration review pending.", At: now.Add(-time.Minute)},
		},
	}
	return m
}

func goldenScreenView(m *Model, screen string) string {
	switch screen {
	case "team":
		m.teams.Screen = "members"
	case "inbox":
		m.inbox.Open = true
	case "palette":
		return m.paletteView(&PaletteScreen{Open: true, Query: "permission"})
	}
	return m.View()
}

func assertScreenGolden(t *testing.T, name, view string) {
	t.Helper()
	lines := strings.Split(ansi.Strip(view), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	actual := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	filename := filepath.Join("testdata", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(filename, []byte(actual), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read golden (regenerate explicitly with UPDATE_GOLDEN=1): %v", err)
	}
	if actual != string(want) {
		t.Fatalf("screen differs from %s; inspect the view before updating:\n%s", filename, actual)
	}
}
