package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// RenderInbox returns a bounded plain-text view; actions remain with the caller's guarded dispatch.
func RenderInbox(entries []InboxEntry, selectedID string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if len(entries) == 0 {
		return ansi.Truncate("Inbox empty", width, "…")
	}
	selected := 0
	for i := range entries {
		if entries[i].ID == selectedID {
			selected = i
			break
		}
	}
	rows := max(1, height/2)
	start := max(0, min(selected-rows/2, len(entries)-rows))
	lines := make([]string, 0, min(height, len(entries)*2))
	for i := start; i < len(entries) && len(lines) < height; i++ {
		entry := entries[i]
		marker := "  "
		if entry.ID == selectedID {
			marker = "› "
		}
		title := fmt.Sprintf("%s[%s] %s · %s", marker, entry.Kind, entry.Identity.Agent, entry.Title)
		lines = append(lines, ansi.Truncate(inboxLine(title), width, "…"))
		if len(lines) < height {
			detail := "  " + entry.Identity.Directory + " · " + entry.Identity.Session + " · " + entry.Detail
			lines = append(lines, ansi.Truncate(inboxLine(detail), width, "…"))
		}
	}
	return strings.Join(lines, "\n")
}

func inboxLine(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, ansi.Strip(text))
}
