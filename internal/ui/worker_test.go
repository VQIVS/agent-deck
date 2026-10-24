package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestCreateWorkerWithModelAndNewSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != "/tmp/project" {
			t.Errorf("wrong directory %s", r.URL)
		}
		switch r.URL.Path {
		case "/config/providers":
			fmt.Fprint(w, `{"providers":[{"id":"example","models":{"fast":{},"smart":{}}}]}`)
		case "/agent":
			fmt.Fprint(w, `[{"name":"build"}]`)
		case "/session":
			fmt.Fprint(w, `{"id":"session-created"}`)
		case "/session/session-created/prompt_async":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	m := transferModel(t)
	saved := team.Team{
		Version: 1, Name: "Example", Repo: "/tmp/project", Base: "main", Server: server.URL, Capacity: 2,
		Members: []team.Member{{
			Name: "Lead", Role: "master", Mode: "shared", Dir: "/tmp/project",
			Permissions: team.Preset("coder"),
		}},
	}
	revision, err := m.teams.Store.Save(t.Context(), &saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.OpenTeam(&saved, revision, m.teams.Store.Root)
	catalog := m.teamAction("team-add")
	m.handleTeams(catalog())
	if m.teams.fieldCount() != 6 || len(m.teams.Models) != 2 {
		t.Fatal("worker basics or model loading missing")
	}
	m.input.SetValue("Tester")
	m.teamAction("form-model")
	if m.teams.Screen != "choices" || !strings.Contains(m.choicesView(), "example/fast") {
		t.Fatal("model picker unavailable")
	}
	m.teamKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("smart")})
	if len(m.teams.choiceMatches()) != 1 {
		t.Fatal("model search did not filter")
	}
	m.teamKey(tea.KeyMsg{Type: tea.KeyEnter})
	cmd := m.teamAction("form-save")
	if cmd == nil {
		t.Fatal(m.note)
	}
	result := cmd().(teamOperationMsg)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	loaded, _, err := m.teams.Store.Load(t.Context(), "Example")
	if err != nil {
		t.Fatal(err)
	}
	worker := loaded.Members[1]
	if worker.Session != "session-created" || worker.Model != "example/smart" || worker.Dir != saved.Repo {
		t.Fatalf("worker bindings not persisted: %+v", worker)
	}
}
