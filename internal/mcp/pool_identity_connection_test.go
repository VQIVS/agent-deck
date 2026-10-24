package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestPoolIdentityAttachmentRefreshesBindingsAndChecksRevision(t *testing.T) {
	r, manager := poolIdentityFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/session":
			if req.Method == http.MethodGet {
				fmt.Fprint(w, `[]`)
			} else {
				fmt.Fprint(w, `{"id":"attached-session"}`)
			}
		case "/mcp":
			fmt.Fprint(w, `{}`)
		default:
			t.Error("unexpected", req.URL)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	manager.Team.Server = server.URL
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.manager = manager
	r.options.Provider = func(ctx context.Context) (*team.Manager, error) {
		loaded, version, err := manager.Store.Load(ctx, manager.Team.Name)
		return &team.Manager{Store: manager.Store, Team: loaded, Version: version}, err
	}
	r.userToken = "owner-only"
	owner := httptest.NewServer(http.HandlerFunc(r.serveUser))
	defer owner.Close()
	connection := &RuntimeConnection{
		state:  runtimeDiscovery{Endpoint: owner.URL + "/mcp", Token: r.userToken},
		client: owner.Client(),
	}
	stale := *manager
	if err := connection.SetupPoolWorker(t.Context(), manager, 0); err != nil {
		t.Fatal(err)
	}
	if manager.Team.Members[0].Session != "attached-session" || manager.Team.Members[0].ControlDir == "" {
		t.Fatal("attachment did not refresh callback manager")
	}
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal("attachment returned stale save revision", err)
	}
	if err := connection.SetupPoolWorker(t.Context(), &stale, 0); err == nil {
		t.Fatal("stale attachment revision accepted")
	}
	connection.readOnly = true
	if err := connection.SetupPoolWorker(t.Context(), manager, 0); err == nil {
		t.Fatal("read-only attachment setup accepted")
	}
}
