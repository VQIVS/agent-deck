package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// DisconnectMCP closes an MCP server connection on the directory's instance; a missing server is fine.
func (c *Client) DisconnectMCP(ctx context.Context, dir, name string) error {
	err := c.request(ctx, http.MethodPost, "/mcp/"+url.PathEscape(name)+"/disconnect", dir, nil, nil)
	if err != nil && !strings.Contains(err.Error(), "404") {
		return fmt.Errorf("disconnect MCP server %q: %w", name, err)
	}
	return nil
}

// RegisterMCP adds or replaces an MCP server on the directory's running instance.
func (c *Client) RegisterMCP(ctx context.Context, dir, name string, config any) error {
	body := map[string]any{"name": name, "config": config}
	if err := c.request(ctx, http.MethodPost, "/mcp", dir, body, nil); err != nil {
		return fmt.Errorf("register MCP server %q: %w", name, err)
	}
	return nil
}
