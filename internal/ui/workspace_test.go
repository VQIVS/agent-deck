package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/channel"
	"github.com/vqivs/agent-deck/internal/gitinfo"
)

func TestWorkspaceTabAndChannelMouseRouting(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w, m.h = 100, 24
	m.View()
	clickWorkspaceTarget(t, m, "view:channel")
	if !m.channel.Open {
		t.Fatal("channel tab did not open")
	}
	m.View()
	clickWorkspaceTarget(t, m, "channel-compose")
	if !m.channel.Editing {
		t.Fatal("compose click not routed")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.channel.Draft != "2" || !m.channel.Open {
		t.Fatal("tab intercepted composition")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.View()
	clickWorkspaceTarget(t, m, "channel-back")
	if m.channel.Open || m.workspace.View != "dashboard" {
		t.Fatal("back did not open dashboard")
	}
}

func clickWorkspaceTarget(t *testing.T, m *Model, action string) {
	t.Helper()
	for _, z := range m.zones {
		if z.Action == action {
			m.mouse(tea.MouseMsg{X: z.X, Y: z.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			return
		}
	}
	t.Fatalf("missing target %s", action)
}

func TestWorkspaceGitMouseSelectionAndWheel(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w, m.h = 100, 24
	m.workspace.View = "git"
	m.changes.Files = []gitinfo.ChangedFile{{Path: "first.go"}, {Path: "second.go"}}
	m.changes.Index = 0
	m.View()
	clickWorkspaceTarget(t, m, "change:1")
	if m.changes.Index != 1 || m.changes.Offset != 0 || m.changes.DiffFocus {
		t.Fatal("file click not routed")
	}
	m.mouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if !m.changes.DiffFocus || m.changes.Offset != 3 {
		t.Fatal("wheel did not scroll diff")
	}
}

func TestWorkspaceSummaryRefreshAndChannelWheel(t *testing.T) {
	m := requestModel("http://unused", false)
	m.EnableChannel(&fakeTeamChannel{})
	if cmd := m.openWorkspace("summary"); cmd == nil || !m.channel.Loading {
		t.Fatal("summary did not load evidence")
	}
	m.channel.Loading = false
	if cmds := m.workspaceRefresh(); len(cmds) != 2 || !m.channel.Loading {
		t.Fatal("summary did not poll evidence")
	}
	m.openWorkspace("channel")
	m.channel.Records = []channel.Record{{Kind: "message", Text: "hello"}}
	m.mouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.channel.Offset != 3 {
		t.Fatal("channel wheel not routed")
	}
}

func TestWorkspaceNavigationAndBoundedScreens(t *testing.T) {
	for _, view := range []string{"dashboard", "git", "summary", "channel"} {
		for _, size := range [][2]int{{0, 0}, {20, 8}, {45, 9}, {80, 24}, {160, 48}} {
			t.Run(fmt.Sprintf("%s_%dx%d", view, size[0], size[1]), func(t *testing.T) {
				m := requestModel("http://unused", false)
				m.workspace.View = view
				m.w, m.h = size[0], size[1]
				text := m.View()
				if lipgloss.Width(text) > m.w || text != "" && lipgloss.Height(text) > m.h {
					t.Fatalf("overflow: %q", text)
				}
			})
		}
	}
	m := requestModel("http://unused", false)
	m.board.Open = true
	m.workspaceKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3"), Alt: true})
	if m.workspace.View != "summary" || m.board.Open {
		t.Fatal("Alt tab did not leave board")
	}
	m.w, m.h = 80, 24
	if !strings.Contains(m.View(), "3 Summary") {
		t.Fatal("tabs not discoverable")
	}
}

func TestWorkspaceModalPrecedenceAndUnboundPaletteKey(t *testing.T) {
	for _, view := range []string{"git", "summary"} {
		m := requestModel("http://unused", false)
		m.workspace.View = view
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
		if m.workspace.View != view || m.modalOpen() {
			t.Fatalf("unbound key changed %s", view)
		}
		m.help = true
		m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if m.help || m.workspace.View != view {
			t.Fatal("modal keys reached hidden feature")
		}
	}
}

func TestWorkspaceBlockedTabAction(t *testing.T) {
	m := requestModel("http://unused", false)
	m.searching = true
	m.clickAction("view:git")
	if m.workspace.View == "git" {
		t.Fatal("blocked tab fell through to dashboard action")
	}
}

func TestRetainedActionsWithoutPalette(t *testing.T) {
	for _, key := range []string{"i", "c", "b", "C"} {
		t.Run(key, func(t *testing.T) {
			m := requestModel("http://unused", false)
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			if (key == "i" && !m.inbox.Open) || (key == "c" && !m.channel.Open) ||
				(key == "b" && !m.board.Open) || (key == "C" && !m.compactConfirm) {
				t.Fatal("retained action is unreachable")
			}
		})
	}
	m := requestModel("http://unused", true)
	if m.sessionKey("n") != nil || m.compact() != nil {
		t.Fatal("read-only permits nudge or compaction")
	}
	if !strings.Contains(m.helpView("Help"), "Ctrl+P unbound") {
		t.Fatal("removed binding is not documented")
	}
}
