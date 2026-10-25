package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ApplyPermissions bounds native append history and skips an already-applied ordered policy suffix.
func (c *Client) ApplyPermissions(ctx context.Context, dir, id string, rules []Rule) error {
	if id == "" {
		return fmt.Errorf("apply permissions: session ID required")
	}
	needed, err := c.permissionAppendNeeded(ctx, dir, id, rules)
	if err != nil {
		return err
	}
	if !needed {
		return nil
	}
	body := struct {
		Permission []Rule `json:"permission"`
	}{Permission: rules}
	if err := c.request(ctx, http.MethodPatch, "/session/"+url.PathEscape(id), dir, body, nil); err != nil {
		return fmt.Errorf("apply permissions to session %q: %w", id, err)
	}
	return nil
}
