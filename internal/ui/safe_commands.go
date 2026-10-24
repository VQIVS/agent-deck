package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
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
		if memberCanEdit(t, p.Agent.Dir, p.Agent.Session) {
			continue
		}
		for _, request := range p.State.Permissions {
			command := m.permissionCommand(i, request.Tool.CallID)
			if !list.AllowsRequest(request.Permission, []string{command}, false) || m.safeReplies[request.ID] {
				continue
			}
			m.safeReplies[request.ID] = true
			dir, id, session := p.Agent.Dir, request.ID, p.Agent.Session
			flights := m.permissionFlights()
			return m.autoReplyCommand(flights, dir, session, id, command)
		}
	}
	return nil
}

func (m *Model) autoReplyCommand(flights *InboxFlights, dir, session, id, command string) []tea.Cmd {
	if !flights.Begin(dir, session, id) {
		delete(m.safeReplies, id)
		return nil
	}
	return []tea.Cmd{func() tea.Msg {
		err := logAutoApproval(dir, session, command)
		if err == nil {
			err = m.client.ReplyPermission(m.context(), dir, id, "once", "Exact command allowlist")
		}
		flights.Finish(dir, session, id, err)
		return permissionReplyMsg{dir, id, err}
	}}
}

func logAutoApproval(dir, session, command string) error {
	root, err := server.StateRoot()
	if err != nil {
		return fmt.Errorf("approval audit state: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create approval audit: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(root, "approvals.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open approval audit: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := json.NewEncoder(file).Encode(map[string]any{
		"time": time.Now().UTC(), "session": session,
		"directory": dir, "command": command, "action": "approve_once_attempt",
	}); err != nil {
		return fmt.Errorf("write approval audit: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync approval audit: %w", err)
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
		var input map[string]json.RawMessage
		if json.Unmarshal(part.State.Input, &input) == nil {
			for key := range input {
				if key != "command" && key != "description" && key != "timeout" {
					return ""
				}
			}
			var command string
			if json.Unmarshal(input["command"], &command) == nil {
				return command
			}
		}
	}
	return ""
}

func memberCanEdit(t *team.Team, dir, session string) bool {
	verified := false
	for i := range t.Members {
		member := &t.Members[i]
		if member.Dir != dir {
			continue
		}
		if member.ProjectEditUntrusted || member.Capability == "inherited" || member.Permissions["edit"] != "deny" {
			return true
		}
		if member.Session != "" && (member.LeasePolicy == "" || member.LeasePolicy != t.LeaseFingerprint(member)) {
			return true
		}
		if member.Session == session {
			verified = true
		}
	}
	return !verified
}
