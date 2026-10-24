package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestWorkflowResetGuardsAndRetainsBothTranscripts(t *testing.T) {
	busy, readOnly := false, false
	mutations := 0
	control := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			mutations++
			t.Error("reset must not abort, delete or rewrite either transcript", request.Method, request.URL)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.URL.Query().Get("directory") != control {
			t.Error("reset status is not control-directory scoped", request.URL)
		}
		switch request.URL.Path {
		case "/session/status":
			if busy {
				fmt.Fprint(w, `{"control-session":{"type":"busy"}}`)
			} else {
				fmt.Fprint(w, `{}`)
			}
		case "/session":
			fmt.Fprint(w, `[{"id":"control-session"}]`)
		case "/permission", "/question":
			fmt.Fprint(w, `[]`)
		default:
			t.Error("unexpected reset request", request.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	store := team.Store{Root: t.TempDir()}
	repo := t.TempDir()
	tm := team.Team{
		Version: 1, Name: "reset", Server: server.URL, Repo: repo, Base: "main", Capacity: 1,
		Members: []team.Member{{
			Name: "lead", Role: "master", Mode: "shared", Dir: repo,
			Capability: "coder", Permissions: team.Preset("coder"), Session: "prior-session",
		}},
	}
	version, err := store.Save(t.Context(), &tm, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{
		options: RuntimeOptions{StateDir: t.TempDir()}, manager: &team.Manager{Team: tm},
		Tokens: NewTokens(), credentials: make(map[string]runtimeCredential),
	}
	identity, err := PrepareWorkflowIdentity(t.Context(), r.workflowRoot(tm.Name), tm.Members[0].ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	control = identity.ControlDir
	tm.Members[0].PreviousSession, tm.Members[0].PreviousDir = "prior-session", repo
	tm.Members[0].ControlDir, tm.Members[0].Session = control, "control-session"
	tm.Members[0].LeasePolicy = "installed-control-policy"
	if _, err := store.Save(t.Context(), &tm, version); err != nil {
		t.Fatal(err)
	}
	r.options.Provider = func(ctx context.Context) (*team.Manager, error) {
		loaded, version, err := store.Load(ctx, tm.Name)
		return &team.Manager{Store: store, Team: loaded, Version: version, ReadOnly: readOnly}, err
	}
	auth := Identity{Team: tm.Name, Member: "lead"}
	token, err := r.Tokens.Issue(auth, true)
	if err != nil {
		t.Fatal(err)
	}
	c := runtimeCredential{
		memberID: tm.Members[0].ID, identity: auth, dir: control, session: "control-session",
		role: "master", config: filepath.Join(control, "opencode.json"), entry: "agent_deck_reset", token: token,
		lockPath: filepath.Join(t.TempDir(), "config.lock"),
	}
	if err := installRuntimeConfig(&c, "http://127.0.0.1:1234/mcp"); err != nil {
		t.Fatal(err)
	}
	r.credentials["lead"] = c
	readOnly = true
	if err := r.ResetWorkflow(t.Context(), "lead"); err == nil {
		t.Fatal("read-only reset accepted")
	}
	readOnly, busy = false, true
	if err := r.ResetWorkflow(t.Context(), "lead"); err == nil {
		t.Fatal("busy reset accepted")
	}
	busy = false
	outside, v, _ := store.Load(t.Context(), tm.Name)
	outside.Members[0].ControlDir = t.TempDir()
	if _, err := store.Save(t.Context(), &outside, v); err != nil {
		t.Fatal(err)
	}
	if err := r.ResetWorkflow(t.Context(), "lead"); err == nil {
		t.Fatal("outside control path accepted")
	}
	outside, v, _ = store.Load(t.Context(), tm.Name)
	outside.Members[0].ControlDir = control
	if _, err := store.Save(t.Context(), &outside, v); err != nil {
		t.Fatal(err)
	}
	if err := r.ResetWorkflow(t.Context(), "lead"); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.Load(t.Context(), tm.Name)
	if err != nil {
		t.Fatal(err)
	}
	member := &loaded.Members[0]
	if member.Session != "prior-session" || member.ControlDir != "" || member.Dir != repo ||
		member.PreviousSession != "control-session" || member.PreviousDir != control || member.LeasePolicy != "" {
		t.Fatal("reset did not restore and preserve bindings", member)
	}
	if _, valid := r.Tokens.Authenticate(token); valid || mutations != 0 {
		t.Fatal("reset retained credential or mutated transcripts")
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatal("reset removed code checkout", err)
	}
}
