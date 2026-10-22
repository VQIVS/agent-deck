package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/qivs/agent-deck/internal/config"
	"github.com/qivs/agent-deck/internal/opencode"
)

func discover(args []string) error {
	f := flag.NewFlagSet("discover", flag.ContinueOnError)
	var paths dirs
	f.Var(&paths, "dir", "directory to discover (repeatable; default all projects)")
	server := f.String("server", "", "opencode server")
	out := f.String("out", "deck.yaml", "new deck file (never overwritten)")
	master := f.String("master", "", "optional lead session ID")
	if err := f.Parse(args); err != nil {
		return fmt.Errorf("parse discovery flags: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := opencode.New(resolveServer(*server, ""), true)
	agents, err := discoverAgents(ctx, client, paths)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		return fmt.Errorf("no sessions in requested directories")
	}
	if err := selectMaster(agents, *master); err != nil {
		return err
	}
	deck := config.Deck{Server: client.Base, Sessions: agents}
	if err := config.Write(ctx, *out, &deck); err != nil {
		return fmt.Errorf("save discovered deck: %w", err)
	}
	fmt.Println("Wrote", *out)
	return nil
}

func discoverAgents(ctx context.Context, client *opencode.Client, paths []string) ([]config.Agent, error) {
	if len(paths) == 0 {
		inventory, err := client.Discover(ctx)
		if err != nil {
			return nil, fmt.Errorf("discover sessions: %w", err)
		}
		return listedAgents(inventory.Sessions), nil
	}
	var agents []config.Agent
	seen := map[string]bool{}
	for _, dir := range paths {
		sessions, err := client.Sessions(ctx, dir)
		if err != nil {
			return nil, fmt.Errorf("list sessions in %q: %w", dir, err)
		}
		sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].ID > sessions[j].ID })
		for _, session := range sessions {
			key := dir + "\x00" + session.ID
			if filepath.Clean(session.Directory) != filepath.Clean(dir) || seen[key] {
				continue
			}
			seen[key] = true
			agents = append(agents, listedAgents([]opencode.Session{session})...)
		}
	}
	return agents, nil
}

func listedAgents(sessions []opencode.Session) []config.Agent {
	agents := make([]config.Agent, 0, len(sessions))
	for _, session := range sessions {
		fmt.Printf("%s\t%s\t%s\n", session.ID, session.Directory, session.Title)
		agents = append(agents, sessionAgent(session))
	}
	return agents
}

func sessionAgent(session opencode.Session) config.Agent {
	name := session.Title
	if name == "" {
		name = session.ID
	}
	return config.Agent{Name: name, Session: session.ID, Dir: session.Directory, Parent: session.ParentID}
}

func selectMaster(agents []config.Agent, id string) error {
	if id == "" {
		return nil
	}
	for i := range agents {
		if agents[i].Session == id {
			agents[i].Role = "lead"
			return nil
		}
	}
	return fmt.Errorf("master session not discovered")
}

func newSession(args []string) error {
	f := flag.NewFlagSet("new", flag.ContinueOnError)
	dir := f.String("dir", "", "session directory")
	title := f.String("title", "agent-deck session", "session title")
	file := f.String("prompt-file", "", "optional UTF-8 prompt file")
	server := f.String("server", "", "opencode server")
	if err := f.Parse(args); err != nil {
		return fmt.Errorf("parse new-session flags: %w", err)
	}
	if *dir == "" {
		return fmt.Errorf("--dir required")
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return fmt.Errorf("resolve directory %q: %w", *dir, err)
	}
	var prompt []byte
	if *file != "" {
		prompt, err = os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf("read prompt %q: %w", *file, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := opencode.New(resolveServer(*server, ""), false)
	session, err := client.Create(ctx, abs, *title)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	fmt.Println(session.ID)
	if len(prompt) > 0 {
		if err := client.Prompt(ctx, abs, session.ID, string(prompt)); err != nil {
			return fmt.Errorf("send initial prompt: %w", err)
		}
	}
	return nil
}
