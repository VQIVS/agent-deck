package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/team"
)

func teamPickerModel(t *testing.T) *Model {
	t.Helper()
	m := New(context.Background(), &Deck{Server: "http://localhost:4096"}, Options{NoGit: true, Theme: "dark"})
	m.EnableTeams(t.TempDir())
	return m
}

func TestThemedPickerAndHitTargets(t *testing.T) {
	for _, mode := range []string{"dark", "light", "auto"} {
		m := teamPickerModel(t)
		m.theme, _ = ResolveTheme(mode)
		m.w, m.h = 100, 32
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "Your next team starts here") ||
			!strings.Contains(view, "[New team]") || !strings.Contains(view, "╭") {
			t.Fatal(view)
		}
		m.teams.Teams = []team.Team{{Name: "Example", Members: make([]team.Member, 3)}}
		m.teams.Status = map[string]teamStatus{
			"Example": {Busy: 1, Idle: 1, Waiting: 1, Last: time.Now().Add(-3 * time.Minute)},
		}
		view = ansi.Strip(m.View())
		for _, text := range []string{"Example", "3 members", "1 busy", "1 idle", "1 waiting", "3m ago", "Enter open"} {
			if !strings.Contains(view, text) {
				t.Fatalf("missing %s\n%s", text, view)
			}
		}
		for _, z := range m.zones {
			if z.X < 0 || z.X+z.W > m.w || z.Y < 0 || z.Y >= m.h {
				t.Fatalf("offscreen zone %+v", z)
			}
			if z.Action == "team-import" || z.Action == "team-export" {
				t.Fatal("removed transfer action remains")
			}
		}
		for _, z := range m.zones {
			if z.Action == "team-new" {
				m.mouse(tea.MouseMsg{X: z.X, Y: z.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				break
			}
		}
		if m.teams.Screen != "team-form" {
			t.Fatal("centered hit target missed")
		}
	}
	if _, err := ResolveTheme("typo"); err == nil {
		t.Fatal("invalid theme accepted")
	}
}

func TestDashboardReplacementContext(t *testing.T) {
	m := teamPickerModel(t)
	m.teams.Manager = &team.Manager{Team: team.Team{Server: "http://localhost:4096"}}
	m.loadDashboard()
	m.loadDashboard()
	if m.context().Err() != nil {
		t.Fatal("replacement inherited canceled child context")
	}
}

func TestPickerRetainedNavigation(t *testing.T) {
	for _, mouse := range []bool{false, true} {
		for _, action := range []struct{ key, name string }{
			{"n", "team-new"}, {"enter", "team-open"}, {"c", "team-duplicate"}, {"a", "team-all"},
		} {
			m := teamPickerModel(t)
			m.w, m.h = 100, 32
			configured := team.Team{
				Version: 1, Name: "Example", Repo: "/work/agent-deck", Base: "main", Server: "http://localhost:4096", Capacity: 1,
				Members: []team.Member{{
					Name: "Lead", Role: "master", Mode: "shared", Dir: "/work/agent-deck",
					Capability: "coder", Permissions: team.Preset("coder"),
				}},
			}
			if _, err := m.teams.Store.Save(t.Context(), &configured, nil); err != nil {
				t.Fatal(err)
			}
			m.teams.Teams = []team.Team{configured}
			switch {
			case mouse:
				m.View()
				found := false
				for _, z := range m.zones {
					if z.Action == action.name {
						m.mouse(tea.MouseMsg{X: z.X, Y: z.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing %s target", action.name)
				}
			case action.key == "enter":
				m.teamKey(tea.KeyMsg{Type: tea.KeyEnter})
			default:
				m.teamKey(key(action.key))
			}
			switch action.name {
			case "team-new", "team-duplicate":
				if m.teams.Screen != "team-form" {
					t.Fatalf("%s did not open form", action.name)
				}
			case "team-open":
				if m.teams.Manager == nil || m.teams.Screen == "picker" {
					t.Fatal("team did not open")
				}
			case "team-all":
				if !m.opts.Auto || m.teams.Screen != "" {
					t.Fatal("all-session monitoring unavailable")
				}
			}
		}
	}
}

func TestTeamFormSaveAndReadOnly(t *testing.T) {
	m := New(context.Background(), &Deck{}, Options{NoGit: true})
	m.EnableTeams(t.TempDir())
	m.teamAction("team-new")
	values := map[string]string{
		"Team name":                    "UI team",
		"Server URL":                   "http://localhost:4096",
		"Project repository directory": "/tmp/project",
		"Base branch":                  "main",
		"Maximum parallel workers":     "2",
	}
	for i := range m.teams.Fields {
		if v, ok := values[m.teams.Fields[i].Label]; ok {
			m.teams.Fields[i].Value = v
		}
	}
	m.input.SetValue("UI team")
	m.teamAction("form-save")
	if m.teams.Manager == nil || m.teams.Screen != "members" {
		t.Fatal(m.note)
	}
	loaded, _, e := m.teams.Store.Load(t.Context(), "UI team")
	if e != nil || loaded.Members[0].Dir != "/tmp/project" {
		t.Fatal(loaded, e)
	}
	m.teamAction("team-add")
	if m.teams.Screen != "member-form" {
		t.Fatal("no member form")
	}
	m.input.SetValue("Test worker")
	cmd := m.teamAction("form-save")
	if cmd == nil || !m.teams.Busy || m.teams.Confirm != "" {
		t.Fatal("create worker should schedule session creation directly")
	}
	m.teams.Busy = false
	m.opts.ReadOnly = true
	m.teams.Store.ReadOnly = true
	m.teams.Screen = "members"
	view := m.teamView()
	for _, z := range m.zones {
		if z.Action == "team-add" || z.Action == "member-start" || z.Action == "member-remove" {
			t.Fatal("mutation zone in read-only")
		}
	}
	if !strings.Contains(view, "[Start]") {
		t.Fatal(view)
	}
}

func TestDuplicateTeamCreatesFreshMemberships(t *testing.T) {
	m := requestModel("http://unused", false)
	m.EnableTeams(t.TempDir())
	original := team.Team{
		Version: 1, Name: "source", Server: "http://localhost:4096", Repo: "/tmp/project", Base: "main", Capacity: 1,
		Members: []team.Member{{
			Name: "Master", Role: "master", Mode: "shared", Dir: "/tmp/project",
			Capability: "coder", Permissions: team.Preset("coder"),
		}},
	}
	if _, err := m.teams.Store.Save(t.Context(), &original, nil); err != nil {
		t.Fatal(err)
	}
	m.teams.Teams = []team.Team{original}
	m.duplicateTeam()
	if m.teams.Draft.Members[0].ID != "" {
		t.Fatal("duplicate retained original ID")
	}
	m.teamAction("form-save")
	duplicate, _, err := m.teams.Store.Load(t.Context(), "source copy")
	if err != nil || duplicate.Members[0].ID == original.Members[0].ID || duplicate.Members[0].ID == "" {
		t.Fatal(duplicate, err, m.note)
	}
}

func TestTeamDeletionConfirmationAndExternalReload(t *testing.T) {
	m := New(context.Background(), &Deck{}, Options{})
	m.EnableTeams(t.TempDir())
	t1 := team.Team{
		Version:  1,
		Name:     "Team",
		Server:   "http://localhost:4096",
		Repo:     "/tmp/repo",
		Base:     "main",
		Capacity: 1,
		Members: []team.Member{
			{Name: "Lead", Role: "master", Mode: "shared", Dir: "/tmp/repo", Permissions: team.Preset("coder")},
		},
	}
	v, e := m.teams.Store.Save(t.Context(), &t1, nil)
	if e != nil {
		t.Fatal(e)
	}
	m.teams.Teams = []team.Team{t1}
	m.teamAction("team-delete")
	view := m.teamView()
	if !strings.Contains(view, "Delete team file?") || strings.Contains(view, "[New team]") {
		t.Fatal(view)
	}
	m.teamKey(tea.KeyMsg{Type: tea.KeyEsc})
	if _, _, e = m.teams.Store.Load(t.Context(), "Team"); e != nil {
		t.Fatal("cancel deleted")
	}
	m.OpenTeam(&t1, v, m.teams.Store.Root)
	t1.Rules = "external"
	if _, e = m.teams.Store.Save(t.Context(), &t1, v); e != nil {
		t.Fatal(e)
	}
	msg := m.watchTeam()()
	reload, ok := msg.(teamReloadMsg)
	if !ok || reload.Team.Rules != "external" {
		t.Fatalf("%+v", msg)
	}
}
