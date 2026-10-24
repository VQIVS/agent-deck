package ui

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qivs/agent-deck/internal/team"
)

func (m *Model) safeCommandReplies() []tea.Cmd {
	if m.opts.ReadOnly || m.teams == nil || m.teams.Manager == nil {
		return nil
	}
	t := &m.teams.Manager.Team
	if t.Approval != "allowlist" {
		return nil
	}
	list, err := team.NewCommandAllowlist(t.SafeCommands)
	if err != nil {
		m.note = err.Error()
		return nil
	}
	for i := range m.panes {
		p := &m.panes[i]
		if !configuredMember(t, p.Agent.Dir, p.Agent.Session) {
			continue
		}
		for _, request := range p.State.Permissions {
			command := m.permissionCommand(i, request.Tool.CallID)
			if !list.AllowsRequest(request.Permission, []string{command}, false) || m.safeReplies[request.ID] {
				continue
			}
			m.safeReplies[request.ID] = true
			dir, id := p.Agent.Dir, request.ID
			return []tea.Cmd{func() tea.Msg {
				err := m.client.ReplyPermission(m.context(), dir, id, "once", "Exact command allowlist")
				return permissionReplyMsg{dir, id, err}
			}}
		}
	}
	return nil
}

func configuredMember(t *team.Team, dir, session string) bool {
	for i := range t.Members {
		member := &t.Members[i]
		if member.Session == session && member.Dir == dir {
			return true
		}
	}
	return false
}

func (m *Model) permissionCommand(index int, callID string) string {
	if callID == "" {
		return ""
	}
	for i := range m.panes[index].State.Parts {
		part := &m.panes[index].State.Parts[i]
		if part.CallID != callID || part.Tool != "bash" {
			continue
		}
		var input struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(part.State.Input, &input) == nil {
			return input.Command
		}
	}
	return ""
}
