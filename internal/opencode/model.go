package opencode

import (
	"context"
	"fmt"
)

func (c *Client) ContextLimit(ctx context.Context, dir string, model ModelRef) (int64, error) {
	var catalog struct {
		Providers []struct {
			ID     string `json:"id"`
			Models map[string]struct {
				Limit struct {
					Context int64 `json:"context"`
				} `json:"limit"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := c.request(ctx, "GET", "/config/providers", dir, nil, &catalog); err != nil {
		return 0, fmt.Errorf("read model context limit: %w", err)
	}
	for _, provider := range catalog.Providers {
		if provider.ID == model.ProviderID {
			return provider.Models[model.ModelID].Limit.Context, nil
		}
	}
	return 0, nil
}
