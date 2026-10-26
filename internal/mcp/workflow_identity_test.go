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
	if filepath.Dir(master.ControlDir) == filepath.Dir(worker.ControlDir) {
		t.Fatal("members share discoverable parent")
	}
	parentInfo, _ := os.Stat(filepath.Dir(master.ControlDir))
	if parentInfo.Mode().Perm() != 0o700 {
		t.Fatal("random parent is not private")
	}
	rootInfo, _ := os.Stat(filepath.Dir(filepath.Dir(master.ControlDir)))
	if rootInfo.Mode().Perm() != 0o711 {
		t.Fatal("common parent is listable")
	}
	master.PreviousSession, master.PreviousDir = "prior-session", repo
	if !strings.Contains(master.Instructions(), "prior-session") || !strings.Contains(master.Instructions(), master.Repo) {
		t.Fatal("missing source/transcript instructions")
	}
	for _, identity := range []*WorkflowIdentity{&master, &worker} {
		info, _ := os.Stat(identity.ControlDir)
		if info.Mode().Perm() != 0o700 {
			t.Fatal("public control dir")
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
	alias := filepath.Join(root, "alias")
	os.Symlink(t.TempDir(), alias)
	if _, err := PrepareWorkflowIdentity(t.Context(), alias, "member", repo); err == nil {
		t.Fatal("symlink identity accepted")
	}
}
