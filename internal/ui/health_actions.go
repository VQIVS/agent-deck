package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

type healthResult struct {
	Session, Dir string
	Err          error
}

func (m *Model) modalOpen() bool {
	return m.picker || m.editing || m.rejecting || m.confirm || m.permissionConfirm || m.compactConfirm || m.help
}

func (m *Model) updateHealth(msg tea.Msg) (bool, tea.Cmd) {
	switch v := msg.(type) {
	case boardResult:
		m.applyBoardResult(&v)
		return true, nil
	case channelStarted:
		if v.Err != nil {
			m.note = "Channel: " + v.Err.Error()
		} else {
			m.EnableChannel(v.Connection)
			m.channel.Team = v.Team
		}
		return true, nil
	case reportsResult:
		m.inbox.Reports = v
		return true, nil
	case channelResult:
		m.applyChannelResult(v)
		return true, nil
	case healthResult:
		if v.Err != nil {
			m.note = v.Err.Error()
		} else {
			m.note = "Session compacted"
		}
		for i := range m.panes {
			if m.panes[i].Agent.Session == v.Session && m.panes[i].Agent.Dir == v.Dir {
				return true, m.snapshot(i)
			}
		}
		return true, nil
	}
	return false, nil
}

func (m *Model) applyBoardResult(result *boardResult) {
	m.board.Loading = false
	if result.Err != nil {
		m.note = result.Err.Error()
		return
	}
	if result.State.Revision >= m.board.State.Revision && !m.board.Saving {
		m.board.State, m.board.Actions = result.State, result.Actions
	}
}

func (m *Model) compact() tea.Cmd {
	if m.opts.ReadOnly || len(m.panes) == 0 {
		return nil
	}
	a := m.panes[m.focus].Agent
	return func() tea.Msg {
		return healthResult{Session: a.Session, Dir: a.Dir, Err: m.compactSession(a.Dir, a.Session)}
	}
}

func (m *Model) compactSession(dir, session string) error {
	permissions, err := m.client.Permissions(m.context(), dir)
	if err != nil {
		return fmt.Errorf("check compact permissions: %w", err)
	}
	questions, err := m.client.Questions(m.context(), dir)
	if err != nil {
		return fmt.Errorf("check compact questions: %w", err)
	}
	for _, p := range permissions {
		if p.SessionID == session {
			return fmt.Errorf("resolve or abort permission wait before compacting")
		}
	}
	for _, q := range questions {
		if q.SessionID == session {
			return fmt.Errorf("resolve or abort question wait before compacting")
		}
	}
	statuses, err := m.client.Status(m.context(), dir)
	if err != nil {
		return fmt.Errorf("check compact status: %w", err)
	}
	if status := statuses[session].Type; status != "" && status != "idle" {
		return fmt.Errorf("compact requires an idle session")
	}
	messages, err := m.client.Messages(m.context(), dir, session)
	if err != nil {
		return fmt.Errorf("resolve compact model: %w", err)
	}
	latest := team.LatestAssistant(messages)
	if latest == nil || latest.Info.ProviderID == "" || latest.Info.ModelID == "" {
		return fmt.Errorf("session model is unknown; send a message before compacting")
	}
	if err := m.client.Summarize(m.context(), dir, session,
		api.ModelRef{ProviderID: latest.Info.ProviderID, ModelID: latest.Info.ModelID}); err != nil {
		return fmt.Errorf("compact session: %w", err)
	}
	return nil
}

func (m *Model) paneHealth(index int) string {
	p := &m.panes[index]
	text := m.theme.ContextGauge(p.State.ContextTokens, p.ContextLimit)
	return strings.TrimSpace(text)
}
