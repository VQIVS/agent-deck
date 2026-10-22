package opencode

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAndConfiguredModels(t *testing.T) {
	models := `{"providers":[{"models":{"example":{}}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			fmt.Fprint(w, `{"healthy":true}`)
		case "/config/providers":
			fmt.Fprint(w, models)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := New(server.URL, true)
	if err := client.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
	if ok, err := client.HasModels(t.Context()); err != nil || !ok {
		t.Fatalf("models=%t: %v", ok, err)
	}
	models = `{"providers":[]}`
	if ok, err := client.HasModels(t.Context()); err != nil || ok {
		t.Fatalf("empty models=%t: %v", ok, err)
	}
}
