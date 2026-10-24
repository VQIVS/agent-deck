package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/gitinfo"
)

type changesUI struct {
	Root                             string
	Directory                        string
	Files                            []gitinfo.ChangedFile
	Index, Offset                    int
	Diff                             string
	Loading, Reading, DiffFocus, Top bool
	Err                              string
	Generation                       int
	Request                          int
}
type changedFilesMsg struct {
	Root       string
	Files      []gitinfo.ChangedFile
	Err        error
	Generation int
	Directory  string
}
type fileDiffMsg struct {
	Root, Path string
	Diff       gitinfo.Diff
	Err        error
	Generation int
	Request    int
}

func (m *Model) codeDirectory() string {
	if m.focus >= 0 && m.focus < len(m.panes) {
		a := m.panes[m.focus].Agent
		if a.CodeDir != "" {
			return a.CodeDir
		}
		return a.Dir
	}
	if m.teams != nil && m.teams.Manager != nil {
		return m.teams.Manager.Team.Repo
	}
	return ""
}

func (m *Model) refreshChanges() tea.Cmd {
	if m.opts.NoGit {
		m.changes.Err = "Git integration disabled (--no-git)"
		return nil
	}
	dir := m.codeDirectory()
	if dir == "" {
		m.changes.Err = "Select an agent or team with a code checkout"
		return nil
	}
	if m.changes.Loading && m.changes.Directory == dir {
		return nil
	}
	m.changes.Directory = dir
	m.changes.Loading = true
	m.changes.Generation++
	m.changes.Err = ""
	generation, ctx := m.changes.Generation, m.context()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		root, files, err := gitinfo.ChangedFiles(ctx, dir)
		return changedFilesMsg{Root: root, Files: files, Err: err, Generation: generation, Directory: dir}
	}
}

func (m *Model) readChangeDiff() tea.Cmd {
	if len(m.changes.Files) == 0 {
		return nil
	}
	m.changes.Index = min(max(0, m.changes.Index), len(m.changes.Files)-1)
	root, path := m.changes.Root, m.changes.Files[m.changes.Index].Path
	generation, ctx := m.changes.Generation, m.context()
	m.changes.Request++
	request := m.changes.Request
	m.changes.Reading = true
	m.changes.Err = ""
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		diff, err := gitinfo.FileDiff(ctx, root, path)
		return fileDiffMsg{Root: root, Path: path, Diff: diff, Err: err, Generation: generation, Request: request}
	}
}

func (m *Model) changesKey(v tea.KeyMsg) tea.Cmd {
	if v.String() != "g" {
		m.changes.Top = false
	}
	switch v.String() {
	case "esc", "q":
		m.workspace.View = "dashboard"
	case "r":
		return m.refreshChanges()
	case "tab":
		m.changes.DiffFocus = !m.changes.DiffFocus
	case "h", "left":
		m.changes.DiffFocus = false
	case "l", "right", "enter":
		m.changes.DiffFocus = true
	case "j", "down":
		return m.moveChange(1)
	case "k", "up":
		return m.moveChange(-1)
	case "pgdown", "ctrl+d":
		m.changes.Offset += max(1, m.h/2)
	case "pgup", "ctrl+u":
		m.changes.Offset = max(0, m.changes.Offset-max(1, m.h/2))
	case "g":
		if m.changes.Top {
			m.changes.Top = false
			return m.changeBoundary(false)
		} else {
			m.changes.Top = true
		}
	case "G":
		return m.changeBoundary(true)
	case "v", "e":
		return m.openChangedFile()
	}
	return nil
}

func (m *Model) moveChange(delta int) tea.Cmd {
	if m.changes.DiffFocus {
		m.changes.Offset = max(0, m.changes.Offset+delta)
		return nil
	}
	if len(m.changes.Files) == 0 {
		return nil
	}
	before := m.changes.Index
	m.changes.Index = max(0, min(len(m.changes.Files)-1, before+delta))
	if before != m.changes.Index {
		m.changes.Offset = 0
		return m.readChangeDiff()
	}
	return nil
}

func (m *Model) changeBoundary(last bool) tea.Cmd {
	if m.changes.DiffFocus {
		m.changes.Offset = 0
		if last {
			m.changes.Offset = max(0, len(strings.Split(m.changes.Diff, "\n"))-max(1, m.h-6))
		}
		return nil
	}
	index := 0
	if last {
		index = len(m.changes.Files) - 1
	}
	return m.moveChange(index - m.changes.Index)
}

func (m *Model) openChangedFile() tea.Cmd {
	if m.changes.Loading || m.changes.Directory != m.codeDirectory() ||
		m.changes.Index < 0 || m.changes.Index >= len(m.changes.Files) {
		return nil
	}
	command, err := changedFileEditor(m.context(), m.changes.Root,
		m.changes.Files[m.changes.Index].Path, m.opts.ReadOnly)
	if err != nil {
		m.note = "Open file: " + err.Error()
		return nil
	}
	return tea.ExecProcess(command, func(err error) tea.Msg { return editorReturned{Err: err} })
}

func changedFileEditor(ctx context.Context, checkout, relative string, readOnly bool) (*exec.Cmd, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return nil, fmt.Errorf("invalid checkout-relative file")
	}
	path := filepath.Join(checkout, relative)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve file: %w", err)
	}
	root, err := filepath.EvalSymlinks(checkout)
	if err != nil || !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("file is outside the checkout")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("inspect editor file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("editor requires a regular file")
	}
	args := []string{"--", resolved}
	if readOnly {
		args = []string{"-M", "-n", "-i", "NONE", "--", resolved}
	}
	command := exec.CommandContext(ctx, "vim", args...)
	command.Dir = root
	command.Env = gitinfo.Environment()
	return command, nil
}

type editorReturned struct{ Err error }

func (m *Model) changesView() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	if m.w < 45 || m.h < 7 {
		return fit("Git diff · resize to 45×8 · Esc back", m.w)
	}
	width, height := m.w, max(1, m.h-3)
	listWidth := min(38, max(20, width/3))
	diffWidth := max(1, width-listWidth-4)
	list := []string{fmt.Sprintf("Changed files (%d)", len(m.changes.Files))}
	start := max(0, m.changes.Index-max(1, height-3)+1)
	for i := start; i < len(m.changes.Files) && len(list) < height-1; i++ {
		file := &m.changes.Files[i]
		mark := "  "
		if i == m.changes.Index {
			mark = "› "
		}
		text := fit(mark+file.Status+" "+file.Path, listWidth-2)
		if i == m.changes.Index {
			text = lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Render(text)
		}
		m.zones = append(m.zones, zone{
			Rect: Rect{1, len(list) + 1, listWidth, 1}, Action: fmt.Sprintf("change:%d", i), Pane: -1,
		})
		list = append(list, text)
	}
	if len(m.changes.Files) == 0 && !m.changes.Loading && m.changes.Err == "" {
		list = append(list, "Working tree clean")
	}
	diff := m.changesDisplayDiff()
	lines := strings.Split(clean(diff), "\n")
	offset := max(0, min(m.changes.Offset, max(0, len(lines)-height+2)))
	title := "Diff"
	if m.changes.Index >= 0 && m.changes.Index < len(m.changes.Files) {
		title += " · " + m.changes.Files[m.changes.Index].Path
	}
	right := []string{fit(title, diffWidth)}
	for _, line := range lines[offset:min(len(lines), offset+max(0, height-2))] {
		right = append(right, m.changeDiffLine(line, diffWidth))
	}
	leftPanel := m.theme.panel(listWidth).Height(height - 2).Render(strings.Join(list, "\n"))
	rightPanel := m.theme.panel(diffWidth).Height(height - 2).Render(strings.Join(right, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel) + "\n" +
		fit("j/k files · Tab diff focus · Ctrl+d/u scroll · gg/G · v Vim · r refresh · Esc dashboard · "+m.note, width)
}

func (m *Model) changesDisplayDiff() string {
	if m.changes.Loading {
		return "Loading changed files…"
	}
	if m.changes.Err != "" {
		return m.changes.Err
	}
	if m.changes.Reading {
		return "Loading diff…"
	}
	return m.changes.Diff
}

func (m *Model) changeDiffLine(line string, width int) string {
	style := lipgloss.NewStyle().Foreground(m.theme.Text)
	switch {
	case strings.HasPrefix(line, "+"):
		style = style.Foreground(m.theme.Success)
	case strings.HasPrefix(line, "-"):
		style = style.Foreground(m.theme.Danger)
	case strings.HasPrefix(line, "@@"):
		style = style.Foreground(m.theme.Accent)
	}
	return style.Render(fit(line, width))
}
