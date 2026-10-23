package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func (c *Client) Summarize(ctx context.Context, dir, id string, model ModelRef) error {
	if id == "" || model.ProviderID == "" || model.ModelID == "" {
		return fmt.Errorf("summarize needs a session and its provider/model")
	}
	// Compaction can take minutes; keep normal request deadlines unchanged.
	h := *c.HTTP
	h.Timeout = 5 * time.Minute
	if transport, ok := h.Transport.(*http.Transport); ok {
		long := transport.Clone()
		long.ResponseHeaderTimeout = h.Timeout
		defer long.CloseIdleConnections()
		h.Transport = long
	}
	client := *c
	client.HTTP = &h
	return client.request(ctx, http.MethodPost, "/session/"+url.PathEscape(id)+"/summarize", dir, model, nil)
}
