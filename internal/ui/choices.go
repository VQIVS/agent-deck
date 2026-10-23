package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (u *teamUI) choiceMatches() []string {
	var matches []string
	for _, choice := range u.Fields[u.Field].Choices {
		if fuzzy(u.ChoiceQuery, choice) {
			matches = append(matches, choice)
		}
	}
	return matches
}

func (m *Model) choiceKey(v tea.KeyMsg) tea.Cmd {
	u := m.teams
	switch v.String() {
	case "up":
		u.Index = max(0, u.Index-1)
	case "down":
		u.Index = min(max(0, len(u.choiceMatches())-1), u.Index+1)
	case "enter":
		if len(u.choiceMatches()) > 0 {
			return m.teamAction(fmt.Sprintf("choice:%d", u.Index))
		}
	case "backspace", "ctrl+h":
		query := []rune(u.ChoiceQuery)
		if len(query) > 0 {
			u.ChoiceQuery = string(query[:len(query)-1])
		}
		u.Index = 0
	case "ctrl+u":
		u.ChoiceQuery, u.Index = "", 0
	default:
		if v.Type == tea.KeyRunes {
			u.ChoiceQuery += string(v.Runes)
			u.Index = 0
		}
	}
	return nil
}
