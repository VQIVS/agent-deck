package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestWorkflowRenameKeepsControlAndRemovalCleansIt(t *testing.T) {
	r := &Runtime{
		options: RuntimeOptions{StateDir: t.TempDir()}, Tokens: NewTokens(),
		credentials: make(map[string]runtimeCredential),
	}
	identity, err := PrepareWorkflowIdentity(t.Context(), r.workflowRoot("test"), "stable-id", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	auth := Identity{Team: "test", Member: "old-name"}
	token, _ := r.Tokens.Issue(auth, true)
	c := runtimeCredential{
		memberID: "stable-id", identity: auth, dir: identity.ControlDir, session: "session", role: "worker",
		config: filepath.Join(identity.ControlDir, "opencode.json"), entry: "agent_deck_test", token: token,
		lockPath: filepath.Join(t.TempDir(), "config.lock"),
	}
	if err := installRuntimeConfig(&c, "http://127.0.0.1:1234/mcp"); err != nil {
		t.Fatal(err)
	}
	r.credentials["old-name"] = c
	key, _, exists := r.findCredential("stable-id", "new-name")
	if !exists || key != "old-name" {
		t.Fatal("rename lost immutable credential lookup")
	}
	manager := &team.Manager{Team: team.Team{Name: "test", Members: []team.Member{{
		ID: "stable-id", Name: "new-name",
		Role: "worker", Mode: "shared", ControlDir: identity.ControlDir, Session: "session",
	}}}}
	if err := r.revokeChanged(manager); err != nil {
		t.Fatal(err)
	}
	if _, valid := r.Tokens.Authenticate(token); valid {
		t.Fatal("renamed old identity token retained")
	}
	if _, err := os.Stat(identity.ControlDir); err != nil {
		t.Fatal("rename deleted the control directory the session still uses", err)
	}
	token, _ = r.Tokens.Issue(Identity{Team: "test", Member: "new-name"}, true)
	c.identity, c.token = Identity{Team: "test", Member: "new-name"}, token
	if err := installRuntimeConfig(&c, "http://127.0.0.1:1234/mcp"); err != nil {
		t.Fatal(err)
	}
	r.credentials["new-name"] = c
	if err := r.revokeChanged(&team.Manager{Team: team.Team{Name: "test"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(identity.ControlDir); !os.IsNotExist(err) {
		t.Fatal("removed member's empty control not removed", err)
	}
	if _, err := os.Stat(filepath.Dir(identity.ControlDir)); !os.IsNotExist(err) {
		t.Fatal("empty random parent retained", err)
	}
}

func TestWorkflowCleanupPreservesUserFilesAndCheckout(t *testing.T) {
	r := &Runtime{options: RuntimeOptions{StateDir: t.TempDir()}}
	repo := t.TempDir()
	identity, err := PrepareWorkflowIdentity(t.Context(), r.workflowRoot("test"), "stable-id", repo)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(identity.ControlDir, "user.txt")
	os.WriteFile(path, []byte("keep"), 0o600)
	c := runtimeCredential{
		memberID: "stable-id", identity: Identity{Team: "test"}, dir: identity.ControlDir,
		config: filepath.Join(identity.ControlDir, "opencode.json"),
	}
	r.removeEmptyWorkflowControl(&c)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("user file removed", err)
	}
	c.dir = repo
	r.removeEmptyWorkflowControl(&c)
	if _, err := os.Stat(repo); err != nil {
		t.Fatal("checkout removed", err)
	}
}
