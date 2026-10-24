package opencode

import (
	"context"
	"fmt"
	"net/url"
)

// Permission describes an unresolved tool authorization request.
type Permission struct {
	Tool struct {
		MessageID string `json:"messageID"`
		CallID    string `json:"callID"`
	} `json:"tool"`
	ID         string   `json:"id"`
	SessionID  string   `json:"sessionID"`
	Permission string   `json:"permission"`
	Patterns   []string `json:"patterns"`
}

// Question describes an unresolved interactive question from an agent.
type Question struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Questions []struct {
		Header   string `json:"header"`
		Question string `json:"question"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
}

func (c *Client) Permissions(ctx context.Context, dir string) ([]Permission, error) {
	var v []Permission
	err := c.request(ctx, "GET", "/permission", dir, nil, &v)
	return v, err
}

func (c *Client) Questions(ctx context.Context, dir string) ([]Question, error) {
	var v []Question
	err := c.request(ctx, "GET", "/question", dir, nil, &v)
	return v, err
}

func (c *Client) ReplyPermission(ctx context.Context, dir, id, reply, message string) error {
	if reply != "once" && reply != "always" && reply != "reject" {
		return fmt.Errorf("invalid permission reply %q", reply)
	}
	return c.request(ctx, "POST", "/permission/"+url.PathEscape(id)+"/reply", dir, struct {
		Reply   string `json:"reply"`
		Message string `json:"message,omitempty"`
	}{reply, message}, nil)
}
