package mcp

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeConfigRecoversRecordedEndpointAfterCrash(t *testing.T) {
	dir := t.TempDir()
	c := &runtimeCredential{
		dir: dir, config: filepath.Join(dir, "opencode.json"), entry: "agent_deck_test",
		token: "old-token", lockPath: filepath.Join(t.TempDir(), "config.lock"),
	}
	if err := installRuntimeConfig(c, "http://127.0.0.1:1234/mcp"); err != nil {
		t.Fatal(err)
	}
	c.token = "new-token"
	if err := installRuntimeConfig(c, "http://127.0.0.1:5678/mcp"); err != nil {
		t.Fatal("crash reinstall failed", err)
	}
	config, _, _ := loadRuntimeConfig(c.config)
	servers, _ := configServers(config)
	if !ownedRuntimeEntry(servers[c.entry], "new-token") || !strings.Contains(string(servers[c.entry]), ":5678/mcp") {
		t.Fatal("entry not rotated", servers)
	}
	if err := removeRuntimeConfig(c); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeOwnerAttachmentCredentialActionsAndMemberDenied(t *testing.T) {
	f := newRuntimeFixture(t)
	root := t.TempDir()
	worktree := filepath.Join(root, "worker")
	os.Mkdir(worktree, 0o700)
	bin := t.TempDir()
	git := `#!/bin/sh
case "$*" in
  *--show-toplevel*) printf '%s\n' "$3" ;;
  *--git-common-dir*) printf '%s\n' '` + root + `/common.git' ;;
  *check-ignore*) exit 0 ;;
  *) exit 1 ;;
esac
`
	os.WriteFile(filepath.Join(bin, "git"), []byte(git), 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	f.manager.Team.Repo = root
	f.manager.Team.Members[0].Mode = "worktree"
	f.manager.Team.Members[0].Dir = worktree
	r := startTestRuntime(t, f)
	connection, err := OpenRuntime(t.Context(), &RuntimeOptions{Provider: f.provider, JournalPath: r.options.JournalPath})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.InstallCredential(t.Context(), "bob"); err != nil {
		t.Fatal("authenticated owner install", err)
	}
	if _, err := os.Stat(filepath.Join(worktree, "opencode.json")); err != nil {
		t.Fatal(err)
	}
	memberToken, _ := r.Tokens.Issue(Identity{Team: f.manager.Team.Name, Member: "bob"}, true)
	for _, token := range []string{"forged", memberToken} {
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost,
			strings.TrimSuffix(r.Endpoint(), "/mcp")+"/user", strings.NewReader(`{"action":"revoke","member":"bob"}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatal("nonowner credential accessed user action")
		}
	}
	if err := connection.RevokeCredential(t.Context(), "bob"); err != nil {
		t.Fatal("authenticated owner revoke", err)
	}
}
