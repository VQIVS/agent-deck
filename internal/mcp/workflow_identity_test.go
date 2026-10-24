package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowControlDirectoriesKeepSharedCheckoutSeparate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "identities")
	repo := t.TempDir()
	master, err := PrepareWorkflowIdentity(t.Context(), root, "master-id", repo)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := PrepareWorkflowIdentity(t.Context(), root, "worker-id", repo)
	if err != nil {
		t.Fatal(err)
	}
	if master.ControlDir == worker.ControlDir || master.Repo != worker.Repo {
		t.Fatal("identity directory alias")
	}
	master.PreviousSession, master.PreviousDir = "prior-session", repo
	if !strings.Contains(master.Instructions(), "prior-session") || !strings.Contains(master.Instructions(), master.Repo) {
		t.Fatal("missing source/transcript instructions")
	}
	state := t.TempDir()
	for _, identity := range []*WorkflowIdentity{&master, &worker} {
		err := InstallWorkflowCredential(t.Context(), identity, state, "test", "http://127.0.0.1:1234/mcp", identity.MemberID)
		if err != nil {
			t.Fatal(err)
		}
		config, _, err := loadRuntimeConfig(filepath.Join(identity.ControlDir, "opencode.json"))
		if err != nil {
			t.Fatal(err)
		}
		servers, _ := configServers(config)
		if !ownedRuntimeEntry(servers["agent_deck_test"], identity.MemberID) {
			t.Fatal("shared member token")
		}
		info, _ := os.Stat(identity.ControlDir)
		if info.Mode().Perm() != 0o700 {
			t.Fatal("public control dir")
		}
		if len(identity.Permissions()) != 2 {
			t.Fatal("missing explicit repo permissions")
		}
	}
	entries, _ := os.ReadDir(repo)
	if len(entries) != 0 {
		t.Fatal("shared checkout config modified")
	}
}

func TestWorkflowIdentityRefusesUnsafeRoots(t *testing.T) {
	repo := t.TempDir()
	for _, id := range []string{"", "../escape", "a/b"} {
		if _, err := PrepareWorkflowIdentity(t.Context(), t.TempDir(), id, repo); err == nil {
			t.Fatal("unsafe ID", id)
		}
	}
	if _, err := PrepareWorkflowIdentity(t.Context(), repo, "member", repo); err == nil {
		t.Fatal("control config inside checkout")
	}
	root := t.TempDir()
	os.Symlink(t.TempDir(), filepath.Join(root, "member"))
	if _, err := PrepareWorkflowIdentity(t.Context(), root, "member", repo); err == nil {
		t.Fatal("symlink identity accepted")
	}
}
