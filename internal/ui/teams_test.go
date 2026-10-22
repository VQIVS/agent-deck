package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qivs/agent-deck/internal/config"
	"github.com/qivs/agent-deck/internal/team"
)

func TestTeamFormSaveAndReadOnly(t *testing.T) {
	m := New(context.Background(), &config.Deck{}, Options{NoGit: true})
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
	m.teamAction("form-save")
	if m.teams.Confirm != "start" {
		t.Fatal("new worker must require explicit start")
	}
	view := m.teamView()
	if !strings.Contains(view, "[Yes]") || !strings.Contains(view, "[No]") {
		t.Fatal(view)
	}
	m.teamAction("team-no")
	m.opts.ReadOnly = true
	m.teams.Store.ReadOnly = true
	m.teams.Screen = "members"
	view = m.teamView()
	for _, z := range m.zones {
		if z.Action == "team-add" || z.Action == "member-start" || z.Action == "member-remove" {
			t.Fatal("mutation zone in read-only")
		}
	}
	if !strings.Contains(view, "[Start]") {
		t.Fatal(view)
	}
}

func TestTeamDeletionConfirmationAndExternalReload(t *testing.T) {
	m := New(context.Background(), &config.Deck{}, Options{})
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
