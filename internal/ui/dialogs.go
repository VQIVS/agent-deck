package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) dialogKey(v tea.KeyMsg) (bool, tea.Cmd) {
	if v.String() == "b" && !m.modalOpen() {
		m.board.Open = true
		return true, m.readBoard()
	}
	if m.compactConfirm {
		m.compactConfirm = false
		if v.String() == "y" {
			return true, m.compact()
		}
		return true, nil
	}
	if m.searching {
		return true, m.searchKey(v)
	}
	return m.actionDialogKey(v)
}

func (m *Model) searchKey(v tea.KeyMsg) tea.Cmd {
	switch v.String() {
	case "esc":
		m.searching = false
		return nil
	case "enter":
		m.searching = false
		m.find()
		return nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(v)
		m.find()
		return cmd
	}
}

func (m *Model) actionDialogKey(v tea.KeyMsg) (bool, tea.Cmd) {
	key := v.String()
	if m.permissionConfirm {
		m.permissionConfirm = false
		if key == "y" || key == "Y" {
			return true, m.replyPermission("always", "")
		}
		return true, nil
	}
	if m.rejecting {
		switch key {
		case "esc":
			m.rejecting = false
			return true, nil
		case "ctrl+s":
			m.rejecting = false
			return true, m.replyPermission("reject", strings.TrimSpace(m.input.Value()))
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(v)
			return true, cmd
		}
	}
	if m.editing {
		return true, m.composeKey(v)
	}
	if m.confirm {
		m.confirm = false
		if key == "y" || key == "Y" {
			a := m.panes[m.focus].Agent
			return true, func() tea.Msg {
				return actionMsg{Err: m.client.Abort(m.context(), a.Dir, a.Session), Text: "Abort requested"}
			}
		}
		return true, nil
	}
	if m.help {
		m.help = false
		return true, nil
	}
	return false, nil
}

func (m *Model) composeKey(v tea.KeyMsg) tea.Cmd {
	key := v.String()
	if key == "esc" {
		m.editing = false
		return nil
	}
	if key == "ctrl+s" || key == "enter" {
		text := strings.TrimSpace(m.input.Value())
		if text != "" {
			m.editing = false
			return m.prompt(text)
		}
	} else {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(v)
		return cmd
	}
	return nil
}
