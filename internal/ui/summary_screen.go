package ui

import tea "github.com/charmbracelet/bubbletea"

func (m *Model) summarySource() *SummarySource {
	source := &SummarySource{Board: &m.board.State.Board, Channel: m.channel.Records}
	for i := range m.panes {
		p := &m.panes[i]
		source.Assistants = append(source.Assistants, SummaryAssistant{
			Member: p.Agent.Name, Directory: p.Agent.Dir, Session: p.Agent.Session,
			Messages: p.Messages, Limit: 30,
		})
	}
	return source
}

func (m *Model) summaryView() string {
	return RenderSummary(m.summarySource(), &m.summary, m.w, m.h)
}

func (m *Model) summaryKey(v tea.KeyMsg) tea.Cmd {
	if SummaryKey(m.summarySource(), &m.summary, v, m.h) || v.String() == "q" {
		return m.openWorkspace("dashboard")
	}
	if v.String() == "r" {
		return tea.Batch(m.readBoard(), m.readChannel())
	}
	return nil
}
