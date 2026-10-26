package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) composerWorkspaceView() string {
	var rows []string
	rows = append(rows, m.workspaceTabs(m.w))
	index := m.targetPane(m.composer.Target)
	if index >= 0 && m.h >= 18 {
		height := max(5, m.h/3)
		rows = append(rows, strings.Split(m.renderPane(index, 0, 1, m.w, height), "\n")...)
	}
	start := len(rows)
	oldHeight := m.h
	m.h = max(1, oldHeight-start)
	body := m.composerView()
	m.h = oldHeight
	for i := range m.zones {
		if m.zones[i].Pane == -1 && m.zones[i].Y < oldHeight-start {
			if m.zones[i].Action == "input" || m.zones[i].Action == "send" ||
				m.zones[i].Action == "discard" || m.zones[i].Action == "esc" {
				m.zones[i].Y += start
			}
		}
	}
	rows = append(rows, strings.Split(body, "\n")...)
	if len(rows) > oldHeight {
		rows = rows[:oldHeight]
	}
	return strings.Join(rows, "\n")
}

func (m *Model) composerView() string {
	c := &m.composer
	t := &m.theme
	if c.Identity {
		return m.composerIdentityView()
	}
	_, sending := c.Pending[c.Target]
	rows := []string{
		t.title(fit("Send message / pinned recipient: "+c.Target.Member, m.w)),
		t.dim(fit("Server: "+c.Target.Server+" / session "+c.Target.Session+" / member "+c.Target.Member, m.w)),
		t.dim(fit("API directory: "+c.Target.Dir, m.w)),
		m.recipientNotice(),
		"",
		t.dim(fit("Draft / Enter sends / Ctrl+J newline / Ctrl+I full identity", m.w)),
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

func (m *Model) composerIdentityView() string {
	c := &m.composer
	checkout := "unavailable"
	if i := m.targetPane(c.Target); i >= 0 {
		checkout = valueOr(m.panes[i].Agent.CodeDir, m.panes[i].Agent.Dir)
	}
	text := "Server: " + c.Target.Server + "\nAPI directory: " + c.Target.Dir +
		"\nSession: " + c.Target.Session + "\nMember: " + c.Target.Member + "\nCode checkout: " + checkout
	lines := strings.Split(ansi.Hardwrap(text, max(1, m.w), true), "\n")
	page := max(1, m.h-2)
	c.IdentityOffset = min(c.IdentityOffset, max(0, len(lines)-page))
	end := min(len(lines), c.IdentityOffset+page)
	return strings.Join(append(lines[c.IdentityOffset:end], "j/k scroll · Esc back to draft"), "\n")
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
	case strings.HasPrefix(status, "Outcome unknown"):
		style = style.Foreground(m.theme.Danger)
	case strings.HasPrefix(status, "Recipient") || strings.HasPrefix(status, "READ ONLY") || status == "Type a message":
		style = style.Foreground(m.theme.Warning)
	}
	return style.Render(fit(status, m.w))
}
