package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeRosterRefreshAndEndpointLifecycle(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	before, _ := r.ReadUser(t.Context())
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := r.ReadUser(t.Context())
	if len(before) != len(after) {
		t.Fatal("unchanged roster broadcast repeated")
	}
	f.mu.Lock()
	f.manager.Team.Members[0].Role = "master"
	f.mu.Unlock()
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	updated, _ := r.ReadUser(t.Context())
	if len(updated) != len(before)+1 || r.members()[0].Role != "master" {
		t.Fatal("current snapshot/roster did not refresh")
	}
	client := &http.Client{Timeout: time.Second}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, r.Endpoint(), http.NoBody)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("unauthenticated endpoint allowed", response.StatusCode)
	}
	r.Close()
	request, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, r.Endpoint(), http.NoBody)
	if response, err := client.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("endpoint survived runtime shutdown")
	}
	if err := r.PostUser(t.Context(), "after close", nil); err == nil {
		t.Fatal("closed runtime accepted mutation")
	}
}

func TestRuntimeRevokesChangedBindingAndPreservesUserReplacement(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	identity := Identity{Team: f.manager.Team.Name, Member: "bob"}
	token, err := r.Tokens.Issue(identity, true)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c := runtimeCredential{
		identity: identity, dir: dir, config: filepath.Join(dir, "opencode.json"),
		entry: "agent_deck_test", token: token, session: "session-bob", role: "worker",
	}
	if err := installRuntimeConfig(&c, r.Endpoint()); err != nil {
		t.Fatal(err)
	}
	r.credentialMu.Lock()
	r.credentials["bob"] = c
	r.credentialMu.Unlock()
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Tokens.Authenticate(token); ok {
		t.Fatal("changed binding retained token")
	}
	config, _, err := loadRuntimeConfig(c.config)
	if err != nil {
		t.Fatal(err)
	}
	servers, _ := configServers(config)
	if len(servers) != 0 {
		t.Fatal("stale credential config remains")
	}
	if err := installRuntimeConfig(&c, r.Endpoint()); err != nil {
		t.Fatal(err)
	}
	config, original, _ := loadRuntimeConfig(c.config)
	servers, _ = configServers(config)
	servers[c.entry] = json.RawMessage(`{"type":"remote","url":"https://user.example"}`)
	config["mcp"], _ = json.Marshal(servers)
	if err := writeRuntimeConfig(c.config, config, original); err != nil {
		t.Fatal(err)
	}
	if err := removeRuntimeConfig(&c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(c.config)
	if !strings.Contains(string(b), "user.example") {
		t.Fatal("revocation removed user replacement")
	}
}

func TestRuntimeConfigSymlinkAndConcurrentChangeRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte(`{}`), 0o600)
	os.Symlink(target, path)
	c := &runtimeCredential{dir: dir, config: path, entry: "agent_deck_test", token: "secret"}
	if err := installRuntimeConfig(c, "http://127.0.0.1:1/mcp"); err == nil {
		t.Fatal("symlink config overwritten")
	}
	os.Remove(path)
	os.WriteFile(path, []byte(`{"model":"new"}`), 0o600)
	if err := writeRuntimeConfig(path, map[string]json.RawMessage{}, []byte(`{}`)); err == nil {
		t.Fatal("concurrent user config change overwritten")
	}
}
