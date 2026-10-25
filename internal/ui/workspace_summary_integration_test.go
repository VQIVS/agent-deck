package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSummaryWorkspacePickerOwnsInputAndMouse(t *testing.T) {
	m := requestModel("http://unused", false)
	m.workspace.View = "summary"
	m.w, m.h = 80, 24
	m.summary.Mode = "picker"
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.workspace.View != "summary" || m.summary.pickerSearch != "2" {
		t.Fatal("workspace navigation intercepted member search")
	}
	if handled, _ := m.workspaceMouse(zone{Action: "summary:cancel"}, true); !handled || m.summary.Mode != "" {
		t.Fatal("picker blocked its own cancel action")
	}
	m.summary.Mode = "picker"
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if !m.palette.Open {
		t.Fatal("member picker swallowed global command palette")
	}
}

func TestSummaryWorkspaceObservesFailedRefreshWithoutDiscardingCache(t *testing.T) {
	m := requestModel("http://unused", false)
	m.Update(boardResult{})
	loaded := m.summary.boardStatus.LastSuccess
	if !m.summary.boardStatus.Loaded || loaded.IsZero() {
		t.Fatal("successful empty board fetch was not recorded")
	}
	m.Update(boardResult{Err: errors.New("offline")})
	status := m.summary.boardStatus
	if !status.Loaded || status.LastSuccess != loaded || status.Error != "offline" {
		t.Fatalf("failed refresh lost cached fetch metadata: %+v", status)
	}
}

func TestSummaryWorkspaceTinyViewAndActiveTab(t *testing.T) {
	m := requestModel("http://unused", false)
	m.workspace.View = "summary"
	m.w, m.h = 80, 8
	view := m.View()
	if !strings.Contains(view, "[>3 Summary]") || !strings.Contains(view, "Summary needs") {
		t.Fatalf("tiny Summary fell back to dashboard: %s", view)
	}
}
