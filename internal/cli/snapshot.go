package cli

import (
	"context"
	"fmt"

	"github.com/qivs/agent-deck/internal/config"
	"github.com/qivs/agent-deck/internal/opencode"
)

func printSnapshot(ctx context.Context, deck *config.Deck, discover bool) error {
	client := opencode.New(deck.Server, true)
	if discover {
		inventory, err := client.Discover(ctx)
		if err != nil {
			return fmt.Errorf("discover snapshot sessions: %w", err)
		}
		for _, session := range inventory.Sessions {
			if inventory.Active[session.Directory+"\x00"+session.ID] {
				deck.Sessions = append(deck.Sessions, sessionAgent(session))
			}
		}
		fmt.Printf("Discovered %d sessions across %d directories; %d active\n",
			len(inventory.Sessions), len(inventory.Dirs), len(deck.Sessions))
	}
	for _, agent := range deck.Agents() {
		if err := printAgent(ctx, client, &agent); err != nil {
			return err
		}
	}
	return nil
}

func printAgent(ctx context.Context, client *opencode.Client, agent *config.Agent) error {
	statuses, err := client.Status(ctx, agent.Dir)
	if err != nil {
		fmt.Printf("%s OFFLINE: %v\n", agent.Name, err)
		return nil
	}
	status := statuses[agent.Session].Type
	if status == "" {
		status = "idle"
	}
	messages, err := client.Messages(ctx, agent.Dir, agent.Session)
	if err != nil {
		return fmt.Errorf("read session %q: %w", agent.Session, err)
	}
	fmt.Printf("%s %s · %d recent messages\n", agent.Name, status, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		if printParts(messages[i].Parts) {
			break
		}
	}
	return nil
}

func printParts(parts []opencode.Part) bool {
	found := false
	for i := range parts {
		part := &parts[i]
		if part.Type == "text" && part.Text != "" {
			fmt.Println(part.Text)
			found = true
		}
		if part.Type == "tool" && part.State.Status == "running" {
			fmt.Printf("tool: %s %s\n", part.Tool, part.State.Input)
			found = true
		}
	}
	return found
}
