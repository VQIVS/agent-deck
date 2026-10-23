package state

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func TestSummarizeScopedModelAndReadOnly(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/session/s/summarize" ||
			r.URL.Query().Get("directory") != "/test workspace" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var model api.ModelRef
		if err := json.NewDecoder(r.Body).Decode(&model); err != nil ||
			model.ProviderID != "provider" || model.ModelID != "model" {
			t.Errorf("unexpected model: %+v, %v", model, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	c := api.New(server.URL, false)
	model := api.ModelRef{ProviderID: "provider", ModelID: "model"}
	if err := c.Summarize(context.Background(), "/test workspace", "s", model); err != nil {
		t.Fatal(err)
	}
	if c.HTTP.Timeout != 20*time.Second {
		t.Fatal("summarize changed shared client timeout")
	}
	c.ReadOnly = true
	if c.Summarize(context.Background(), "/test workspace", "s", model) == nil || calls != 1 {
		t.Fatal("read-only summarize reached server")
	}
	c.ReadOnly = false
	if c.Summarize(context.Background(), "/test workspace", "s", api.ModelRef{}) == nil || calls != 1 {
		t.Fatal("missing session model reached server")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Summarize(ctx, "/test workspace", "s", model) == nil {
		t.Fatal("canceled summarize succeeded")
	}
}
