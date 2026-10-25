package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type workspaceUI struct{ View string }

func (m *Model) workspaceGlobalKey(v tea.KeyMsg) (bool, tea.Cmd) {
	if m.channel.Editing || m.board.Form != nil || m.teamModalOpen() {
		return false, nil
	}
	if m.modalOpen() || m.searching {
		if m.picker {
			return true, m.updatePicker(v)
		}
		return m.dialogKey(v)
	}
	if v.String() == "ctrl+p" {
		m.palette.Open = true
		return true, nil
	}
	return false, nil
}

func (m *Model) workspaceResult(msg tea.Msg) (bool, tea.Cmd) {
	switch v := msg.(type) {
	case changedFilesMsg:
		cmd := m.applyChangedFiles(v)
		return true, tea.Batch(cmd, m.gitChangesArrived(&v))
	case gitClientsMsg:
		m.applyGitClients(v)
		return true, nil
	case gitClientReturned:
		return true, m.gitClientDone(v)
	case fileDiffMsg:
		m.applyFileDiff(v)
		return true, nil
	case editorReturned:
		m.note = "Returned from Vim"
		if v.Err != nil {
			m.note = "Vim: " + v.Err.Error()
		}
		cmd := m.refreshChanges()
		return true, cmd
	}
	return false, nil
}

func (m *Model) workspaceRefresh() []tea.Cmd {
	var cmds []tea.Cmd
	if m.channel.Open || m.workspace.View == "summary" {
		cmds = append(cmds, m.readChannel())
	}
	if m.workspace.View == "summary" {
		cmds = append(cmds, m.readBoard())
	}
	return cmds
}

func (m *Model) workspaceKey(v tea.KeyMsg) (bool, tea.Cmd) {
	if (m.board.Open || m.inbox.Open) && !strings.HasPrefix(v.String(), "alt+") {
		return false, nil
	}
	if m.workspaceBlocked() {
		return false, nil
	}
	key := strings.TrimPrefix(v.String(), "alt+")
	views := map[string]string{"1": "dashboard", "2": "git", "3": "summary", "4": "channel", "5": "board", "6": "inbox"}
	if view, ok := views[key]; ok {
		return true, m.openWorkspace(view)
	}
	return false, nil
}

func (m *Model) workspaceBlocked() bool {
	return m.modalOpen() || m.searching || m.channel.Editing || m.palette.Open ||
		m.board.Form != nil || m.teamModalOpen() ||
		(m.workspace.View == "summary" && m.summary.Mode == "picker")
}

func (m *Model) teamModalOpen() bool {
	return m.teams != nil && (m.teams.Screen != "" || m.teams.Confirm != "")
}

func (m *Model) openWorkspace(view string) tea.Cmd {
	m.workspace.View = view
	m.channel.Open = view == "channel"
	m.board.Open = view == "board"
	m.inbox.Open = view == "inbox"
	switch view {
	case "summary":
		return tea.Batch(m.readBoard(), m.readChannel())
	case "channel":
		return m.readChannel()
	case "board":
		return m.readBoard()
	case "git":
		return m.refreshChanges()
	case "git-tui":
		m.workspace.View = "git"
		refresh := m.refreshChanges()
		return tea.Batch(refresh, m.openGitTUI())
	}
	return nil
}

func (m *Model) workspaceTabs(width int) string {
	buttons := []button{
		{"1 Dashboard", "view:dashboard", false},
		{"2 Git", "view:git", false},
		{"3 Summary", "view:summary", false},
		{"4 Channel", "view:channel", false},
		{"5 Board", "view:board", false},
		{"6 Inbox", "view:inbox", false},
	}
	active := m.workspace.View
	if active == "" {
		active = "dashboard"
	}
	switch {
	case m.inbox.Open:
		active = "inbox"
	case m.board.Open:
		active = "board"
	case m.channel.Open:
		active = "channel"
	}
	for i := range buttons {
		if buttons[i].Action == "view:"+active {
			buttons[i].Label = ">" + buttons[i].Label
		}
	}
	return m.buttonRow(buttons, 0, 0, width, -1)
}

func (m *Model) workspaceMouse(z zone, ok bool) (bool, tea.Cmd) {
	if m.workflowMouseTarget(z, ok) {
		return true, m.workflowEditorClick(z.Action)
	}
	if handled, cmd := m.summaryClick(z, ok); handled {
		return true, cmd
	}
	if m.workspaceBlocked() {
		return false, nil
	}
	if ok && strings.HasPrefix(z.Action, "view:") {
		return true, m.openWorkspace(strings.TrimPrefix(z.Action, "view:"))
	}
	return m.changeClick(z, ok)
}

func (m *Model) changeClick(z zone, ok bool) (bool, tea.Cmd) {
	if handled, cmd := m.gitTUIClick(z, ok); handled || m.gitTUI.Open {
		return handled, cmd
	}
	if m.workspace.View == "git" && ok && strings.HasPrefix(z.Action, "change:") {
		index, err := strconv.Atoi(strings.TrimPrefix(z.Action, "change:"))
		if err == nil && index >= 0 && index < len(m.changes.Files) {
			m.changes.Index = index
			m.changes.Offset = 0
			m.changes.DiffFocus = false
			return true, m.readChangeDiff()
		}
	}
	return false, nil
}

func (m *Model) workspaceView() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	if (m.h < 9 && m.workspace.View != "summary") || m.teamModalOpen() || m.modalOpen() {
		return m.mainView()
	}
	width, height := m.w, m.h
	m.h = max(1, height-1)
	var body string
	switch m.workspace.View {
	case "git":
		body = m.gitView()
	case "summary":
		body = m.summaryView()
	default:
		body = m.mainView()
	}
	m.h = height
	for i := range m.zones {
		m.zones[i].Y++
	}
	tabs := m.workspaceTabs(width)
	return tabs + "\n" + body
}
