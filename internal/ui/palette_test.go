package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func TestPaletteRegistryAvailabilityAndSearch(t *testing.T) {
	seen := map[string]bool{}
	for _, command := range PaletteCommands() {
		if command.ID == "" || command.Title == "" || seen[command.ID] {
			t.Fatalf("invalid registry entry: %+v", command)
		}
		seen[command.ID] = true
		if command.Mutates && command.Disabled(PaletteContext{
			ReadOnly: true, HasSession: true,
			HasTeam: true, HasPermission: true,
		}) != "read-only" {
			t.Fatal("mutation available read-only")
		}
	}
	matches := MatchPalette(PaletteCommands(), "permission once")
	if len(matches) != 1 || matches[0].ID != "y" {
		t.Fatalf("multiword search: %+v", matches)
	}
	if len(MatchPalette(PaletteCommands(), "no such command")) != 0 {
		t.Fatal("unmatched query returned results")
	}
}

func TestPaletteEmptyGoldenAndNavigation(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	m.w, m.h = 80, 6
	screen := &PaletteScreen{Open: true, Query: "missing command"}
	want := "Commands > missing command\nNo matching commands\n↑↓ select · Enter run · Esc close\n"
	if got := m.paletteView(screen); got != want || len(m.zones) != 0 {
		t.Fatalf("empty golden mismatch: %q", got)
	}
	screen.Query = "permission"
	screen.matches()
	screen.move(1)
	if screen.Selected != "Y" {
		t.Fatal("selection did not advance")
	}
	screen.move(99)
	if screen.Selected != "x" {
		t.Fatal("selection did not clamp")
	}
}

func TestPaletteDispatchRejectsChangedTargets(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	screen := &PaletteScreen{Open: true, Query: "compact"}
	action := m.paletteChoose(screen)().(PaletteAction)
	m.deck.Server = "http://new-server"
	if m.dispatchPalette(&action) != nil || m.compactConfirm {
		t.Fatal("server replacement received old action")
	}
	m = requestModel("http://unused.invalid", false)
	m.panes[0].State.Permissions = []api.Permission{{ID: "original", SessionID: "session-a"}}
	screen = &PaletteScreen{Open: true, Query: "always approve"}
	action = m.paletteChoose(screen)().(PaletteAction)
	m.panes[0].State.Permissions[0].ID = "replacement"
	if m.dispatchPalette(&action) != nil || m.permissionConfirm {
		t.Fatal("permission replacement received old action")
	}
}

func TestPaletteGoldenViewsAndGeneratedHelp(t *testing.T) {
	m := requestModel("http://unused.invalid", true)
	m.w, m.h = 80, 6
	screen := &PaletteScreen{Open: true, Query: "compact"}
	view := m.paletteView(screen)
	want := "Commands > compact\n› Compact session · C [read-only]\n↑↓ select · Enter run · Esc close\n"
	if view != want {
		t.Fatalf("palette golden mismatch:\n%q\nwant:\n%q", view, want)
	}
	commands := []PaletteCommand{
		{ID: "r", Title: "Refresh", Key: "r"},
		{ID: "m", Title: "Message", Key: "m", Mutates: true},
	}
	help := PaletteHelp(commands, PaletteContext{ReadOnly: true}, 80)
	if help != "r        Refresh\nm        Message [read-only]" {
		t.Fatalf("help golden mismatch: %q", help)
	}
	for _, height := range []int{0, 1, 3, 8} {
		m.w, m.h = 12, height
		view = m.paletteView(screen)
		if height > 0 && len(strings.Split(view, "\n")) > height {
			t.Fatal("palette exceeds terminal height")
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > m.w {
				t.Fatal("palette exceeds terminal width")
			}
		}
	}
}

func TestPaletteControllerRevalidatesActions(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	screen := &PaletteScreen{Open: true, Query: "compact"}
	m.paletteView(screen)
	m.opts.ReadOnly = true
	if cmd := m.paletteKey(screen, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || !screen.Open {
		t.Fatal("availability change bypassed guard")
	}
	m.opts.ReadOnly = false
	cmd := m.paletteKey(screen, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || cmd().(PaletteAction).ID != "C" || screen.Open {
		t.Fatal("enabled action not emitted")
	}
	screen = &PaletteScreen{Open: true, Query: "refresh"}
	m.paletteView(screen)
	if len(m.zones) != 1 {
		t.Fatal("palette row targets missing")
	}
	cmd = m.paletteClick(screen, m.zones[0], true)
	if cmd == nil || cmd().(PaletteAction).ID != "r" {
		t.Fatal("mouse dispatched wrong command")
	}
}
