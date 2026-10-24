package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

type exitError struct {
	Code int
	Text string
}

func (e *exitError) Error() string { return e.Text }

type teamFlags struct {
	Name, Server, Member, Text, File, ID, Message string
	JSON, ReadOnly, Always, HasText               bool
	Timeout, Poll                                 time.Duration
}

func parseTeamFlags(command string, args []string) (teamFlags, error) {
	var opts teamFlags
	f := flag.NewFlagSet("agent-deck team "+command, flag.ContinueOnError)
	f.StringVar(&opts.Name, "team", "", "saved team name")
	f.StringVar(&opts.Server, "server", "", "override server URL")
	f.BoolVar(&opts.ReadOnly, "read-only", false, "disable mutations")
	f.BoolVar(&opts.JSON, "json", false, "print JSON")
	switch command {
	case "status", "ls", "requests":
	case "board":
		f.StringVar(&opts.File, "file", "", "configure private board from JSON")
	case "recover":
		f.StringVar(&opts.ID, "id", "", "uncertain pool action ID")
		f.StringVar(&opts.Text, "decision", "inspect", "inspect, adopt or delete orphan")
	case "send":
		f.StringVar(&opts.Member, "member", "", "member name")
		f.StringVar(&opts.Text, "text", "", "message text")
		f.StringVar(&opts.File, "file", "", "read message from file")
	case "wait":
		f.StringVar(&opts.Member, "member", "", "member name")
		f.DurationVar(&opts.Timeout, "timeout", 30*time.Minute, "maximum wait")
		f.DurationVar(&opts.Poll, "poll", 5*time.Second, "poll interval")
	case "last":
		f.StringVar(&opts.Member, "member", "", "member name")
	case "approve", "reject":
		f.StringVar(&opts.ID, "id", "", "pending request ID")
		if command == "approve" {
			f.BoolVar(&opts.Always, "always", false, "approve always rather than once")
		} else {
			f.StringVar(&opts.Message, "message", "", "rejection reason")
		}
	default:
		return opts, fmt.Errorf("unknown team command %q", command)
	}
	if err := f.Parse(args); err != nil {
		return opts, fmt.Errorf("parse team flags: %w", err)
	}
	f.Visit(func(f *flag.Flag) {
		if f.Name == "text" {
			opts.HasText = true
		}
	})
	if f.NArg() != 0 {
		return opts, fmt.Errorf("unexpected arguments: %v", f.Args())
	}
	return opts, nil
}

func teamCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: agent-deck team ls|status|send|wait|last|requests|approve|reject [flags]")
	}
	opts, err := parseTeamFlags(args[0], args[1:])
	if err != nil {
		return err
	}
	store := team.Store{Root: team.DefaultRoot(), ReadOnly: opts.ReadOnly}
	if args[0] == "ls" {
		return listSavedTeams(ctx, store, &opts, out)
	}
	if opts.Name == "" {
		return fmt.Errorf("--team is required")
	}
	t, revision, err := store.Load(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("load team: %w", err)
	}
	t.Server = resolveServer(opts.Server, t.Server)
	manager := &team.Manager{Store: store, Team: t, Version: revision, ReadOnly: opts.ReadOnly}
	return executeTeam(ctx, args[0], manager, &opts, out)
}

func executeTeam(ctx context.Context, command string, manager *team.Manager, opts *teamFlags, out io.Writer) error {
	switch command {
	case "status":
		return teamStatus(ctx, manager, opts, out)
	case "requests":
		return teamRequests(ctx, manager, opts, out)
	case "approve", "reject":
		return teamReply(ctx, command, manager, opts, out)
	case "board":
		return poolBoard(ctx, manager, opts, out)
	case "recover":
		return poolRecover(ctx, manager, opts, out)
	}
	if opts.Member == "" {
		return fmt.Errorf("--member is required")
	}
	index, err := manager.MemberIndex(opts.Member)
	if err != nil {
		return fmt.Errorf("select member: %w", err)
	}
	if manager.Team.Members[index].Session == "" {
		return fmt.Errorf("member %q has no session", opts.Member)
	}
	switch command {
	case "send":
		return teamSend(ctx, manager, index, opts, out)
	case "wait":
		return teamWait(ctx, manager, index, opts, out)
	default:
		return teamLast(ctx, manager, index, opts, out)
	}
}

func writeJSON(out io.Writer, value any) error {
	if err := json.NewEncoder(out).Encode(value); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	return nil
}

func writeText(out io.Writer, text string) error {
	if _, err := fmt.Fprintln(out, text); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func listSavedTeams(ctx context.Context, store team.Store, opts *teamFlags, out io.Writer) error {
	teams, err := store.List(ctx)
	if err != nil {
		return fmt.Errorf("list teams: %w", err)
	}
	if teams == nil {
		teams = []team.Team{}
	}
	if opts.Name != "" {
		filtered := []team.Team{}
		for i := range teams {
			if teams[i].Name == opts.Name {
				filtered = append(filtered, teams[i])
			}
		}
		teams = filtered
	}
	if opts.JSON {
		return writeJSON(out, teams)
	}
	for i := range teams {
		t := &teams[i]
		if err := writeText(out, fmt.Sprintf("%s\t%d members\t%s", t.Name, len(t.Members), t.Repo)); err != nil {
			return err
		}
	}
	return nil
}

func readPrompt(opts *teamFlags) (string, error) {
	if opts.HasText == (opts.File != "") {
		return "", fmt.Errorf("provide exactly one of --text or --file")
	}
	text := opts.Text
	if opts.File != "" {
		b, err := os.ReadFile(opts.File)
		if err != nil {
			return "", fmt.Errorf("read message file: %w", err)
		}
		text = string(b)
	}
	if text == "" {
		return "", fmt.Errorf("message is empty")
	}
	return text, nil
}
