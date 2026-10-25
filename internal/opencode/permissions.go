package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
)

// MaxNativePermissionRules bounds future appends; it does not compact existing history.
const MaxNativePermissionRules = 8192

func (c *Client) permissionAppendNeeded(ctx context.Context, dir, id string, rules []Rule) (bool, error) {
	if c.ReadOnly {
		return false, fmt.Errorf("read-only: applying permissions disabled")
	}
	if len(rules) == 0 {
		return false, nil
	}
	var session struct {
		ID         string          `json:"id"`
		Permission json.RawMessage `json:"permission"`
	}
	if err := c.request(ctx, http.MethodGet, "/session/"+url.PathEscape(id), dir, nil, &session); err != nil {
		return false, fmt.Errorf("read native permissions for session %q: %w", id, err)
	}
	if session.ID != id {
		return false, fmt.Errorf("read native permissions: expected session %q, got %q", id, session.ID)
	}
	var current []Rule
	if len(session.Permission) != 0 && string(session.Permission) != "null" {
		if err := json.Unmarshal(session.Permission, &current); err != nil {
			return false, fmt.Errorf("decode native permissions for session %q: %w", id, err)
		}
	}
	if len(current) >= len(rules) && slices.Equal(current[len(current)-len(rules):], rules) {
		return false, nil
	}
	if len(current) > MaxNativePermissionRules-len(rules) {
		return false, fmt.Errorf("session %q permission history limit: current %d + incoming %d exceeds %d; "+
			"policy remains pending, no rules appended; explicit session migration or upstream reset support required",
			id, len(current), len(rules), MaxNativePermissionRules)
	}
	return true, nil
}
