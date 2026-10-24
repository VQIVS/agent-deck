package ui

import tea "github.com/charmbracelet/bubbletea"

//nolint:gocritic // Bubble Tea result values retain the dispatch API shared with dashboard.go.
func (m *Model) applyChangedFiles(v changedFilesMsg) tea.Cmd {
	if v.Generation != m.changes.Generation || v.Directory != m.codeDirectory() {
		return nil
	}
	m.changes.Loading = false
	if v.Err != nil {
		m.changes.Err = v.Err.Error()
		return nil
	}
	selected := ""
	if m.changes.Index >= 0 && m.changes.Index < len(m.changes.Files) {
		selected = m.changes.Files[m.changes.Index].Path
	}
	m.changes.Root = v.Root
	m.changes.Files = v.Files
	m.changes.Index = 0
	m.changes.Err = ""
	m.changes.Diff = ""
	m.changes.Reading = false
	m.changes.Offset = 0
	for i := range v.Files {
		if v.Files[i].Path == selected {
			m.changes.Index = i
			break
		}
	}
	return m.readChangeDiff()
}

//nolint:gocritic // Bubble Tea result values retain the dispatch API shared with dashboard.go.
func (m *Model) applyFileDiff(v fileDiffMsg) {
	if v.Generation != m.changes.Generation || v.Request != m.changes.Request ||
		v.Root != m.changes.Root || m.changes.Directory != m.codeDirectory() ||
		m.changes.Index < 0 || m.changes.Index >= len(m.changes.Files) ||
		v.Path != m.changes.Files[m.changes.Index].Path {
		return
	}
	m.changes.Reading = false
	m.changes.Err = ""
	if v.Err != nil {
		m.changes.Err = v.Err.Error()
		return
	}
	m.changes.Diff = v.Diff.Text
	if m.changes.Diff == "" && !v.Diff.Binary {
		m.changes.Diff = "No textual changes (empty file or metadata-only change)"
	}
	if v.Diff.Binary {
		m.changes.Diff = "Binary file · preview unavailable\n" + m.changes.Diff
	}
	if v.Diff.Truncated {
		m.changes.Diff += "\n… diff truncated · v opens file in Vim"
	}
}
