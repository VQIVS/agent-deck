package ui

import "github.com/vqivs/agent-deck/internal/team"

// Agent binds a dashboard pane to a session and its code directory.
type Agent struct {
	CodeDir string
	Name    string
	Session string
	Dir     string
	Branch  string
	Base    string
	Role    string
	Parent  string
}

// Deck is the in-memory dashboard input, not a file format.
type Deck struct {
	Server   string
	Master   Agent
	Workers  []Agent
	Sessions []Agent
}

func (d *Deck) Agents() []Agent {
	var out []Agent
	if d.Master.Session != "" {
		a := d.Master
		a.Role = "lead"
		out = append(out, a)
	}
	out = append(out, d.Workers...)
	return append(out, d.Sessions...)
}

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
