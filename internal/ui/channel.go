package ui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/qivs/agent-deck/internal/channel"
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

type incrementalChannel interface {
	ReadUserSince(context.Context, string, int) ([]channel.Record, error)
}

type channelUI struct {
	Runtime                TeamChannel
	Open, Editing, Loading bool
	Records                []channel.Record
	Draft                  string
	Offset                 int
	Team                   string
}

type channelResult struct {
	Records     []channel.Record
	Err         error
	Incremental bool
}

func (m *Model) applyChannelResult(v channelResult) {
	m.channel.Loading = false
	if v.Err != nil {
		m.note = v.Err.Error()
		return
	}
	if v.Incremental {
		m.channel.Records = append(m.channel.Records, v.Records...)
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
	if m.channel.Editing {
		switch v.String() {
		case "esc":
			m.channel.Editing = false
		case "enter":
			return m.postChannel()
		default:
			editInboxText(&m.channel.Draft, v)
		}
		return nil
	}
	switch v.String() {
	case "esc", "c":
		m.channel.Open = false
	case "up", "k":
		m.channel.Offset++
	case "down", "j":
		m.channel.Offset = max(0, m.channel.Offset-1)
	case "m":
		m.channel.Editing = !m.opts.ReadOnly
	case "r":
		return m.readChannel()
	}
	return nil
}

func (m *Model) postChannel() tea.Cmd {
	if m.opts.ReadOnly || m.channel.Runtime == nil || strings.TrimSpace(m.channel.Draft) == "" {
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
	m.channel.Editing = false
	m.channel.Draft = ""
	return func() tea.Msg {
		if err := runtime.PostUser(m.context(), text, mentions); err != nil {
			return channelResult{Err: err}
		}
		records, err := runtime.ReadUser(m.context())
		return channelResult{Records: records, Err: err}
	}
}

func (m *Model) channelView() string {
	lines := []string{"Team channel · m post · @member wakes idle recipient · ↑↓ scroll · Esc back"}
	if m.channel.Runtime == nil {
		lines = append(lines, "Channel runtime unavailable")
	}
	var content []string
	for i := range m.channel.Records {
		r := &m.channel.Records[i]
		if r.Kind != "message" && r.Kind != "roster" {
			continue
		}
		label := r.Sender
		if r.Private {
			label += " → " + strings.Join(r.Recipients, ",") + " [DM]"
		}
		text := fmt.Sprintf("%s %s · %s", r.Time.Format("15:04"), label, r.Text)
		content = append(content, strings.Split(ansi.Hardwrap(clean(text), max(1, m.w), true), "\n")...)
	}
	limit := max(1, m.h-4)
	m.channel.Offset = min(m.channel.Offset, max(0, len(content)-limit))
	end := max(0, len(content)-m.channel.Offset)
	lines = append(lines, content[max(0, end-limit):end]...)
	if m.channel.Editing {
		lines = append(lines, "Post: "+m.channel.Draft+"▏ · Enter send / Esc cancel")
	}
	lines = append(lines, m.note)
	for i := range lines {
		lines[i] = fit(lines[i], m.w)
	}
	return strings.Join(lines, "\n")
}
