package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ApplyPermissions appends native rules to a session; OpenCode keeps earlier rules and later matches win.
func (c *Client) ApplyPermissions(ctx context.Context, dir, id string, rules []Rule) error {
	if id == "" {
		return fmt.Errorf("apply permissions: session ID required")
	}
	if rules == nil {
		rules = []Rule{}
	}
	body := struct {
		Permission []Rule `json:"permission"`
	}{Permission: rules}
	if err := c.request(ctx, http.MethodPatch, "/session/"+url.PathEscape(id), dir, body, nil); err != nil {
		return fmt.Errorf("apply permissions to session %q: %w", id, err)
	}
	return nil
}
