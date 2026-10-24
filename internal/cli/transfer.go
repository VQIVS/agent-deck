package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vqivs/agent-deck/internal/team"
)

func confirmTransfer(prompt string) error {
	fmt.Fprint(os.Stderr, prompt+" [y/N] ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil ||
		(strings.TrimSpace(strings.ToLower(line)) != "y" && strings.TrimSpace(strings.ToLower(line)) != "yes") {
		return fmt.Errorf("canceled")
	}
	return nil
}

// Standard flag parsing stops at the first positional argument; the documented
// transfer commands allow their file/team argument before flags.
func transferArgs(args []string) []string {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return append(append([]string(nil), args[1:]...), args[0])
	}
	return args
}

func importTeam(args []string) error {
	f := flag.NewFlagSet("import", flag.ContinueOnError)
	name := f.String("name", "", "managed team name (default filename)")
	repo := f.String("repo", "", "local repository for portable templates")
	yes := f.Bool("yes", false, "accept preview for a NEW team; replacements still confirm")
	if err := f.Parse(transferArgs(args)); err != nil {
		return fmt.Errorf("parse import flags: %w", err)
	}
	if f.NArg() != 1 {
		return fmt.Errorf("usage: agent-deck import <file> [--name NAME] [--repo DIR] [--yes]")
	}
	b, err := os.ReadFile(f.Arg(0))
	if err != nil {
		return fmt.Errorf("read import %q: %w", f.Arg(0), err)
	}
	n := *name
	if n == "" {
		n = strings.TrimSuffix(filepath.Base(f.Arg(0)), filepath.Ext(f.Arg(0)))
	}
	// Preserve the name of exported teams unless explicitly renamed.
	t, err := team.Import(b, team.ImportOptions{Name: *name, Repo: *repo})
	if err != nil && *name == "" {
		t, err = team.Import(b, team.ImportOptions{Name: n, Repo: *repo})
	}
	if err != nil {
		return fmt.Errorf("import team: %w", err)
	}
	s := team.Store{Root: team.DefaultRoot()}
	old, err := os.ReadFile(s.Path(t.Name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing team: %w", err)
	}
	fmt.Print(team.Preview(&t, old))
	if len(old) > 0 || !*yes {
		if err := confirmTransfer("Save " + s.Path(t.Name) + "?"); err != nil {
			return err
		}
	}
	if _, err := s.Save(context.Background(), &t, old); err != nil {
		return fmt.Errorf("save imported team: %w", err)
	}
	fmt.Println("Imported", t.Name, "→", s.Path(t.Name))
	return nil
}

func exportTeam(args []string) error {
	f := flag.NewFlagSet("export", flag.ContinueOnError)
	out := f.String("out", "", "output file (default stdout)")
	format := f.String("format", "yaml", "yaml or json")
	portable := f.Bool("portable", false, "strip session IDs and local path bindings")
	if err := f.Parse(transferArgs(args)); err != nil {
		return fmt.Errorf("parse export flags: %w", err)
	}
	if f.NArg() != 1 {
		return fmt.Errorf("usage: agent-deck export <team> [--out FILE] [--format yaml|json] [--portable]")
	}
	t, _, err := (team.Store{Root: team.DefaultRoot()}).Load(context.Background(), f.Arg(0))
	if err != nil {
		return fmt.Errorf("load exported team: %w", err)
	}
	b, err := team.Export(&t, *format, *portable)
	if err != nil {
		return fmt.Errorf("encode export: %w", err)
	}
	if *out == "" {
		_, err = os.Stdout.Write(b)
		if err != nil {
			return fmt.Errorf("write export to stdout: %w", err)
		}
		return nil
	}
	replace := false
	if _, err = os.Stat(*out); err == nil {
		if err := confirmTransfer("Replace " + *out + "?"); err != nil {
			return err
		}
		replace = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output %q: %w", *out, err)
	}
	if err := team.WriteExport(context.Background(), *out, b, replace); err != nil {
		return fmt.Errorf("write export %q: %w", *out, err)
	}
	return nil
}
