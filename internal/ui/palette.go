package ui

import (
	"fmt"
	"strings"
)

// PaletteContext describes current action availability without retaining model state.
type PaletteContext struct {
	ReadOnly      bool
	HasSession    bool
	HasTeam       bool
	HasPermission bool
}

// PaletteCommand is shared metadata for command search and generated keyboard help.
type PaletteCommand struct {
	ID         string
	Title      string
	Key        string
	Group      string
	Mutates    bool
	Session    bool
	Team       bool
	Permission bool
}

func PaletteCommands() []PaletteCommand {
	return []PaletteCommand{
		{ID: "view:git", Title: "Open Git diff and Vim file browser", Key: "2", Group: "Workspace"},
		{ID: "view:summary", Title: "Open team history summary", Key: "3", Group: "Workspace"},
		{ID: "p", Title: "Choose sessions", Key: "p", Group: "Workspace"},
		{ID: "i", Title: "Open inbox", Key: "i", Group: "Workspace"},
		{ID: "b", Title: "Open work board", Key: "b", Group: "Workspace", Team: true},
		{ID: "c", Title: "Open team channel", Key: "c", Group: "Workspace", Team: true},
		{ID: "team-members", Title: "Manage team", Group: "Workspace", Team: true},
		{ID: "team-add", Title: "Add worker", Group: "Workspace", Team: true, Mutates: true},
		{ID: "r", Title: "Refresh sessions", Key: "r", Group: "Workspace"},
		{ID: "m", Title: "Message member", Key: "m", Group: "Session", Session: true, Mutates: true},
		{ID: "n", Title: "Nudge member", Key: "n", Group: "Session", Session: true, Mutates: true},
		{ID: "s", Title: "Stop session", Key: "s", Group: "Session", Session: true, Mutates: true},
		{ID: "a", Title: "Attach to session", Key: "a", Group: "Session", Session: true, Mutates: true},
		{ID: "C", Title: "Compact session", Key: "C", Group: "Session", Session: true, Mutates: true},
		{ID: "y", Title: "Approve permission once", Key: "y", Group: "Requests", Permission: true, Mutates: true},
		{ID: "Y", Title: "Always approve permission", Key: "Y", Group: "Requests", Permission: true, Mutates: true},
		{ID: "x", Title: "Reject permission", Key: "x", Group: "Requests", Permission: true, Mutates: true},
		{ID: "R", Title: "Toggle reasoning", Key: "R", Group: "Display", Session: true},
		{ID: "N", Title: "Toggle notifications", Key: "N", Group: "Display"},
		{ID: "?", Title: "Show help", Key: "?", Group: "Display"},
		{ID: "q", Title: "Quit", Key: "q", Group: "Workspace"},
	}
}

func (c PaletteCommand) Disabled(ctx PaletteContext) string {
	switch {
	case c.Mutates && ctx.ReadOnly:
		return "read-only"
	case c.Session && !ctx.HasSession:
		return "no session"
	case c.Team && !ctx.HasTeam:
		return "no managed team"
	case c.Permission && (!ctx.HasSession || !ctx.HasPermission):
		return "no pending permission"
	default:
		return ""
	}
}

func MatchPalette(commands []PaletteCommand, query string) []PaletteCommand {
	words := strings.Fields(strings.ToLower(query))
	result := make([]PaletteCommand, 0, len(commands))
	for _, command := range commands {
		text := strings.ToLower(command.Title + " " + command.Group + " " + command.Key)
		matches := true
		for _, word := range words {
			if !strings.Contains(text, word) {
				matches = false
				break
			}
		}
		if matches {
			result = append(result, command)
		}
	}
	return result
}

func PaletteHelp(commands []PaletteCommand, ctx PaletteContext, width int) string {
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		key := command.Key
		if key == "" {
			key = "palette"
		}
		line := fmt.Sprintf("%-8s %s", key, command.Title)
		if reason := command.Disabled(ctx); reason != "" {
			line += " [" + reason + "]"
		}
		lines = append(lines, RenderInboxLine(line, width))
	}
	return strings.Join(lines, "\n")
}
