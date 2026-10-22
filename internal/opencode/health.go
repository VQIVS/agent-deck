package opencode

import "context"

const MinimumVersion = "1.19.0"

func (c *Client) Health(ctx context.Context) error {
	return c.request(ctx, "GET", "/global/health", "", nil, nil)
}

func (c *Client) HasModels(ctx context.Context) (bool, error) {
	var catalog struct {
		Providers []struct {
			Models map[string]any `json:"models"`
		} `json:"providers"`
	}
	if err := c.request(ctx, "GET", "/config/providers", "", nil, &catalog); err != nil {
		return false, err
	}
	for _, provider := range catalog.Providers {
		if len(provider.Models) > 0 {
			return true, nil
		}
	}
	return false, nil
}
