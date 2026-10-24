package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func poolIdentityFixture(t *testing.T) (*Runtime, *team.Manager) {
	t.Helper()
	f := newRuntimeFixture(t)
	manager, err := f.provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	manager.Store = team.Store{Root: t.TempDir()}
	manager.Team.Version, manager.Team.Capacity = 1, 2
	member := &manager.Team.Members[0]
	member.Session, member.ID = "", ""
	member.Role, member.Mode, member.Capability = "worker", "shared", "coder"
	member.Permissions, member.PoolCreated = team.Preset("coder"), true
	master := *member
	master.Name, master.Role, master.PoolCreated = "lead", "master", false
	manager.Team.Members = append(manager.Team.Members, master)
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	git := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q\n", member.Dir)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(git), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &Runtime{
		options: RuntimeOptions{StateDir: t.TempDir()}, Tokens: NewTokens(),
		credentials: map[string]runtimeCredential{}, URL: "http://127.0.0.1:1234/mcp",
	}
	return r, manager
}

func TestPoolIdentityCreateRegistrationRetryAndRestart(t *testing.T) {
	r, manager := poolIdentityFixture(t)
	creates, registrations, disconnects := 0, 0, 0
	failRegistration := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		directory := req.URL.Query().Get("directory")
		if directory == manager.Team.Members[0].Dir || directory == "" {
			t.Error("request escaped private control directory", req.URL)
		}
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/session":
			fmt.Fprint(w, `[]`)
		case req.Method == http.MethodPost && req.URL.Path == "/session":
			creates++
			fmt.Fprint(w, `{"id":"private-session"}`)
		case req.Method == http.MethodPost && req.URL.Path == "/mcp/agent_deck_runtime-test/disconnect":
			disconnects++
			fmt.Fprint(w, `{}`)
		case req.Method == http.MethodPost && req.URL.Path == "/mcp":
			registrations++
			if disconnects != registrations {
				t.Error("registration must follow one disconnect per attempt", disconnects, registrations)
			}
			if failRegistration {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{}`)
		default:
			t.Error("unexpected request", req.Method, req.URL)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	manager.Team.Server = server.URL
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.SetupPoolWorker(t.Context(), manager, 0); err == nil {
		t.Fatal("registration failure hidden")
	}
	failRegistration = false
	if err := r.SetupPoolWorker(t.Context(), manager, 0); err != nil {
		t.Fatal(err)
	}
	member := manager.Team.Members[0]
	_, credential, ok := r.findCredential(member.ID, member.Name)
	if !ok || member.Session != "private-session" || creates != 1 || registrations != 2 || disconnects != 2 {
		t.Fatal("setup/retry duplicated binding", creates, registrations)
	}
	if identity, ok := r.Tokens.Authenticate(credential.token); !ok || identity.Member != member.Name {
		t.Fatal("new worker cannot authenticate immediately")
	}
	identity, _ := r.Tokens.Authenticate(credential.token)
	tools := &TaskTools{Provider: func(context.Context) (*team.Manager, team.BoardStore, error) {
		return manager, team.BoardStore{Root: t.TempDir(), Team: manager.Team.Name}, nil
	}}
	if _, err := tools.Call(t.Context(), identity, "my_tasks", json.RawMessage(`{}`)); err != nil {
		t.Fatal("new worker cannot immediately access board", err)
	}
	if _, err := os.Stat(filepath.Join(member.Dir, "opencode.json")); !os.IsNotExist(err) {
		t.Fatal("credential leaked into checkout", err)
	}
	loaded, version, err := manager.Store.Load(t.Context(), manager.Team.Name)
	if err != nil {
		t.Fatal(err)
	}
	manager.Team, manager.Version = loaded, version
	r.credentials, r.Tokens = map[string]runtimeCredential{}, NewTokens()
	if err := r.SetupPoolWorker(t.Context(), manager, 0); err != nil {
		t.Fatal("restart recovery", err)
	}
	if creates != 1 || len(r.credentials) != 1 {
		t.Fatal("restart created duplicate identity")
	}
	if err := r.SetupPoolWorker(t.Context(), manager, 0); err != nil || registrations != 3 || disconnects != 3 {
		t.Fatal("successful live registration was unnecessarily repeated", err, registrations)
	}
	_, old, _ := r.findCredential(member.ID, member.Name)
	manager.Team.Members[0].PreviousSession = member.Session
	manager.Team.Members[0].PreviousDir = member.ControlDir
	manager.Team.Members[0].Session, manager.Team.Members[0].ControlDir = "", ""
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.SetupPoolWorker(t.Context(), manager, 0); err != nil {
		t.Fatal("replacement identity", err)
	}
	replacement := manager.Team.Members[0]
	_, current, _ := r.findCredential(replacement.ID, replacement.Name)
	if creates != 2 || replacement.ControlDir == member.ControlDir || current.token == old.token ||
		replacement.PreviousSession != member.Session || replacement.PreviousDir != member.ControlDir {
		t.Fatal("replacement failed to rotate identity or preserve history")
	}
	if _, ok := r.Tokens.Authenticate(old.token); ok {
		t.Fatal("old credential remained valid after replacement")
	}
}

func TestPoolIdentityAuthorizationGates(t *testing.T) {
	for _, gate := range []string{"readonly", "user", "session", "inherited", "muted"} {
		t.Run(gate, func(t *testing.T) {
			r, manager := poolIdentityFixture(t)
			member := &manager.Team.Members[0]
			switch gate {
			case "readonly":
				manager.ReadOnly = true
			case "user":
				member.PoolCreated = false
			case "session":
				member.Session = "user-configured"
			case "inherited":
				member.Capability = "inherited"
			case "muted":
				member.Muted = true
			}
			if err := r.SetupPoolWorker(t.Context(), manager, 0); err == nil {
				t.Fatal("gate allowed setup")
			}
			if member.ControlDir != "" || len(r.credentials) != 0 {
				t.Fatal("gate produced side effects")
			}
		})
	}
}

func TestPoolIdentityRecoversSessionBeforeBindingSave(t *testing.T) {
	r, manager := poolIdentityFixture(t)
	identity, err := PrepareWorkflowIdentity(t.Context(), r.workflowRoot(manager.Team.Name),
		manager.Team.Members[0].ID, manager.Team.Members[0].Dir)
	if err != nil {
		t.Fatal(err)
	}
	manager.Team.Members[0].ControlDir = identity.ControlDir
	disconnected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == "/session" {
			json.NewEncoder(w).Encode([]map[string]string{{
				"id": "recovered", "title": manager.Team.Members[0].Name,
				"directory": identity.ControlDir,
			}})
			return
		}
		if req.Method == http.MethodPost && req.URL.Path == "/mcp/agent_deck_runtime-test/disconnect" {
			disconnected = true
			fmt.Fprint(w, `{}`)
			return
		}
		if req.Method == http.MethodPost && req.URL.Path == "/mcp" {
			if !disconnected {
				t.Error("recovered registration did not disconnect stale MCP")
			}
			fmt.Fprint(w, `{}`)
			return
		}
		t.Error("unexpected duplicate creation", req.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	manager.Team.Server = server.URL
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.SetupPoolWorker(t.Context(), manager, 0); err != nil {
		t.Fatal(err)
	}
	if manager.Team.Members[0].Session != "recovered" || r.validateControl(identity.MemberID, identity.ControlDir) != nil {
		t.Fatal("partial binding not recovered privately")
	}
}

func TestPoolIdentityLifecycleRegistersBeforeBrief(t *testing.T) {
	r, manager := poolIdentityFixture(t)
	worker := manager.Team.Members[0]
	manager.Team.Members = manager.Team.Members[1:]
	disconnected, registered, prompted := false, false, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/session":
			fmt.Fprint(w, `[]`)
		case req.Method == http.MethodPost && req.URL.Path == "/session":
			fmt.Fprint(w, `{"id":"new-private"}`)
		case req.Method == http.MethodPost && req.URL.Path == "/mcp/agent_deck_runtime-test/disconnect":
			disconnected = true
			fmt.Fprint(w, `{}`)
		case req.Method == http.MethodPost && req.URL.Path == "/mcp":
			if !disconnected {
				t.Error("lifecycle registration did not disconnect stale MCP")
			}
			registered = true
			fmt.Fprint(w, `{}`)
		case req.Method == http.MethodPost && req.URL.Path == "/session/new-private/prompt_async":
			prompted = true
			if !registered {
				t.Error("brief delivered before tools registered")
			}
			if req.URL.Query().Get("directory") == worker.Dir {
				t.Error("brief delivered to checkout session")
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Error("unexpected lifecycle request", req.Method, req.URL)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	manager.Team.Server = server.URL
	if err := manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	board := team.BoardStore{Root: t.TempDir(), Team: manager.Team.Name}
	state := team.BoardState{
		Version: 2, Board: team.TaskBoard{Version: 2},
		Policy: team.SchedulingPolicy{Approval: "auto", MaxWorkers: 2},
	}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	transport := &team.LifecycleTransport{
		Store: board, WorkerSetup: r.SetupPoolWorker,
		LoadManager: func(context.Context) (*team.Manager, error) { return manager, nil },
		Gate:        func(context.Context, *team.ReconcileAction) error { return nil },
	}
	worker.ID, worker.Session, worker.PoolCreated, worker.Brief = "", "", false, "task brief"
	request := &team.ReconcileRequest{Action: team.ReconcileAction{ID: "identity-create", Kind: "create"}, Member: worker}
	if _, err := transport.Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	index, err := manager.MemberIndex(worker.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Send(t.Context(), index, worker.Brief); err != nil {
		t.Fatal(err)
	}
	if !registered || !prompted || len(r.credentials) != 1 {
		t.Fatal("pool lifecycle omitted identity or brief")
	}
}
