package ui

import (
	"github.com/vqivs/agent-deck/internal/config"
	"github.com/vqivs/agent-deck/internal/team"
)

// Agent binds a dashboard pane to a session and its code directory.
type Agent = config.Agent

// Deck is the in-memory dashboard input, not a file format.
type Deck = config.Deck

func TeamDeck(current *team.Team) Deck {
	d := Deck{Server: current.Server}
	for i := range current.Members {
		member := &current.Members[i]
		if member.Session == "" {
			continue
		}
		a := Agent{
			Name: member.Name, Session: member.Session, Dir: member.SessionDir(), CodeDir: member.Dir,
			Branch: member.Branch, Base: current.Base,
		}
		if member.Role == "master" {
			a.Role = "lead"
		}
		d.Sessions = append(d.Sessions, a)
	}
	return d
}
