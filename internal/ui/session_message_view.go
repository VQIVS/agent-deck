package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) composerView() string {
	c := &m.composer
	t := &m.theme
	_, sending := c.Pending[c.Target]
	rows := []string{
		t.title(fit("Send message / pinned recipient: "+c.Target.Member, m.w)),
		t.dim(fit("Server: "+c.Target.Server+" / session "+c.Target.Session+" / member "+c.Target.Member, m.w)),
		t.dim(fit("Code checkout: "+c.Target.Dir, m.w)),
		m.recipientNotice(),
		"",
		t.dim(fit("Draft / Enter sends / Ctrl+J newline", m.w)),
	}
	m.input.SetWidth(max(10, m.w-2))
	m.input.SetHeight(min(6, max(1, m.h-14)))
	input := m.input.View()
	m.zones = append(m.zones, zone{Rect: Rect{0, len(rows), m.w, lipgloss.Height(input)}, Action: "input", Pane: -1})
	rows = append(rows, input)
	y := len(rows) - 1 + lipgloss.Height(input)
	buttons := []button{
		{"Enter Send to " + c.Target.Member, "send", m.opts.ReadOnly},
		{"Esc Cancel, keep draft", "esc", false},
		{"Ctrl+X Discard draft", "discard", false},
	}
	switch {
	case c.Discarding:
		buttons = []button{{"Enter Discard", "send", false}, {"Esc Keep", "esc", false}}
		rows = append(rows, t.dim("Discard this draft?"))
		y++
	case sending:
		buttons = []button{{"Sending... / duplicate sends disabled", "sending", true}, buttons[1]}
	}
	rows = append(rows, m.buttonRow(buttons, 0, y, m.w, -1))
	return strings.Join(append(rows, m.composerStatus()), "\n")
}

func (m *Model) recipientNotice() string {
	index := m.targetPane(m.composer.Target)
	style := lipgloss.NewStyle().Foreground(m.theme.Muted)
	text := "Same recipient while focus, pane order or size changes."
	switch {
	case m.opts.ReadOnly:
		style, text = style.Foreground(m.theme.Warning), "READ ONLY: messaging unavailable"
	case index < 0:
		style, text = style.Foreground(m.theme.Warning), "Recipient changed/unavailable; draft kept for original target"
	case m.panes[index].State.Waiting():
		style = style.Foreground(m.theme.Waiting)
		text = "WAITING request pending: sending text does not answer it; Esc keeps the draft, then open Inbox"
	case m.panes[index].State.DisplayStatus() == "BUSY":
		text = "BUSY recipient: uses existing send behavior; text does not stop work or approve requests"
	}
	return style.Render(fit(text, m.w))
}

func (m *Model) composerStatus() string {
	status := m.composer.Status
	style := lipgloss.NewStyle().Foreground(m.theme.Muted)
	switch {
	case status == "":
		status = "Draft / not sent / live transcript continues behind this composer"
	case strings.HasPrefix(status, "Send failed"):
		style = style.Foreground(m.theme.Danger)
	case strings.HasPrefix(status, "Recipient") || strings.HasPrefix(status, "READ ONLY") || status == "Type a message":
		style = style.Foreground(m.theme.Warning)
	}
	return style.Render(fit(status, m.w))
}
