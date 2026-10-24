package ui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/channel"
)

// TeamChannel is the privileged user path supplied by the team's runtime owner.
type TeamChannel interface {
	ReadUser(context.Context) ([]channel.Record, error)
	PostUser(context.Context, string, []string) error
}

type channelCredentials interface {
	InstallCredential(context.Context, string) error
	RevokeCredential(context.Context, string) error
}

type workflowSetup interface {
	SetupWorkflow(context.Context, string) error
}

type workflowReset interface {
	ResetWorkflow(context.Context, string) error
}

type incrementalChannel interface {
	ReadUserSince(context.Context, string, int) ([]channel.Record, error)
}

type channelUI struct {
	Runtime                       TeamChannel
	Open, Editing, Loading        bool
	Records                       []channel.Record
	Draft                         string
	Offset                        int
	Team                          string
	Sending, ShowRoster, PendingG bool
	Status                        string
}

type channelResult struct {
	Records     []channel.Record
	Err         error
	Incremental bool
	Sent        bool
	Submitted   string
}

func (m *Model) applyChannelResult(v channelResult) {
	m.channel.Loading = false
	m.applyChannelSendResult(v)
	if v.Err != nil {
		m.note = v.Err.Error()
		if v.Submitted == "" {
			m.channel.Status = "Refresh failed"
		}
		return
	}
	m.applyChannelRecords(v)
}

func (m *Model) applyChannelSendResult(v channelResult) {
	if v.Submitted != "" {
		m.channel.Sending = false
		if v.Sent {
			m.channel.Status = "Sent"
			if m.channel.Draft == v.Submitted {
				m.channel.Draft = ""
				m.channel.Editing = false
			}
		} else {
			m.channel.Status = "Send failed · draft preserved"
			m.channel.Editing = true
		}
	}
}

func (m *Model) applyChannelRecords(v channelResult) {
	if v.Incremental {
		seen := make(map[string]bool, len(m.channel.Records))
		for i := range m.channel.Records {
			seen[m.channel.Records[i].ID] = true
		}
		for i := range v.Records {
			if v.Records[i].ID == "" || !seen[v.Records[i].ID] {
				m.channel.Records = append(m.channel.Records, v.Records[i])
				seen[v.Records[i].ID] = true
			}
		}
	} else {
		m.channel.Records = v.Records
	}
	if len(m.channel.Records) > 1000 {
		m.channel.Records = m.channel.Records[len(m.channel.Records)-1000:]
	}
}

func (m *Model) EnableChannel(runtime TeamChannel) {
	m.channel.Runtime = runtime
}

func (m *Model) CloseChannel() {
	if closer, ok := m.channel.Runtime.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func (m *Model) readChannel() tea.Cmd {
	if m.channel.Runtime == nil || m.channel.Loading {
		return nil
	}
	m.channel.Loading = true
	runtime := m.channel.Runtime
	cursor := ""
	if n := len(m.channel.Records); n > 0 {
		cursor = m.channel.Records[n-1].ID
	}
	return func() tea.Msg {
		if incremental, ok := runtime.(incrementalChannel); ok {
			records, err := incremental.ReadUserSince(m.context(), cursor, 100)
			return channelResult{Records: records, Err: err, Incremental: true}
		}
		records, err := runtime.ReadUser(m.context())
		return channelResult{Records: records, Err: err}
	}
}

func (m *Model) channelKey(v tea.KeyMsg) tea.Cmd {
	if v.String() != "g" {
		m.channel.PendingG = false
	}
	if m.channel.Editing {
		return m.channelComposeKey(v)
	}
	switch v.String() {
	case "esc", "c":
		return m.openWorkspace("dashboard")
	case "up", "k":
		m.channel.Offset++
	case "down", "j":
		m.channel.Offset = max(0, m.channel.Offset-1)
	case "pgup", "ctrl+u":
		m.channel.Offset += max(1, m.h-8)
	case "pgdown", "ctrl+d":
		m.channel.Offset = max(0, m.channel.Offset-max(1, m.h-8))
	case "g":
		if m.channel.PendingG {
			m.channel.Offset = 1 << 30
			m.channel.PendingG = false
		} else {
			m.channel.PendingG = true
		}
	case "G":
		m.channel.Offset = 0
	case "o":
		m.channel.ShowRoster = !m.channel.ShowRoster
	case "m", "enter":
		m.channel.Editing = !m.opts.ReadOnly
	case "r":
		return m.readChannel()
	}
	return nil
}

func (m *Model) channelComposeKey(v tea.KeyMsg) tea.Cmd {
	if m.opts.ReadOnly {
		m.channel.Editing = false
		return nil
	}
	switch v.String() {
	case "esc":
		m.channel.Editing = false
	case "enter":
		return m.postChannel()
	case "tab":
		m.completeChannelMention()
	case "ctrl+j":
		if len(m.channel.Draft) < 4096 {
			m.channel.Draft += "\n"
		}
	default:
		editInboxText(&m.channel.Draft, v)
	}
	return nil
}

func (m *Model) postChannel() tea.Cmd {
	if m.opts.ReadOnly || m.channel.Sending || m.channel.Runtime == nil || strings.TrimSpace(m.channel.Draft) == "" {
		return nil
	}
	text := m.channel.Draft
	mentions := []string{}
	if m.teams != nil && m.teams.Manager != nil {
		for i := range m.teams.Manager.Team.Members {
			name := m.teams.Manager.Team.Members[i].Name
			for _, word := range strings.Fields(text) {
				if strings.TrimRight(word, ",.:;!") == "@"+name {
					mentions = append(mentions, name)
					break
				}
			}
		}
	}
	runtime := m.channel.Runtime
	m.channel.Sending = true
	m.channel.Status = "Sending…"
	return func() tea.Msg {
		if err := runtime.PostUser(m.context(), text, mentions); err != nil {
			return channelResult{Err: err, Submitted: text}
		}
		records, err := runtime.ReadUser(m.context())
		return channelResult{Records: records, Err: err, Sent: true, Submitted: text}
	}
}

func (m *Model) channelView() string {
	return m.renderChannelChat()
}
