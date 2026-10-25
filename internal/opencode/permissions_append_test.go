package opencode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestPermissionAppendSemantics(t *testing.T) {
	var native []Rule
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/session/ses_append" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			Permission []Rule `json:"permission"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		native = append(native, body.Permission...)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := New(server.URL, false)
	policy := []Rule{
		{Permission: "*", Pattern: "*", Action: "ask"},
		{Permission: "edit", Pattern: "*", Action: "deny"},
		{Permission: "edit", Pattern: "owned/**", Action: "allow"},
	}
	for range 2 {
		if err := client.request(t.Context(), http.MethodPatch, "/session/ses_append", "/repo",
			map[string]any{"permission": policy}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(native) != 2*len(policy) || !slices.Equal(native[:len(policy)], native[len(policy):]) {
		t.Fatalf("append-semantic peer did not retain duplicate resets: %+v", native)
	}
	if err := client.request(t.Context(), http.MethodPatch, "/session/ses_append", "/repo",
		map[string]any{"permission": []Rule{}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(native) != 2*len(policy) {
		t.Fatal("empty PATCH unexpectedly cleared native policy")
	}
	revoke := []Rule{
		{Permission: "*", Pattern: "*", Action: "ask"},
		{Permission: "edit", Pattern: "*", Action: "deny"},
	}
	if err := client.request(t.Context(), http.MethodPatch, "/session/ses_append", "/repo",
		map[string]any{"permission": revoke}, nil); err != nil {
		t.Fatal(err)
	}
	if native[len(native)-1].Action != "deny" || len(native) != 2*len(policy)+len(revoke) {
		t.Fatal("later complete reset did not retain history and end with edit deny")
	}
}
