package opencode

import (
	"context"
	"net/url"
	"sort"
)

// Rule sets a creation-time tool permission for a matching resource pattern.
type Rule struct {
	Permission string `json:"permission"`
	Pattern    string `json:"pattern"`
	Action     string `json:"action"`
}

// PromptOptions configures a message's model, agent, system prompt, and content.
type PromptOptions struct {
	Parts  []map[string]string `json:"parts"`
	System string              `json:"system,omitempty"`
	Agent  string              `json:"agent,omitempty"`
	Model  *ModelRef           `json:"model,omitempty"`
}

// ModelRef selects a provider and model supported by OpenCode.
type ModelRef struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

func (c *Client) CreateMember(ctx context.Context, dir, title string, rules []Rule) (Session, error) {
	var s Session
	e := c.request(ctx, "POST", "/session", dir, map[string]any{"title": title, "permission": rules}, &s)
	return s, e
}

func (c *Client) PromptWith(ctx context.Context, dir, id string, options PromptOptions) error {
	return c.request(ctx, "POST", "/session/"+url.PathEscape(id)+"/prompt_async", dir, options, nil)
}

func (c *Client) Catalog(ctx context.Context, dir string) (models, names []string, err error) {
	var providers struct {
		All []struct {
			ID     string         `json:"id"`
			Models map[string]any `json:"models"`
		} `json:"all"`
	}
	if e := c.request(ctx, "GET", "/provider", dir, nil, &providers); e != nil {
		return nil, nil, e
	}
	var agents []struct {
		Name string `json:"name"`
	}
	if e := c.request(ctx, "GET", "/agent", dir, nil, &agents); e != nil {
		return nil, nil, e
	}
	for _, p := range providers.All {
		for id := range p.Models {
			models = append(models, p.ID+"/"+id)
		}
	}
	for _, a := range agents {
		names = append(names, a.Name)
	}
	sort.Strings(models)
	sort.Strings(names)
	return models, names, nil
}
