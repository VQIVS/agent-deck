package ui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/gitinfo"
)

type gitClient struct {
	Name, Hint string
	Args       []string
}

// supportedGitClients is the fixed launch list; argv is direct so paths never pass through a shell.
func supportedGitClients() []gitClient {
	return []gitClient{
		{Name: "lazygit", Hint: "Suggested: Vim-style defaults; q returns"},
		{Name: "gitui", Hint: "Arrow-key defaults; use client help/quit"},
		{Name: "tig", Hint: "Opens status; client quit returns", Args: []string{"status"}},
	}
}

type foundGitClient struct {
	gitClient
	Path string
}

type gitTarget struct{ Member, Dir, Root string }

type gitTUIUI struct {
	Open, Detecting, Starting, Remember, Details bool
	Target                                       gitTarget
	Blocked                                      []string
	Clients                                      []foundGitClient
	Index, Generation                            int
	Remembered, Status, Returned                 string
	LookPath                                     func(string) (string, error)
}

type gitClientsMsg struct {
	Generation int
	Clients    []foundGitClient
	Err        error
}

type gitClientReturned struct {
	Client string
	Target gitTarget
	Err    error
}

func (g *gitTUIUI) lookPath() func(string) (string, error) {
	if g.LookPath != nil {
		return g.LookPath
	}
	return exec.LookPath
}

func (m *Model) gitTarget() gitTarget {
	target := gitTarget{Dir: m.codeDirectory()}
	if m.focus >= 0 && m.focus < len(m.panes) {
		target.Member = m.panes[m.focus].Agent.Name
	}
	if target.Dir != "" && m.changes.Directory == target.Dir {
		target.Root = m.changes.Root
	}
	return target
}

func (m *Model) gitTUIBlocked(target gitTarget) []string {
	switch {
	case m.opts.NoGit:
		return []string{"Git integration disabled (--no-git).", "No PATH or Git lookup and no launch."}
	case m.opts.ReadOnly:
		return []string{
			"READ ONLY: external Git clients are disabled.",
			"Clients can change Git state or invoke helpers.", "Use built-in read-only diff.",
		}
	case target.Dir == "":
		return []string{"No code checkout selected.", "Select a member or team with a checkout."}
	case target.Root == "" && m.changes.Loading:
		return []string{"Changed files still loading.", "Press r to check the checkout again."}
	case target.Root == "":
		return []string{"Selected directory is not a Git checkout.", "Built-in diff shows the reason."}
	}
	return nil
}

func (m *Model) openGitTUI() tea.Cmd {
	g := &m.gitTUI
	g.Open, g.Details, g.Starting, g.Detecting = true, false, false, false
	g.Clients, g.Status, g.Index = nil, "", 0
	g.Remember = g.Remembered != ""
	g.Target = m.gitTarget()
	g.Blocked = m.gitTUIBlocked(g.Target)
	if g.Blocked != nil {
		return nil
	}
	return m.detectGitClients()
}

func (m *Model) detectGitClients() tea.Cmd {
	g := &m.gitTUI
	if g.Detecting || g.Blocked != nil {
		return nil
	}
	g.Detecting = true
	g.Generation++
	generation, look := g.Generation, g.lookPath()
	return func() tea.Msg {
		clients, err := findGitClients(look)
		return gitClientsMsg{Generation: generation, Clients: clients, Err: err}
	}
}

func findGitClients(look func(string) (string, error)) ([]foundGitClient, error) {
	var found []foundGitClient
	var failures []error
	for _, client := range supportedGitClients() {
		path, err := look(client.Name)
		switch {
		case err == nil:
			found = append(found, foundGitClient{gitClient: client, Path: path})
		case !errors.Is(err, exec.ErrNotFound):
			failures = append(failures, fmt.Errorf("check %s: %w", client.Name, err))
		}
	}
	return found, errors.Join(failures...)
}

func (m *Model) applyGitClients(v gitClientsMsg) {
	g := &m.gitTUI
	if !g.Open || v.Generation != g.Generation {
		return
	}
	g.Detecting, g.Clients, g.Index, g.Status = false, v.Clients, 0, ""
	if v.Err != nil {
		g.Status = "Detection failed: " + v.Err.Error()
	}
	if g.Remembered == "" {
		return
	}
	for i := range g.Clients {
		if g.Clients[i].Name == g.Remembered {
			g.Index = i
			return
		}
	}
	g.Status = "Previous choice unavailable; choose an installed client."
}

func (m *Model) gitViewKey(v tea.KeyMsg) tea.Cmd {
	if m.gitTUI.Open {
		return m.gitTUIKey(v)
	}
	if v.String() == "t" {
		return m.openGitTUI()
	}
	return m.changesKey(v)
}

func (m *Model) gitTUIKey(v tea.KeyMsg) tea.Cmd {
	g := &m.gitTUI
	if g.Details {
		if key := v.String(); key == "esc" || key == "q" || key == "i" {
			g.Details = false
		}
		return nil
	}
	switch v.String() {
	case "esc", "q":
		g.Open = false
	case "j", "down":
		g.Index = max(0, min(len(g.Clients)-1, g.Index+1))
	case "k", "up":
		g.Index = max(0, g.Index-1)
	case " ":
		g.Remember = !g.Remember && len(g.Clients) > 0
	case "i":
		g.Details = true
	case "r":
		if g.Blocked != nil {
			return m.openGitTUI()
		}
		return m.detectGitClients()
	case "enter":
		return m.launchGitClient()
	}
	return nil
}

func (m *Model) launchGitClient() tea.Cmd {
	g := &m.gitTUI
	if g.Blocked != nil || g.Detecting || g.Starting || g.Index < 0 || g.Index >= len(g.Clients) {
		return nil
	}
	if m.gitTarget() != g.Target {
		g.Status = "Checkout changed; reopen launcher"
		return nil
	}
	client := g.Clients[g.Index]
	if path, err := g.lookPath()(client.Name); err != nil || path != client.Path {
		g.Status = client.Name + " is no longer at " + client.Path + "; press r to detect again"
		return nil
	}
	g.Remembered = ""
	if g.Remember {
		g.Remembered = client.Name
	}
	g.Starting, g.Status = true, "Starting "+client.Name+"…"
	target := g.Target
	return tea.ExecProcess(gitClientCommand(m.context(), &client, target.Root), func(err error) tea.Msg {
		return gitClientReturned{Client: client.Name, Target: target, Err: err}
	})
}

func gitClientCommand(ctx context.Context, client *foundGitClient, root string) *exec.Cmd {
	command := exec.CommandContext(ctx, client.Path, client.Args...)
	command.Dir = root
	command.Env = gitinfo.Environment()
	return command
}

//nolint:gocritic // Bubble Tea result values retain the dispatch API shared with dashboard.go.
func (m *Model) gitClientDone(v gitClientReturned) tea.Cmd {
	g := &m.gitTUI
	g.Starting = false
	var exit *exec.ExitError
	switch {
	case v.Err == nil:
		m.note = "Returned from " + v.Client + "; refreshing original checkout…"
	case errors.As(v.Err, &exit):
		m.note = fmt.Sprintf("%s exited with code %d; dashboard restored", v.Client, exit.ExitCode())
	default:
		g.Status = "Could not start " + v.Client + ": " + v.Err.Error()
		return nil
	}
	g.Open = false
	cmds := m.refreshCommands()
	if m.gitTarget().Dir != v.Target.Dir {
		m.note += " · original checkout no longer selected"
		return tea.Batch(cmds...)
	}
	g.Returned = v.Client
	m.changes.Loading = false
	return tea.Batch(append(cmds, m.refreshChanges())...)
}

// gitChangesArrived reopens a launcher that waited for the inventory and reports a post-return refresh.
func (m *Model) gitChangesArrived(v *changedFilesMsg) tea.Cmd {
	g := &m.gitTUI
	if v.Generation != m.changes.Generation {
		return nil
	}
	if g.Open && g.Target.Root == "" && g.Target.Dir == v.Directory {
		return m.openGitTUI()
	}
	if g.Returned == "" {
		return nil
	}
	if v.Err != nil {
		m.note = "Returned from " + g.Returned + "; Git refresh failed · r retry"
	} else {
		m.note = "Returned from " + g.Returned + "; checkout refreshed " + time.Now().UTC().Format("15:04") + " UTC"
	}
	g.Returned = ""
	return nil
}
