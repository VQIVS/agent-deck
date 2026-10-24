package opencode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterMCPScopedToDirectory(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/mcp" || r.URL.Query().Get("directory") != "/ctl" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	config := map[string]any{"type": "remote", "url": "http://127.0.0.1:1/mcp"}
	if err := New(server.URL, false).RegisterMCP(t.Context(), "/ctl", "agent_deck_pool", config); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "agent_deck_pool" || got["config"] == nil {
		t.Fatalf("body=%v", got)
	}
}
