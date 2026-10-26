package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/config"
	"github.com/vqivs/agent-deck/internal/gitinfo"
)

func fakeLookPath(installed map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if path, ok := installed[name]; ok {
			return path, nil
		}
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
}

func gitTUIModel(t *testing.T, installed map[string]string, opts Options) *Model {
	t.Helper()
	opts.Theme = "dark"
	m := New(t.Context(), &config.Deck{}, opts)
	dir := filepath.Join(t.TempDir(), "my repo")
	m.panes = []pane{{Agent: config.Agent{Name: "core-dev", Dir: dir}}}
	files := []gitinfo.ChangedFile{{Path: "a.go", Status: "M"}}
	m.changes = changesUI{Root: dir, Directory: dir, Files: files, Generation: 1}
	m.workspace.View = "git"
	m.gitTUI.LookPath = fakeLookPath(installed)
	m.w, m.h = 80, 24
	return m
}

func openLauncher(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.gitViewKey(gitKey("t"))
	if cmd == nil {
		if m.gitTUI.Blocked == nil {
			t.Fatal("launcher neither detected nor blocked")
		}
		return
	}
	if handled, _ := m.workspaceResult(cmd()); !handled {
		t.Fatal("detection result not handled")
	}
}

func gitKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestGitTUIDetectsOnlyInstalledClients(t *testing.T) {
	m := gitTUIModel(t, map[string]string{"lazygit": "/bin/lazygit", "tig": "/bin/tig"}, Options{})
	openLauncher(t, m)
	names := make([]string, 0, len(m.gitTUI.Clients))
	for i := range m.gitTUI.Clients {
		names = append(names, m.gitTUI.Clients[i].Name)
	}
	if !slices.Equal(names, []string{"lazygit", "tig"}) || m.gitTUI.Status != "" || m.gitTUI.Starting {
		t.Fatalf("clients=%v status=%q", names, m.gitTUI.Status)
	}
	view := ansi.Strip(m.View())
	if strings.Contains(view, "gitui") && !strings.Contains(view, "Optional clients") {
		t.Fatalf("absent client rendered as choice:\n%s", view)
	}
}

func TestGitTUIDetectionErrorIsNotAbsence(t *testing.T) {
	m := gitTUIModel(t, nil, Options{})
	m.gitTUI.LookPath = func(name string) (string, error) {
		return "", &exec.Error{Name: name, Err: os.ErrPermission}
	}
	openLauncher(t, m)
	if !strings.HasPrefix(m.gitTUI.Status, "Detection failed") {
		t.Fatalf("status=%q", m.gitTUI.Status)
	}
}

func TestGitTUIBlockedStatesNeverLookUpOrLaunch(t *testing.T) {
	for name, opts := range map[string]Options{"read-only": {ReadOnly: true}, "no-git": {NoGit: true}} {
		t.Run(name, func(t *testing.T) {
			m := gitTUIModel(t, nil, opts)
			m.gitTUI.LookPath = func(string) (string, error) {
				t.Fatal("PATH lookup in blocked state")
				return "", nil
			}
			openLauncher(t, m)
			if m.gitViewKey(gitKey("enter")) != nil || m.gitViewKey(gitKey("r")) != nil {
				t.Fatal("blocked launcher produced a command")
			}
			view := ansi.Strip(m.View())
			want := map[string]string{"read-only": "READ ONLY", "no-git": "--no-git"}[name]
			if !strings.Contains(view, want) || hasZone(m, "gittui:launch") {
				t.Fatalf("missing %q or launch zone present:\n%s", want, view)
			}
		})
	}
}

func TestGitTUINoCheckoutAndLoadingStates(t *testing.T) {
	m := gitTUIModel(t, map[string]string{"lazygit": "/bin/lazygit"}, Options{})
	m.changes.Root, m.changes.Loading = "", true
	openLauncher(t, m)
	if !strings.Contains(strings.Join(m.gitTUI.Blocked, " "), "loading") {
		t.Fatalf("blocked=%v", m.gitTUI.Blocked)
	}
	dir := m.panes[0].Agent.Dir
	m.changes.Root, m.changes.Loading = dir, false
	cmd := m.gitChangesArrived(&changedFilesMsg{Generation: 1, Directory: dir, Root: dir})
	if cmd == nil || m.gitTUI.Blocked != nil {
		t.Fatal("launcher did not resume detection after inventory arrived")
	}
	m.panes = nil
	m.gitViewKey(gitKey("esc"))
	openLauncher(t, m)
	if !strings.Contains(strings.Join(m.gitTUI.Blocked, " "), "No code checkout") {
		t.Fatalf("blocked=%v", m.gitTUI.Blocked)
	}
}

func TestGitTUIRememberPreselectsButNeverAutoLaunches(t *testing.T) {
	installed := map[string]string{"lazygit": "/bin/lazygit", "gitui": "/bin/gitui"}
	m := gitTUIModel(t, installed, Options{})
	openLauncher(t, m)
	m.gitViewKey(gitKey("j"))
	m.gitViewKey(gitKey(" "))
	if cmd := m.gitViewKey(gitKey("enter")); cmd == nil || m.gitTUI.Remembered != "gitui" || !m.gitTUI.Starting {
		t.Fatalf("launch not started or choice not remembered: %+v", m.gitTUI)
	}
	m.gitClientDone(gitClientReturned{Client: "gitui", Target: m.gitTUI.Target})
	openLauncher(t, m)
	if m.gitTUI.Starting || m.gitTUI.Clients[m.gitTUI.Index].Name != "gitui" || !m.gitTUI.Remember {
		t.Fatalf("remembered choice not preselected: %+v", m.gitTUI)
	}
	delete(installed, "gitui")
	m.gitViewKey(gitKey("esc"))
	openLauncher(t, m)
	if m.gitTUI.Clients[m.gitTUI.Index].Name != "lazygit" || !strings.Contains(m.gitTUI.Status, "Previous choice") {
		t.Fatalf("missing remembered client handled silently: %+v", m.gitTUI)
	}
}

func TestGitTUIRefusesChangedTargetOrMovedClient(t *testing.T) {
	installed := map[string]string{"lazygit": "/bin/lazygit"}
	m := gitTUIModel(t, installed, Options{})
	openLauncher(t, m)
	m.panes[0].Agent.Dir = t.TempDir()
	if m.gitViewKey(gitKey("enter")) != nil || !strings.Contains(m.gitTUI.Status, "Checkout changed") {
		t.Fatalf("launched against changed target: %q", m.gitTUI.Status)
	}
	m = gitTUIModel(t, installed, Options{})
	openLauncher(t, m)
	installed["lazygit"] = "/elsewhere/lazygit"
	if m.gitViewKey(gitKey("enter")) != nil || !strings.Contains(m.gitTUI.Status, "no longer") {
		t.Fatalf("launched moved client: %q", m.gitTUI.Status)
	}
}

func TestGitClientCommandUsesDirectArgvAndFilteredEnv(t *testing.T) {
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	root := filepath.Join(t.TempDir(), "repo; rm -rf $HOME")
	command := gitClientCommand(t.Context(), &foundGitClient{
		gitClient: gitClient{Name: "tig", Args: []string{"status"}}, Path: "/bin/tig",
	}, root)
	if !slices.Equal(command.Args, []string{"/bin/tig", "status"}) || command.Dir != root {
		t.Fatalf("args=%v dir=%q", command.Args, command.Dir)
	}
	for _, entry := range command.Env {
		if strings.HasPrefix(entry, "GIT_DIR=") {
			t.Fatal("inherited GIT_DIR reached the client")
		}
	}
}

func TestGitClientReturnStates(t *testing.T) {
	m := gitTUIModel(t, map[string]string{"lazygit": "/bin/lazygit"}, Options{})
	openLauncher(t, m)
	m.gitViewKey(gitKey("enter"))
	m.gitClientDone(gitClientReturned{Client: "lazygit", Target: m.gitTUI.Target, Err: errors.New("fork failed")})
	if !m.gitTUI.Open || m.gitTUI.Starting || !strings.Contains(m.gitTUI.Status, "Could not start lazygit: fork failed") {
		t.Fatalf("spawn failure: %+v", m.gitTUI)
	}
	exit := exec.CommandContext(t.Context(), "sh", "-c", "exit 3").Run()
	if cmd := m.gitClientDone(gitClientReturned{Client: "lazygit", Target: m.gitTUI.Target, Err: exit}); cmd == nil {
		t.Fatal("return did not refresh")
	}
	if m.gitTUI.Open || !strings.Contains(m.note, "exited with code 3") || !m.changes.Loading {
		t.Fatalf("nonzero exit: open=%v note=%q", m.gitTUI.Open, m.note)
	}
	m.gitTUI.Returned = "lazygit"
	m.gitChangesArrived(&changedFilesMsg{Generation: m.changes.Generation, Err: errors.New("status failed")})
	if !strings.Contains(m.note, "Git refresh failed") {
		t.Fatalf("note=%q", m.note)
	}
}

func TestGitTUIMouseSelectsWithoutLaunching(t *testing.T) {
	m := gitTUIModel(t, map[string]string{"lazygit": "/bin/lazygit", "gitui": "/bin/gitui"}, Options{})
	openLauncher(t, m)
	m.View()
	z := findZone(t, m, "gittui:client:1")
	if handled, cmd := m.workspaceMouse(z, true); !handled || cmd != nil || m.gitTUI.Index != 1 || m.gitTUI.Starting {
		t.Fatalf("click on client launched or did not select: %+v", m.gitTUI)
	}
	m.workspaceMouse(findZone(t, m, "gittui:remember"), true)
	if !m.gitTUI.Remember {
		t.Fatal("remember checkbox click ignored")
	}
	m.workspaceMouse(findZone(t, m, "gittui:cancel"), true)
	m.View()
	if m.gitTUI.Open || !hasZone(m, "gittui:open") {
		t.Fatal("cancel did not return to built-in view with launch action")
	}
}

func TestGitTUIRendersFramedAtAnchorSizes(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {160, 48}} {
		m := gitTUIModel(t, map[string]string{"lazygit": "/usr/local/bin/lazygit", "tig": "/usr/bin/tig"}, Options{})
		m.panes[0].Agent.Dir = "/Users/dev/agent-deck"
		m.changes.Root, m.changes.Directory = "/Users/dev/agent-deck", "/Users/dev/agent-deck"
		m.w, m.h = size[0], size[1]
		openLauncher(t, m)
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d rendered %d rows", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > size[0] {
				t.Fatalf("row %d is %d cells wide", i+1, w)
			}
		}
		assertScreenGolden(t, fmt.Sprintf("git_tui_%dx%d", size[0], size[1]), view)
	}
}

func hasZone(m *Model, action string) bool {
	return slices.ContainsFunc(m.zones, func(z zone) bool { return z.Action == action })
}

func findZone(t *testing.T, m *Model, action string) zone {
	t.Helper()
	for _, z := range m.zones {
		if z.Action == action {
			return z
		}
	}
	t.Fatalf("no zone %q in %v", action, m.zones)
	return zone{}
}
