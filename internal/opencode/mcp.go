package opencode

import (
	"context"
	"fmt"
	"net/http"
)

// RegisterMCP adds or replaces an MCP server on the directory's running instance.
func (c *Client) RegisterMCP(ctx context.Context, dir, name string, config any) error {
	body := map[string]any{"name": name, "config": config}
	if err := c.request(ctx, http.MethodPost, "/mcp", dir, body, nil); err != nil {
		return fmt.Errorf("register MCP server %q: %w", name, err)
	}
	return nil
}
