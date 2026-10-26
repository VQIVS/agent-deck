package ui

import (
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestTeamDeckSeparatesControlAndCodeDirectories(t *testing.T) {
	current := team.Team{Server: "http://127.0.0.1:4096", Base: "main", Members: []team.Member{
		{Name: "lead", Role: "master", Session: "controlled", ControlDir: "/work/control", Dir: "/work/agent-deck"},
		{Name: "inactive", Role: "worker", Dir: "/work/agent-deck"},
	}}
	deck := TeamDeck(&current)
	if len(deck.Sessions) != 1 || deck.Server != current.Server || deck.Sessions[0].Dir != "/work/control" ||
		deck.Sessions[0].CodeDir != "/work/agent-deck" || deck.Sessions[0].Role != "lead" {
		t.Fatal(deck)
	}
}
