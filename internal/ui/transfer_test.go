package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/qivs/agent-deck/internal/config"
	api "github.com/qivs/agent-deck/internal/opencode"
	"github.com/qivs/agent-deck/internal/team"
)

func transferModel(t *testing.T) *Model {
	t.Helper()
	m := New(context.Background(), &config.Deck{Server: "http://localhost:4096"}, Options{NoGit: true, Theme: "dark"})
	m.EnableTeams(t.TempDir())
	return m
}

func TestUIImportPreviewAndConflict(t *testing.T) {
	m := transferModel(t)
	path := filepath.Join(t.TempDir(), "example.yaml")
	deck := []byte("sessions:\n  - {name: Lead, role: lead, session: session-lead, dir: /tmp/project}\n")
	if err := os.WriteFile(path, deck, 0o600); err != nil {
		t.Fatal(err)
	}
	m.teamAction("team-import")
	m.input.SetValue(path)
	m.teamAction("import-file")
	if m.teams.Screen != "import-preview" {
		t.Fatal(m.note)
	}
	if _, _, err := m.teams.Store.Load(t.Context(), "example"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("saved before preview confirmation")
	}
	m.teamAction("import-save")
	v, old, err := m.teams.Store.Load(t.Context(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if v.Members[0].Mode != "adopt" || v.Members[0].Owned {
		t.Fatal(v)
	}
	m.teamAction("team-import")
	m.input.SetValue(path)
	m.teamAction("import-file")
	if !strings.Contains(m.teams.Transfer.Preview, "--- Before") {
		t.Fatal("replacement lacks diff")
	}
	v.Rules = "concurrent change"
	if _, err = m.teams.Store.Save(t.Context(), &v, old); err != nil {
		t.Fatal(err)
	}
	m.teamAction("import-save")
	if !strings.Contains(m.note, "changed on disk") {
		t.Fatal(m.note)
	}
	m.opts.ReadOnly = true
	m.teamAction("import-save")
	got, _, _ := m.teams.Store.Load(t.Context(), "example")
	if got.Rules != "concurrent change" {
		t.Fatal("read-only overwritten")
	}
}

func TestLiveSelectionAndExportConfirmation(t *testing.T) {
	m := transferModel(t)
	m.teamAction("team-import")
	x := &m.teams.Transfer
	x.Name = "Live team"
	x.Server = "http://localhost:4096"
	x.Live = []api.Session{
		{ID: "session-lead", Title: "Lead", Directory: "/tmp/project"},
		{ID: "session-worker", Title: "Worker", Directory: "/tmp/worker"},
	}
	m.teams.Screen = "import-live"
	m.teams.Index = 1
	m.handleTeams(teamsMsg{Status: map[string]teamStatus{}})
	if m.teams.Index != 1 {
		t.Fatal("background summaries changed live selection")
	}
	m.teams.Index = 0
	m.teamAction("live-preview")
	if m.teams.Screen != "import-live" {
		t.Fatal("missing master accepted")
	}
	m.teamAction("live-master")
	m.teams.Index = 1
	m.teamAction("live-toggle")
	m.teamAction("live-preview")
	if m.teams.Screen != "import-preview" || len(x.Draft.Members) != 2 {
		t.Fatal(m.note)
	}
	m.teamAction("import-save")
	m.teams.Teams = []team.Team{x.Draft}
	m.teams.Index = 0
	m.teamAction("team-export")
	out := filepath.Join(t.TempDir(), "team.yaml")
	m.input.SetValue(out)
	if err := os.WriteFile(out, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.teamAction("export-save")
	if m.teams.Confirm != "export-replace" {
		t.Fatal("missing confirmation")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Replace export file") {
		t.Fatal(view)
	}
	b, _ := os.ReadFile(out)
	if string(b) != "existing" {
		t.Fatal("clobbered before confirmation")
	}
	m.teamKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	b, _ = os.ReadFile(out)
	if !strings.Contains(string(b), "agent-deck/team") {
		t.Fatal("not exported", m.note)
	}
}

func TestThemedPickerAndHitTargets(t *testing.T) {
	for _, mode := range []string{"dark", "light", "auto"} {
		m := transferModel(t)
		m.theme, _ = ResolveTheme(mode)
		m.w, m.h = 100, 32
		view := ansi.Strip(m.View())
		if mode == "dark" {
			t.Log("Empty picker:\n" + view)
		}
		if !strings.Contains(view, "Your next team starts here") || !strings.Contains(view, "[Import]") ||
			!strings.Contains(view, "╭") {
			t.Fatal(view)
		}
		for _, z := range m.zones {
			if z.X < 0 || z.X+z.W > m.w || z.Y < 0 || z.Y >= m.h {
				t.Fatalf("offscreen zone %+v", z)
			}
		}
		m.teams.Teams = []team.Team{{Name: "Example", Members: make([]team.Member, 3)}}
		m.teams.Status = map[string]teamStatus{
			"Example": {Busy: 1, Idle: 1, Waiting: 1, Last: time.Now().Add(-3 * time.Minute)},
		}
		view = ansi.Strip(m.View())
		if mode == "dark" {
			t.Log("Populated picker:\n" + view)
		}
		for _, text := range []string{"Example", "3 members", "1 busy", "1 idle", "1 waiting", "3m ago", "Enter open"} {
			if !strings.Contains(view, text) {
				t.Fatalf("missing %s\n%s", text, view)
			}
		}
		for _, z := range m.zones {
			if z.Action == "team-import" {
				m.mouse(tea.MouseMsg{X: z.X, Y: z.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				break
			}
		}
		if m.teams.Screen != "import-form" {
			t.Fatal("centered hit target missed")
		}
	}
	if _, err := ResolveTheme("typo"); err == nil {
		t.Fatal("invalid theme accepted")
	}
}

func TestDashboardReplacementContext(t *testing.T) {
	m := transferModel(t)
	m.teams.Manager = &team.Manager{Team: team.Team{Server: "http://localhost:4096"}}
	m.loadDashboard()
	m.loadDashboard()
	if m.context().Err() != nil {
		t.Fatal("replacement inherited canceled child context")
	}
}
