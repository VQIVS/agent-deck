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

	"github.com/vqivs/agent-deck/internal/channel"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestWorkflowSetupPreservesOldBindingAndScopedLease(t *testing.T) {
	f := newRuntimeFixture(t)
	journal, err := channel.Open(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	r := &Runtime{
		options: RuntimeOptions{StateDir: t.TempDir()}, journal: journal, Tokens: NewTokens(),
		credentials: make(map[string]runtimeCredential), URL: "http://127.0.0.1:1234/mcp",
	}
	manager, _ := f.provider(t.Context())
	bin := t.TempDir()
	git := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q\n", manager.Team.Members[0].Dir)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(git), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r.manager = manager
	manager.Store = team.Store{Root: t.TempDir()}
	manager.Team.Version, manager.Team.Capacity = 1, 2
	manager.Team.Members[0].Session = ""
	manager.Team.Members[0].ID = ""
	manager.Team.Members[0].Role = "master"
	manager.Team.Members[0].Capability = "coder"
	manager.Team.Members[0].Permissions = team.Preset("coder")
	created := false
	prompted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "POST" && req.URL.Path == "/session/new-session/prompt_async" {
			prompted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if req.Method != "POST" || req.URL.Path != "/session" {
			t.Error("unexpected", req.Method, req.URL)
			w.WriteHeader(500)
			return
		}
		var payload map[string]json.RawMessage
		json.NewDecoder(req.Body).Decode(&payload)
		if len(payload["permission"]) == 0 || req.URL.Query().Get("directory") == manager.Team.Members[0].Dir {
			t.Error("control scope or rules missing")
		}
		created = true
		fmt.Fprint(w, `{"id":"new-session"}`)
	}))
	defer server.Close()
	manager.Team.Server = server.URL
	version, err := manager.Store.Save(t.Context(), &manager.Team, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.Version = version
	r.options.Provider = func(ctx context.Context) (*team.Manager, error) {
		tm, v, err := manager.Store.Load(ctx, manager.Team.Name)
		return &team.Manager{Store: manager.Store, Team: tm, Version: v}, err
	}
	before, version, err := manager.Store.Load(t.Context(), manager.Team.Name)
	if err != nil {
		t.Fatal(err)
	}
	before.Members[0].Mode = "worktree"
	if _, err := manager.Store.Save(t.Context(), &before, version); err == nil {
		t.Fatal("worktree member saved")
	}
	before.Members[0].Mode = "shared"
	before.Members[0].SystemFile = filepath.Join(t.TempDir(), "missing-system")
	if _, err := manager.Store.Save(t.Context(), &before, version); err != nil {
		t.Fatal(err)
	}
	if err := r.SetupWorkflow(t.Context(), "bob"); err == nil {
		t.Fatal("missing system file accepted")
	}
	if created || len(r.credentials) != 0 {
		t.Fatal("preflight failure produced identity side effects")
	}
	before, version, err = manager.Store.Load(t.Context(), manager.Team.Name)
	if err != nil {
		t.Fatal(err)
	}
	if before.Members[0].ControlDir != "" || before.Members[0].PreviousSession != "" {
		t.Fatal("failed setup changed prior binding")
	}
	before.Members[0].SystemFile = ""
	if _, err := manager.Store.Save(t.Context(), &before, version); err != nil {
		t.Fatal(err)
	}
	if err := r.SetupWorkflow(t.Context(), "bob"); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := manager.Store.Load(t.Context(), manager.Team.Name)
	if err != nil || !created || !prompted || loaded.Members[0].Session != "new-session" ||
		loaded.Members[0].ControlDir == "" {
		t.Fatal("workflow binding not persisted", loaded, err)
	}
	if loaded.Members[0].Dir != manager.Team.Members[0].Dir {
		t.Fatal("code directory replaced")
	}
	fingerprint := loaded.PolicyFingerprint(&loaded.Members[0])
	if loaded.Members[0].LeasePolicy == "" || loaded.Members[0].LeasePolicy != fingerprint {
		t.Fatal("installed control policy fingerprint missing")
	}
	if _, err := os.Stat(filepath.Join(loaded.Members[0].ControlDir, "opencode.json")); err != nil {
		t.Fatal(err)
	}
}
