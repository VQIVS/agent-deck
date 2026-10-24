package ui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/channel"
)

func (m *Model) channelRole(sender string) string {
	if sender == "user" {
		return "user"
	}
	if sender == "agent-deck" {
		return "system"
	}
	if m.teams != nil && m.teams.Manager != nil {
		for i := range m.teams.Manager.Team.Members {
			member := &m.teams.Manager.Team.Members[i]
			if member.Name == sender {
				return member.Role
			}
		}
	}
	return "worker"
}

func (m *Model) channelCard(record *channel.Record, width int) []string {
	to := "channel"
	if len(record.Recipients) > 0 {
		to = strings.Join(record.Recipients, ", ")
	}
	label := fmt.Sprintf("%s [%s] → %s · %s", record.Sender, m.channelRole(record.Sender), to, record.Time.Format("15:04"))
	if record.Private {
		label += " · private"
	}
	lines := []string{m.theme.title(fit(clean(label), width))}
	text := record.Text
	if record.Kind == "roster" && !m.channel.ShowRoster {
		text = "Roster updated · o expand/collapse system updates"
	}
	for _, line := range strings.Split(ansi.Hardwrap(clean(text), max(1, width-2), true), "\n") {
		lines = append(lines, "  "+line)
	}
	return append(lines, "")
}

func (m *Model) renderChannelChat() string {
	width := max(1, m.w)
	lines := []string{m.theme.title("Team channel")}
	lines = append(lines, m.buttonRow([]button{
		{"Back", "channel-back", false},
		{"Compose", "channel-compose", m.opts.ReadOnly || m.channel.Sending},
		{"Roster", "channel-roster", false},
		{"Refresh", "channel-refresh", m.channel.Loading},
	}, 0, 1, width, -1))
	var content []string
	for i := range m.channel.Records {
		record := &m.channel.Records[i]
		if record.Kind == "message" || record.Kind == "roster" {
			content = append(content, m.channelCard(record, width)...)
		}
	}
	footer := m.channelFooter(width)
	limit := max(0, m.h-len(lines)-len(footer))
	offset := max(0, min(m.channel.Offset, max(0, len(content)-limit)))
	m.channel.Offset = offset
	end := max(0, len(content)-offset)
	lines = append(lines, content[max(0, end-limit):end]...)
	if len(content) == 0 && limit > 0 {
		lines = append(lines, m.theme.dim("No messages yet · Enter or m to compose"))
	}
	lines = append(lines, footer...)
	for i := range lines {
		lines[i] = fit(lines[i], width)
	}
	if len(lines) > max(1, m.h) {
		lines = lines[:max(1, m.h)]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) channelFooter(width int) []string {
	var footer []string
	if m.channel.Editing {
		draft := strings.Split(ansi.Hardwrap(clean(m.channel.Draft)+"▏", width, true), "\n")
		help := "Compose · Enter send · Ctrl+J newline · Tab complete @member · Esc keep draft"
		footer = append(footer, m.theme.title(help))
		footer = append(footer, draft[max(0, len(draft)-3):]...)
		if choices := m.channelMentionChoices(); len(choices) > 0 {
			footer = append(footer, m.theme.dim("Members · Tab select: "+strings.Join(choices, "  ")))
		}
	} else if m.channel.Draft != "" {
		footer = append(footer, m.theme.dim("Draft saved · Enter resume"))
	}
	if m.channel.Runtime == nil {
		footer = append(footer, "Channel runtime unavailable")
	}
	if m.opts.ReadOnly {
		footer = append(footer, "Read-only · composing disabled")
	}
	for _, status := range []string{m.channel.Status, m.note} {
		if status != "" {
			footer = append(footer, status)
		}
	}
	footer = append(footer, m.theme.dim("Enter/m compose · j/k/PgUp/PgDn scroll · gg/G ends · o roster · Esc back"))
	return footer
}

func (m *Model) channelMentionChoices() []string {
	if m.teams == nil || m.teams.Manager == nil {
		return nil
	}
	start := strings.LastIndex(m.channel.Draft, "@")
	if start < 0 {
		return nil
	}
	if start > 0 {
		previous, _ := utf8.DecodeLastRuneInString(m.channel.Draft[:start])
		if !unicode.IsSpace(previous) {
			return nil
		}
	}
	prefix := m.channel.Draft[start+1:]
	if strings.ContainsAny(prefix, " \n\t") {
		return nil
	}
	var choices []string
	for i := range m.teams.Manager.Team.Members {
		name := m.teams.Manager.Team.Members[i].Name
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			choices = append(choices, "@"+name)
		}
	}
	return choices[:min(5, len(choices))]
}

func (m *Model) completeChannelMention() {
	if m.teams == nil || m.teams.Manager == nil {
		return
	}
	text := m.channel.Draft
	start := strings.LastIndex(text, "@")
	if start < 0 {
		return
	}
	matches := m.channelMentionChoices()
	if len(matches) == 0 {
		return
	}
	if len(matches) > 1 {
		m.channel.Status = "Mention candidates: " + strings.Join(matches, ", ")
	}
	m.channel.Draft = text[:start] + matches[0] + " "
}

func (m *Model) channelClick(action string) (bool, tea.Cmd) {
	switch action {
	case "channel-back":
		return true, m.openWorkspace("dashboard")
	case "channel-compose":
		m.channel.Editing = !m.opts.ReadOnly && !m.channel.Sending
	case "channel-roster":
		m.channel.ShowRoster = !m.channel.ShowRoster
	case "channel-refresh":
		return true, m.readChannel()
	default:
		return false, nil
	}
	return true, nil
}
