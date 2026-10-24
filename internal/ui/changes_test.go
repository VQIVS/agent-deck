package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/config"
	"github.com/vqivs/agent-deck/internal/gitinfo"
)

func changesModel(t *testing.T) *Model {
	t.Helper()
	m := New(t.Context(), &config.Deck{}, Options{Theme: "dark"})
	m.panes = []pane{{Agent: config.Agent{Dir: t.TempDir()}}}
	m.changes = changesUI{
		Root: m.panes[0].Agent.Dir, Directory: m.panes[0].Agent.Dir,
		Files: []gitinfo.ChangedFile{{Path: "a"}, {Path: "b"}, {Path: "c"}}, Generation: 1,
	}
	return m
}

func TestChangesNavigationAndStaleDiff(t *testing.T) {
	m := changesModel(t)
	if cmd := m.changesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}); cmd == nil || m.changes.Index != 1 {
		t.Fatal("j did not select next file")
	}
	old := m.changes.Request
	m.moveChange(-1)
	m.moveChange(1)
	m.applyFileDiff(fileDiffMsg{
		Root: m.changes.Root, Path: "b", Generation: 1, Request: old,
		Diff: gitinfo.Diff{Text: "stale"},
	})
	if !m.changes.Reading || m.changes.Diff == "stale" {
		t.Fatal("stale request applied after selection returned to same file")
	}
	m.applyFileDiff(fileDiffMsg{
		Root: m.changes.Root, Path: "b", Generation: 1, Request: m.changes.Request,
		Diff: gitinfo.Diff{Binary: true, Truncated: true},
	})
	if m.changes.Reading || !strings.Contains(m.changes.Diff, "Binary") || !strings.Contains(m.changes.Diff, "truncated") {
		t.Fatal(m.changes)
	}
	m.changesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.changes.Index != 2 {
		t.Fatal("G did not select last file")
	}
	for range 2 {
		m.changesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	}
	if m.changes.Index != 0 {
		t.Fatal("gg did not select first file")
	}
	m.changes.DiffFocus = true
	m.moveChange(3)
	if m.changes.Offset != 3 || m.changes.Index != 0 {
		t.Fatal("diff navigation moved selected file")
	}
}

func TestChangesResultsAndCheckoutGeneration(t *testing.T) {
	m := changesModel(t)
	m.changes.Loading = true
	m.applyChangedFiles(changedFilesMsg{Generation: 0, Directory: m.codeDirectory()})
	if !m.changes.Loading {
		t.Fatal("stale inventory applied")
	}
	m.applyChangedFiles(changedFilesMsg{Generation: 1, Directory: "other"})
	if !m.changes.Loading {
		t.Fatal("other checkout applied")
	}
	m.changes.Index = 1
	cmd := m.applyChangedFiles(changedFilesMsg{
		Generation: 1, Directory: m.codeDirectory(), Root: m.changes.Root,
		Files: []gitinfo.ChangedFile{{Path: "b"}, {Path: "c"}},
	})
	if cmd == nil || m.changes.Index != 0 || m.changes.Files[0].Path != "b" {
		t.Fatal("selection not preserved")
	}
	m.applyFileDiff(fileDiffMsg{
		Generation: 1, Request: m.changes.Request, Root: m.changes.Root, Path: "b",
		Err: errors.New("failed diff"),
	})
	if m.changes.Err != "failed diff" {
		t.Fatal(m.changes)
	}
	m.applyChangedFiles(changedFilesMsg{Generation: 1, Directory: m.codeDirectory(), Root: m.changes.Root})
	if len(m.changes.Files) != 0 || m.changes.Diff != "" || m.changes.Reading {
		t.Fatal("clean inventory retained old diff")
	}
}

func TestChangedFileEditor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file with spaces"), []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, readOnly := range []bool{false, true} {
		cmd, err := changedFileEditor(context.Background(), root, "file with spaces", readOnly)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"vim", "--", filepath.Join(resolved, "file with spaces")}
		if readOnly {
			want = []string{"vim", "-M", "-n", "-i", "NONE", "--", filepath.Join(resolved, "file with spaces")}
		}
		if !reflect.DeepEqual(cmd.Args, want) || cmd.Dir != resolved {
			t.Fatalf("command: %v dir=%q", cmd.Args, cmd.Dir)
		}
	}
	for _, path := range []string{"../outside", "/absolute", "missing"} {
		if _, err := changedFileEditor(t.Context(), root, path, false); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := changedFileEditor(t.Context(), root, "link", false); err == nil {
		t.Fatal("accepted editor symlink outside checkout")
	}
}

func TestChangesViewSmallAndPure(t *testing.T) {
	m := changesModel(t)
	m.changes.Offset = 1000
	m.changes.Index = 1000
	for _, size := range [][2]int{{0, 0}, {20, 5}, {45, 7}, {80, 24}, {160, 48}} {
		m.w, m.h = size[0], size[1]
		before := m.changes
		view := m.changesView()
		if !reflect.DeepEqual(before, m.changes) {
			t.Fatal("View mutated changes state")
		}
		if lipgloss.Width(view) > m.w || lipgloss.Height(view) > max(1, m.h) {
			t.Fatalf("size %v produced %dx%d", size, lipgloss.Width(view), lipgloss.Height(view))
		}
	}
}

func TestChangesLoadingErrorAndEditorReturn(t *testing.T) {
	m := changesModel(t)
	m.w, m.h = 80, 24
	for _, tc := range []struct {
		loading, reading bool
		err, want        string
	}{
		{true, false, "", "Loading changed files"},
		{false, true, "", "Loading diff"},
		{false, false, "failed inventory", "failed inventory"},
	} {
		m.changes.Loading, m.changes.Reading, m.changes.Err = tc.loading, tc.reading, tc.err
		if view := m.changesView(); !strings.Contains(view, tc.want) {
			t.Fatalf("missing %q: %s", tc.want, view)
		}
	}
	m.changes.Loading = false
	before := m.changes.Generation
	_, cmd := m.Update(editorReturned{})
	if cmd == nil || !m.changes.Loading || m.changes.Generation != before+1 || m.note != "Returned from Vim" {
		t.Fatal("editor return did not refresh inventory")
	}
	m.changes.Loading = true
	m.changes.Directory = "old checkout"
	if cmd := m.refreshChanges(); cmd == nil {
		t.Fatal("checkout switch did not supersede in-flight inventory")
	}
}
