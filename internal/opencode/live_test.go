//go:build live

package opencode

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLive(t *testing.T) {
	server := os.Getenv("AGENT_DECK_SERVER")
	if server == "" {
		server = "http://127.0.0.1:4096"
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client := New(server, true)
	inventory, err := client.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range inventory.Dirs {
		if _, err := client.Status(ctx, dir); err != nil {
			t.Errorf("status: %v", err)
		}
		if _, err := client.Permissions(ctx, dir); err != nil {
			t.Errorf("permissions: %v", err)
		}
		if _, err := client.Questions(ctx, dir); err != nil {
			t.Errorf("questions: %v", err)
		}
	}
	t.Logf("read-only discovery: %d sessions across %d directories", len(inventory.Sessions), len(inventory.Dirs))
}
