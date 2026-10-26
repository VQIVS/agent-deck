package cli

import (
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestServerPrecedence(t *testing.T) {
	t.Setenv("AGENT_DECK_SERVER", "http://env")
	if resolveServer("http://flag", "http://team") != "http://flag" ||
		resolveServer("", "http://team") != "http://env" {
		t.Fatal("precedence")
	}
	t.Setenv("AGENT_DECK_SERVER", "")
	if resolveServer("", "http://team") != "http://team" || resolveServer("", "") != "http://127.0.0.1:4096" {
		t.Fatal("defaults")
	}
}

func TestRemovedDeckCommandsAndFlagsFailClearly(t *testing.T) {
	for _, args := range [][]string{{"discover"}, {"new"}, {"--snapshot"}, {"--config", "deck.yaml"}} {
		if err := run(args); err == nil || (!strings.Contains(err.Error(), "unexpected arguments") &&
			!strings.Contains(err.Error(), "flag provided but not defined")) {
			t.Fatal(args, err)
		}
	}
}

func TestSavedTeamAndAllDashboardInputs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGENT_DECK_SERVER", "")
	saved := team.Team{
		Version: 1, Name: "saved", Repo: "/work/agent-deck", Base: "main",
		Server: "http://127.0.0.1:4096", Capacity: 1, Members: []team.Member{{
			Name: "lead", Role: "master", Mode: "shared", Dir: "/work/agent-deck",
			Session: "session-fixture", Permissions: team.Preset("coder"),
		}},
	}
	if _, err := (team.Store{Root: team.DefaultRoot()}).Save(t.Context(), &saved, nil); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--team", "saved"}, {"--all"}, {}} {
		flags, err := parseDashboard(args)
		if err != nil {
			t.Fatal(err)
		}
		deck, current, _, err := loadDeck(t.Context(), &flags)
		if err != nil || deck.Server == "" || (current != nil) != (flags.teamName != "") {
			t.Fatal(deck, current, err)
		}
	}
}
