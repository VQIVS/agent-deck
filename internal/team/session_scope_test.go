package team

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlSessionUsesAPIScopeAndKeepsCodeCheckout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != "/private/control" {
			t.Error("request used code dir", r.URL)
		}
		switch r.URL.Path {
		case "/session/status":
			fmt.Fprint(w, `{}`)
		case "/session":
			fmt.Fprint(w, `[{"id":"controlled"}]`)
		case "/session/controlled/prompt_async":
			w.WriteHeader(http.StatusNoContent)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer server.Close()
	tm := example()
	tm.Server = server.URL
	tm.Members = tm.Members[:1]
	member := &tm.Members[0]
	member.Session, member.ControlDir = "controlled", "/private/control"
	manager := Manager{Team: tm}
	if err := manager.Send(t.Context(), 0, "hello"); err != nil {
		t.Fatal(err)
	}
	if status := manager.Observe(t.Context(), 0); status.State != "idle" || status.Directory != "/private/control" {
		t.Fatal(status)
	}
	deck := tm.Deck()
	if deck.Sessions[0].Dir != "/private/control" || deck.Sessions[0].CodeDir != "/tmp/project" {
		t.Fatal(deck)
	}
	options, err := Options(t.Context(), &tm, member, "hello")
	if err != nil || !strings.Contains(options.System, "Code checkout: \"/tmp/project\"") {
		t.Fatal(options, err)
	}
}
