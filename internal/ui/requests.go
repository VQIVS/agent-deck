package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	api "github.com/qivs/agent-deck/internal/opencode"
)

type requestsMsg struct {
	Dir                        string
	Permissions                []api.Permission
	Questions                  []api.Question
	PermissionErr, QuestionErr error
}
type permissionReplyMsg struct {
	Dir, ID string
	Err     error
}
type permissionTarget struct{ Dir, ID, Description string }

func (m *Model) requests(dir string) tea.Cmd {
	if _, busy := m.requestEvents[dir]; busy {
		return nil
	}
	m.requestEvents[dir] = nil
	return func() tea.Msg {
		p, pe := m.client.Permissions(m.context(), dir)
		q, qe := m.client.Questions(m.context(), dir)
		return requestsMsg{dir, p, q, pe, qe}
	}
}

func (m *Model) applyRequests(v *requestsMsg) []tea.Cmd {
	events := m.requestEvents[v.Dir]
	delete(m.requestEvents, v.Dir)
	if v.PermissionErr != nil {
		m.note = "Permissions: " + v.PermissionErr.Error()
	}
	if v.QuestionErr != nil {
		m.note = "Questions: " + v.QuestionErr.Error()
	}
	var cmds []tea.Cmd
	cmds = append(cmds, m.addWaiting(v)...)
	for i := range m.panes {
		if m.applyPaneRequests(i, v, events) {
			cmds = append(cmds, m.notify(i))
		}
	}
	return cmds
}

func (m *Model) addWaiting(v *requestsMsg) []tea.Cmd {
	var cmds []tea.Cmd
	if m.opts.Auto {
		pending := map[string]bool{}
		for _, r := range v.Permissions {
			pending[r.SessionID] = true
		}
		for _, r := range v.Questions {
			pending[r.SessionID] = true
		}
		for _, s := range m.inventory.Sessions {
			if s.Directory == v.Dir && pending[s.ID] {
				a := agent(s)
				cmds = append(cmds, m.add(&a)...)
			}
		}
	}
	return cmds
}

func (m *Model) applyPaneRequests(i int, v *requestsMsg, events []api.Event) bool {
	p := &m.panes[i]
	if p.Agent.Dir != v.Dir {
		return false
	}
	was := p.State.Waiting()
	since := p.State.WaitingSince
	permissions := p.State.Permissions
	questions := p.State.Questions
	if v.PermissionErr == nil {
		permissions = nil
		for _, r := range v.Permissions {
			if r.SessionID == p.Agent.Session {
				permissions = append(permissions, r)
			}
		}
	}
	if v.QuestionErr == nil {
		questions = nil
		for _, r := range v.Questions {
			if r.SessionID == p.Agent.Session {
				questions = append(questions, r)
			}
		}
	}
	p.State.UpdateRequests(permissions, questions, time.Now())
	for _, e := range events {
		p.State.ApplyRequest(e, p.Agent.Session, time.Now())
	}
	if was && p.State.Waiting() {
		p.State.WaitingSince = since
	}
	return !was && p.State.Waiting()
}

func (m *Model) permissionKey(key string) tea.Cmd {
	if m.opts.ReadOnly {
		m.note = "Read-only: permission replies disabled"
		return nil
	}
	p := m.panes[m.focus]
	if len(p.State.Permissions) == 0 {
		m.note = "No pending permission; answer questions through attach"
		return nil
	}
	r := p.State.Permissions[0]
	m.permissionTarget = permissionTarget{p.Agent.Dir, r.ID, r.Permission + " " + strings.Join(r.Patterns, ", ")}
	switch key {
	case "y":
		return m.replyPermission("once", "")
	case "Y":
		m.permissionConfirm = true
	case "x":
		m.rejecting = true
		m.input.Reset()
		m.input.SetHeight(4)
		m.input.Focus()
		return textarea.Blink
	}
	return nil
}

func (m *Model) replyPermission(reply, message string) tea.Cmd {
	target := m.permissionTarget
	if m.opts.ReadOnly {
		m.note = "Read-only: permission replies disabled"
		return nil
	}
	// A confirmation always refers to the original request, never a replacement.
	valid := false
	for i := range m.panes {
		p := &m.panes[i]
		if p.Agent.Dir == target.Dir {
			for _, r := range p.State.Permissions {
				if r.ID == target.ID {
					valid = true
				}
			}
		}
	}
	if !valid {
		m.note = "Permission is no longer pending"
		return nil
	}
	return func() tea.Msg {
		return permissionReplyMsg{
			target.Dir,
			target.ID,
			m.client.ReplyPermission(m.context(), target.Dir, target.ID, reply, message),
		}
	}
}

func (m *Model) replied(v permissionReplyMsg) tea.Cmd {
	if v.Err != nil {
		m.note = v.Err.Error()
	} else {
		m.note = "Permission reply accepted"
		e := api.Event{Type: "permission.replied"}
		// Polling may predate this successful reply; replay a session-scoped removal.
		for i := range m.panes {
			p := &m.panes[i]
			if p.Agent.Dir != v.Dir {
				continue
			}
			e.Properties = []byte(fmt.Sprintf(`{"sessionID":%q,"requestID":%q}`, p.Agent.Session, v.ID))
			p.State.ApplyRequest(e, p.Agent.Session, time.Now())
			if _, busy := m.requestEvents[v.Dir]; busy {
				m.requestEvents[v.Dir] = append(m.requestEvents[v.Dir], e)
			}
		}
	}
	return m.requests(v.Dir)
}
