package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// messageTarget pins a composer to one session identity; pane order and focus never retarget it.
type messageTarget struct{ Server, Dir, Session, Member string }

type composerUI struct {
	Target           messageTarget
	Open, Discarding bool
	Status           string
	Drafts           map[messageTarget]string
	Pending          map[messageTarget]string
}

type messageSent struct {
	Target messageTarget
	Text   string
	Err    error
}

func (m *Model) paneTarget(i int) messageTarget {
	a := m.panes[i].Agent
	return messageTarget{Server: m.deck.Server, Dir: a.Dir, Session: a.Session, Member: a.Name}
}

// targetPane finds the pane that still holds the pinned identity, or -1 when it is gone or replaced.
func (m *Model) targetPane(target messageTarget) int {
	for i := range m.panes {
		if m.paneTarget(i) == target {
			return i
		}
	}
	return -1
}

func (m *Model) openComposer(i int) tea.Cmd {
	if i < 0 || i >= len(m.panes) {
		return nil
	}
	c := &m.composer
	c.Target, c.Open, c.Discarding, c.Status = m.paneTarget(i), true, false, ""
	m.editing = true
	m.input.Reset()
	m.input.Placeholder = "Message for " + c.Target.Member
	m.input.SetValue(c.Drafts[c.Target])
	if text, ok := c.Pending[c.Target]; ok {
		m.input.SetValue(text)
		c.Status = "Dispatch in progress for " + c.Target.Member
	}
	m.input.Focus()
	return textarea.Blink
}

func (m *Model) closeComposer(note string) {
	m.composer.Open, m.composer.Discarding = false, false
	m.editing = false
	m.input.Blur()
	if note != "" {
		m.note = note
	}
}

func (m *Model) saveDraft() {
	c := &m.composer
	if c.Drafts == nil {
		c.Drafts = map[messageTarget]string{}
	}
	if _, sending := c.Pending[c.Target]; sending {
		return
	}
	if text := m.input.Value(); strings.TrimSpace(text) != "" {
		c.Drafts[c.Target] = text
	} else {
		delete(c.Drafts, c.Target)
	}
}

func (m *Model) composeKey(v tea.KeyMsg) tea.Cmd {
	c := &m.composer
	key := v.String()
	if c.Discarding {
		c.Discarding = false
		if key == "enter" || key == "ctrl+s" {
			delete(c.Drafts, c.Target)
			m.input.Reset()
			c.Status = "Draft discarded"
		}
		return nil
	}
	_, sending := c.Pending[c.Target]
	switch key {
	case "esc":
		m.saveDraft()
		note := "Draft saved for " + c.Target.Member
		if sending {
			note = "Message to " + c.Target.Member + " still sending"
		} else if strings.TrimSpace(m.input.Value()) == "" {
			note = ""
		}
		m.closeComposer(note)
		return nil
	case "ctrl+x":
		c.Discarding = !sending && strings.TrimSpace(m.input.Value()) != ""
		return nil
	case "ctrl+s", "enter":
		return m.sendComposed()
	}
	if sending {
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(v)
	return cmd
}

func (m *Model) sendComposed() tea.Cmd {
	c := &m.composer
	text := strings.TrimSpace(m.input.Value())
	switch {
	case m.opts.ReadOnly:
		c.Status = "READ ONLY: messaging unavailable"
		return nil
	case c.Pending[c.Target] != "":
		return nil
	case text == "":
		c.Status = "Type a message"
		return nil
	}
	index := m.targetPane(c.Target)
	if index < 0 {
		m.saveDraft()
		c.Status = "Recipient changed/unavailable; draft kept for original target"
		return nil
	}
	if c.Pending == nil {
		c.Pending = map[messageTarget]string{}
	}
	c.Pending[c.Target] = m.input.Value()
	c.Status = "Dispatch in progress for " + c.Target.Member
	target := c.Target
	send := m.promptTo(index, text)
	return func() tea.Msg { return messageSent{Target: target, Text: text, Err: send()} }
}

//nolint:gocritic // Bubble Tea result values retain the dispatch API shared with dashboard.go.
func (m *Model) applyMessageSent(v messageSent) {
	c := &m.composer
	draft := c.Pending[v.Target]
	delete(c.Pending, v.Target)
	current := c.Open && c.Target == v.Target
	if v.Err != nil {
		if c.Drafts == nil {
			c.Drafts = map[messageTarget]string{}
		}
		c.Drafts[v.Target] = draft
		reason := "Send failed; draft kept: " + v.Err.Error()
		if current {
			c.Status = reason
		} else {
			m.note = reason + " (" + v.Target.Member + ")"
		}
		return
	}
	delete(c.Drafts, v.Target)
	note := "Message sent to " + v.Target.Member + " (transport accepted)"
	if current {
		m.input.Reset()
		m.closeComposer(note)
		return
	}
	m.note = note
}
