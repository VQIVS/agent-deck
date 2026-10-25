package ui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func summaryFetchResult(status *SummaryStatus, err error) {
	status.Loading = false
	if err != nil {
		status.Error = err.Error()
		return
	}
	status.Loaded = true
	status.LastSuccess = time.Now()
	status.Error = ""
}

// summaryObserve is called before normal result handlers, even off-workspace.
func (m *Model) summaryObserve(msg tea.Msg) {
	s := &m.summary
	switch v := msg.(type) {
	case boardResult:
		summaryFetchResult(&s.boardStatus, v.Err)
	case channelResult:
		if v.Submitted == "" || v.Sent {
			summaryFetchResult(&s.channelStatus, v.Err)
		}
	case snapshotMsg:
		if v.Index < 0 || v.Index >= len(m.panes) {
			return
		}
		p := &m.panes[v.Index]
		if s.assistantStatus == nil {
			s.assistantStatus = map[string]SummaryStatus{}
		}
		key := p.Agent.Dir + "\x00" + p.Agent.Session
		status := s.assistantStatus[key]
		summaryFetchResult(&status, v.Err)
		s.assistantStatus[key] = status
	}
}

func (m *Model) summarySource() *SummarySource {
	source := &SummarySource{
		Channel: m.channel.Records, BoardStatus: m.summary.boardStatus, ChannelStatus: m.summary.channelStatus,
	}
	source.BoardStatus.Loading = m.board.Loading
	source.ChannelStatus.Loading = m.channel.Loading
	if source.BoardStatus.Loaded {
		source.Board = &m.board.State.Board
	}
	if m.teams != nil && m.teams.Manager != nil {
		source.Team = m.teams.Manager.Team.Name
		for i := range m.teams.Manager.Team.Members {
			source.Members = append(source.Members, m.teams.Manager.Team.Members[i].Name)
		}
	}
	for i := range m.panes {
		p := &m.panes[i]
		status := m.summary.assistantStatus[p.Agent.Dir+"\x00"+p.Agent.Session]
		status.Loading = p.Loading
		source.Assistants = append(source.Assistants, SummaryAssistant{
			Member: p.Agent.Name, Directory: p.Agent.Dir, Session: p.Agent.Session,
			Messages: p.Messages, Limit: 30, Status: status,
		})
	}
	return source
}

func (m *Model) summaryView() string {
	view := renderSummary(m.summarySource(), &m.summary, m.w, m.h, &m.theme)
	m.zones = append(m.zones, m.summary.zones...)
	return view
}

func (m *Model) summaryKey(v tea.KeyMsg) tea.Cmd {
	picker := m.summary.Mode == "picker"
	if SummaryKey(m.summarySource(), &m.summary, v, m.h) {
		return m.openWorkspace("dashboard")
	}
	if !picker && v.String() == "b" {
		return m.openWorkspace("board")
	}
	if !picker && v.String() == "r" {
		return tea.Batch(m.readBoard(), m.readChannel())
	}
	return nil
}

func (m *Model) summaryClick(z zone, ok bool) (bool, tea.Cmd) {
	if m.workspace.View != "summary" || !ok || !strings.HasPrefix(z.Action, "summary:") {
		return false, nil
	}
	action := strings.TrimPrefix(z.Action, "summary:")
	s := &m.summary
	source := m.summarySource()
	s.sync(source)
	return m.summaryAction(action, source)
}

func (m *Model) summaryAction(action string, source *SummarySource) (bool, tea.Cmd) {
	s := &m.summary
	switch action {
	case "picker":
		s.Mode = "picker"
		s.pickerSearch = ""
		s.pickerCursor = 0
		s.pickerIndex = 0
	case "coverage":
		s.Mode = "coverage"
		s.DetailOffset = 0
	case "help":
		s.Mode = "help"
		s.DetailOffset = 0
	case "detail":
		if s.selected() >= 0 {
			s.Mode = "detail"
			s.DetailOffset = 0
		}
	case "board":
		return true, m.openWorkspace("board")
	case "refresh":
		return true, tea.Batch(m.readBoard(), m.readChannel())
	case "cancel":
		s.Mode = ""
	case "apply":
		options := summaryPickerOptions(source, s)
		if s.pickerIndex >= 0 && s.pickerIndex < len(options) {
			s.filter(source, options[s.pickerIndex])
		}
	default:
		s.clickIndex(action)
	}
	return true, nil
}

func (s *SummaryState) clickIndex(action string) {
	if index, err := strconv.Atoi(strings.TrimPrefix(action, "event:")); err == nil {
		if index >= 0 && index < len(s.entries) {
			s.SelectedID = s.entries[index].ID
		}
	}
	if index, err := strconv.Atoi(strings.TrimPrefix(action, "option:")); err == nil {
		s.pickerIndex = index
	}
}

func (m *Model) summaryWheel(delta int) {
	m.summary.move(delta)
}

func (m *Model) summaryWheelAt(x, y, delta int) {
	s := &m.summary
	y-- // Persistent workspace tab row is outside the Summary body.
	if x <= 0 || x >= s.width-1 || y <= s.contentTop || y >= s.contentBottom-1 {
		return
	}
	if s.Mode == "picker" {
		options := summaryPickerOptions(m.summarySource(), s)
		s.pickerIndex = max(0, min(len(options)-1, s.pickerIndex+delta))
		return
	}
	s.move(delta)
}
