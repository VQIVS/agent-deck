package ui

import (
	"strings"
	"testing"

	"github.com/qivs/agent-deck/internal/config"
	"github.com/qivs/agent-deck/internal/team"
)

func TestAdoptedFormPreservesInheritedAndExplicitPreset(t *testing.T) {
	m := transferModel(t)
	v := team.FromDeck(
		&config.Deck{
			Server:   "http://localhost:4096",
			Sessions: []config.Agent{{Name: "Lead", Role: "lead", Session: "session-lead", Dir: "/tmp/project"}},
		},
		"Example",
	)
	version, err := m.teams.Store.Save(t.Context(), &v, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.teams.Manager = &team.Manager{Store: m.teams.Store, Team: v, Version: version}
	m.teamAction("member-edit")
	if !strings.Contains(m.View(), "Permissions apply only to newly created sessions") {
		t.Fatal(m.View())
	}
	if cmd := m.teamAction("form-save"); cmd != nil {
		t.Fatal("editing inherited permissions must not send roster")
	}
	got, _, err := m.teams.Store.Load(t.Context(), "Example")
	if err != nil {
		t.Fatal(err)
	}
	if got.Members[0].Capability != "inherited" || len(got.Members[0].Permissions) != 0 {
		t.Fatal(got)
	}
	if !strings.Contains(m.View(), "inherited from session") {
		t.Fatal(m.View())
	}
	m.teamAction("member-edit")
	for i := range m.teams.Fields {
		if m.teams.Fields[i].Label == "Permission preset" {
			m.teams.Fields[i].Value = "coder"
		}
	}
	if cmd := m.teamAction("form-save"); cmd != nil {
		t.Fatal("preset save must be local only")
	}
	got, _, err = m.teams.Store.Load(t.Context(), "Example")
	if err != nil {
		t.Fatal(err)
	}
	if got.Members[0].Capability != "coder" || got.Members[0].Permissions["edit"] != "allow" {
		t.Fatal(got)
	}
	if !strings.Contains(m.View(), "applies only to newly created sessions") {
		t.Fatal(m.View())
	}
}
