package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qivs/agent-deck/internal/config"
	"github.com/qivs/agent-deck/internal/team"
	"github.com/qivs/agent-deck/internal/ui"
)

type dirs []string

func (d *dirs) String() string { return fmt.Sprint([]string(*d)) }

func (d *dirs) Set(s string) error {
	p, err := filepath.Abs(s)
	if err != nil {
		return fmt.Errorf("resolve directory %q: %w", s, err)
	}
	*d = append(*d, p)
	return nil
}

func Run(args []string, build Build) int {
	if len(args) == 2 && args[1] == "--version" {
		fmt.Printf("agent-deck %s (%s, %s)\n", build.Version, build.Commit, build.Date)
		return 0
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if err := run(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "agent-deck:", err)
		var exit *exitError
		if errors.As(err, &exit) {
			return exit.Code
		}
		return 1
	}
	return 0
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "team":
			return teamCommand(context.Background(), args[1:], os.Stdout)
		case "import":
			return importTeam(args[1:])
		case "export":
			return exportTeam(args[1:])
		case "discover":
			return discover(args[1:])
		case "new":
			return newSession(args[1:])
		case "doctor":
			return doctor(args[1:])
		case "up", "down", "status", "supervise":
			return serverCommand(args[0], args[1:])
		case "run":
			args = args[1:]
		}
	}
	return dashboard(args)
}

type dashboardFlags struct {
	teamName, path, server string
	all, snapshot          bool
	options                ui.Options
}

func parseDashboard(args []string) (dashboardFlags, error) {
	var flags dashboardFlags
	f := flag.NewFlagSet("agent-deck", flag.ContinueOnError)
	f.StringVar(&flags.teamName, "team", "", "open a saved team")
	f.BoolVar(&flags.all, "all", false, "skip team picker and discover all active sessions")
	f.StringVar(&flags.options.Theme, "theme", "auto", "color theme: auto, dark, light")
	f.StringVar(&flags.path, "config", "", "optional YAML or JSON deck file")
	f.StringVar(&flags.server, "server", "", "server URL (overrides deck and AGENT_DECK_SERVER)")
	f.BoolVar(&flags.options.ReadOnly, "read-only", false, "disable all mutations")
	f.BoolVar(&flags.options.NoGit, "no-git", false, "do not inspect local worktrees")
	f.BoolVar(&flags.options.Notify, "notify", true, "macOS desktop notifications")
	f.BoolVar(&flags.snapshot, "snapshot", false, "print a read-only HTTP snapshot and exit")
	if err := f.Parse(args); err != nil {
		return flags, fmt.Errorf("parse dashboard flags: %w", err)
	}
	if f.NArg() > 0 {
		return flags, fmt.Errorf("unexpected arguments: %v", f.Args())
	}
	if _, err := ui.ResolveTheme(flags.options.Theme); err != nil {
		return flags, fmt.Errorf("select theme: %w", err)
	}
	return flags, nil
}

func dashboard(args []string) error {
	flags, err := parseDashboard(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deck, saved, revision, err := loadDeck(ctx, &flags)
	if err != nil {
		return err
	}
	if flags.snapshot {
		return printSnapshot(ctx, &deck, flags.path == "" && saved == nil)
	}
	flags.options.Auto = flags.path == "" && saved == nil
	model := ui.New(ctx, &deck, flags.options)
	if saved != nil {
		model.OpenTeam(saved, revision, team.DefaultRoot())
	} else if flags.path == "" && !flags.all {
		model.EnableTeams(team.DefaultRoot())
	}
	if printFindingsOnFailure(ctx, deck.Server) {
		if !offerServer(ctx, deck.Server, flags.options.ReadOnly) {
			return fmt.Errorf("complete setup above, then run agent-deck again")
		}
	}
	if _, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		return fmt.Errorf("run terminal: %w", err)
	}
	return nil
}

func loadDeck(ctx context.Context, flags *dashboardFlags) (config.Deck, *team.Team, []byte, error) {
	var (
		deck     config.Deck
		saved    *team.Team
		revision []byte
	)
	switch {
	case flags.teamName != "":
		store := team.Store{Root: team.DefaultRoot(), ReadOnly: flags.options.ReadOnly}
		loaded, rev, err := store.Load(ctx, flags.teamName)
		if err != nil {
			return deck, nil, nil, fmt.Errorf("load team %q: %w", flags.teamName, err)
		}
		deck, saved, revision = loaded.Deck(), &loaded, rev
	case flags.path != "":
		loaded, err := config.Load(ctx, flags.path)
		if err != nil {
			return deck, nil, nil, fmt.Errorf("load configuration: %w", err)
		}
		deck = loaded
	}
	deck.Server = resolveServer(flags.server, deck.Server)
	return deck, saved, revision, nil
}

func printFindingsOnFailure(ctx context.Context, server string) bool {
	checks := newDiagnostics(server).check(ctx, false)
	for _, check := range checks {
		if check.fix != "" {
			fmt.Println("\nagent-deck · Let's finish setup")
			printFindings(os.Stdout, checks)
			fmt.Println("\nRun agent-deck doctor for a full setup check.")
			return true
		}
	}
	return false
}

func resolveServer(server, deck string) string {
	for _, s := range []string{server, os.Getenv("AGENT_DECK_SERVER"), deck, "http://127.0.0.1:4096"} {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
