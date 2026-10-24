package ui

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qivs/agent-deck/internal/team"
)

type reportsResult map[InboxIdentity][]InboxReport

func (m *Model) readReports() tea.Cmd {
	if m.teams == nil || m.teams.Manager == nil {
		return nil
	}
	t := m.teams.Manager.Team
	root := filepath.Join(m.teams.Store.Root, ".reports")
	return func() tea.Msg {
		out := reportsResult{}
		store := team.ReportStore{Root: root, ReadOnly: true}
		for i := range t.Members {
			member := &t.Members[i]
			if member.Role != "worker" || member.Session == "" {
				continue
			}
			record, err := store.Load(m.context(), &t, member.Name)
			if err != nil {
				continue
			}
			identity := InboxIdentity{Agent: member.Name, Directory: member.Dir, Session: member.Session}
			out[identity] = []InboxReport{{
				ID:   fmt.Sprintf("%s/%d", member.Session, record.RecordedAt.UnixNano()),
				Text: ReportCard(&record, true, 120), At: record.RecordedAt, Inferred: record.Source == "inferred",
			}}
		}
		return out
	}
}
