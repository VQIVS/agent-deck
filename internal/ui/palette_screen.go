package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// PaletteScreen holds search and selection; action execution remains in guarded UI dispatch.
type PaletteScreen struct {
	Open     bool
	Query    string
	Selected string
	Offset   int
	Note     string
}

// PaletteAction requests an existing UI action after availability has been revalidated.
type PaletteAction struct {
	ID        string
	Server    string
	Identity  InboxIdentity
	RequestID string
}

func (m *Model) paletteContext() PaletteContext {
	ctx := PaletteContext{
		ReadOnly: m.opts.ReadOnly,
		HasTeam:  m.teams != nil && m.teams.Manager != nil,
	}
	if m.focus >= 0 && m.focus < len(m.panes) {
		ctx.HasSession = m.panes[m.focus].Agent.Session != ""
		ctx.HasPermission = len(m.panes[m.focus].State.Permissions) > 0
	}
	return ctx
}

func (s *PaletteScreen) matches() []PaletteCommand {
	matches := MatchPalette(PaletteCommands(), s.Query)
	for _, command := range matches {
		if command.ID == s.Selected {
			return matches
		}
	}
	s.Selected = ""
	if len(matches) > 0 {
		s.Selected = matches[0].ID
	}
	return matches
}

func (s *PaletteScreen) move(delta int) {
	matches := s.matches()
	for i, command := range matches {
		if command.ID == s.Selected {
			s.Selected = matches[max(0, min(len(matches)-1, i+delta))].ID
			break
		}
	}
}

func (m *Model) paletteKey(screen *PaletteScreen, key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		screen.Open = false
	case "up":
		screen.move(-1)
	case "down":
		screen.move(1)
	case "enter":
		return m.paletteChoose(screen)
	default:
		editInboxText(&screen.Query, key)
		screen.Note = ""
		screen.matches()
	}
	return nil
}

func (m *Model) paletteChoose(screen *PaletteScreen) tea.Cmd {
	for _, command := range screen.matches() {
		if command.ID != screen.Selected {
			continue
		}
		if reason := command.Disabled(m.paletteContext()); reason != "" {
			screen.Note = command.Title + ": " + reason
			return nil
		}
		screen.Open = false
		action := PaletteAction{ID: command.ID, Server: m.deck.Server}
		if command.Session || command.Permission {
			a := m.panes[m.focus].Agent
			action.Identity = InboxIdentity{Agent: a.Name, Directory: a.Dir, Session: a.Session}
			if command.Permission {
				action.RequestID = m.panes[m.focus].State.Permissions[0].ID
			}
		}
		return func() tea.Msg { return action }
	}
	return nil
}

func (m *Model) dispatchPalette(action *PaletteAction) tea.Cmd {
	if action.Server != m.deck.Server {
		m.note = "Palette target server changed"
		return nil
	}
	for _, command := range PaletteCommands() {
		if command.ID != action.ID {
			continue
		}
		if command.Session || command.Permission {
			index := m.inboxPane(&InboxEntry{Identity: action.Identity})
			if index < 0 {
				m.note = "Palette target session changed"
				return nil
			}
			m.focus = index
		}
		if reason := command.Disabled(m.paletteContext()); reason != "" {
			m.note = "Palette action disabled: " + reason
			return nil
		}
		if command.Permission && (action.RequestID == "" ||
			m.panes[m.focus].State.Permissions[0].ID != action.RequestID) {
			m.note = "Palette permission changed; select it again"
			return nil
		}
		if command.Team && strings.HasPrefix(command.ID, "team-") {
			return m.teamAction(command.ID)
		}
		return m.paletteWorkspaceAction(command.ID)
	}
	m.note = "Unknown palette command"
	return nil
}

func (m *Model) paletteWorkspaceAction(id string) tea.Cmd {
	if !strings.HasPrefix(id, "view:") {
		m.openWorkspace("dashboard")
	}
	return m.clickAction(id)
}

func (m *Model) paletteClick(screen *PaletteScreen, target zone, ok bool) tea.Cmd {
	if !ok || !strings.HasPrefix(target.Action, "palette:") {
		return nil
	}
	id := strings.TrimPrefix(target.Action, "palette:")
	for _, command := range screen.matches() {
		if command.ID == id {
			screen.Selected = id
			return m.paletteChoose(screen)
		}
	}
	return nil
}

func (m *Model) paletteView(screen *PaletteScreen) string {
	commands := screen.matches()
	rows := max(0, m.h-3)
	selected := 0
	for i, command := range commands {
		if command.ID == screen.Selected {
			selected = i
		}
	}
	screen.Offset = max(0, min(selected-rows/2, len(commands)-rows))
	m.zones = nil
	lines := []string{RenderInboxLine("Commands > "+screen.Query, m.w)}
	ctx := m.paletteContext()
	for i := screen.Offset; i < len(commands) && len(lines) <= rows; i++ {
		command := commands[i]
		marker := "  "
		if command.ID == screen.Selected {
			marker = "› "
		}
		text := fmt.Sprintf("%s%s · %s", marker, command.Title, command.Key)
		if reason := command.Disabled(ctx); reason != "" {
			text += " [" + reason + "]"
		}
		m.zones = append(m.zones, zone{
			Rect:   Rect{X: 0, Y: len(lines), W: m.w, H: 1},
			Action: "palette:" + command.ID, Pane: -1,
		})
		lines = append(lines, RenderInboxLine(text, m.w))
	}
	if len(commands) == 0 && rows > 0 {
		lines = append(lines, RenderInboxLine("No matching commands", m.w))
	}
	lines = append(lines, RenderInboxLine("↑↓ select · Enter run · Esc close", m.w), RenderInboxLine(screen.Note, m.w))
	return strings.Join(lines[:min(len(lines), max(0, m.h))], "\n")
}
