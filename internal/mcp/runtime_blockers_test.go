package mcp

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qivs/agent-deck/internal/channel"
)

func TestRuntimeAliveAndRosterSeedAcrossRestart(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	if !r.Alive() {
		t.Fatal("new runtime is not alive")
	}
	before, _ := r.ReadUser(t.Context())
	options := r.options
	r.Close()
	if r.Alive() {
		t.Fatal("closed runtime is alive")
	}
	next, err := StartRuntime(t.Context(), &options)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := next.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := next.ReadUser(t.Context())
	if len(before) != len(after) {
		t.Fatal("restart rebroadcast unchanged roster")
	}
}

func TestRuntimeRefusesMissingMembershipAndUsesMutedField(t *testing.T) {
	f := newRuntimeFixture(t)
	f.manager.Team.Members[0].ID = ""
	_, err := StartRuntime(t.Context(), &RuntimeOptions{
		Provider: f.provider, JournalPath: filepath.Join(t.TempDir(), "journal"),
	})
	if err == nil || !strings.Contains(err.Error(), "save/migrate") {
		t.Fatal("implicit identity migration", err)
	}
	f.manager.Team.Members[0].ID = "persistent-id"
	f.manager.Team.Members[0].Muted = true
	r := startTestRuntime(t, f)
	if r.members()[0].ID != "persistent-id" || !r.members()[0].Muted {
		t.Fatal("membership fields ignored")
	}
}

func TestRuntimeMissingCredentialConfigRevokedAndDropped(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	identity := Identity{Team: f.manager.Team.Name, Member: "bob"}
	token, _ := r.Tokens.Issue(identity, true)
	r.credentialMu.Lock()
	r.credentials["bob"] = runtimeCredential{
		identity: identity, token: token, config: filepath.Join(t.TempDir(), "gone", "opencode.json"),
	}
	r.credentialMu.Unlock()
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Tokens.Authenticate(token); ok {
		t.Fatal("token retained")
	}
	if len(r.credentials) != 0 {
		t.Fatal("removed config blocked revocation")
	}
}

func TestRuntimeStateOutsideWorktreeAndNoRemoteCredentialActions(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	dir := t.TempDir()
	c := &runtimeCredential{
		dir: dir, config: filepath.Join(dir, "opencode.json"), entry: "test", token: "token",
		lockPath: runtimeStatePath(r.options.StateDir, filepath.Join(dir, "opencode.json"), "config.lock"),
	}
	if err := installRuntimeConfig(c, r.Endpoint()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "opencode.json" {
		t.Fatal("lock leaked into worktree", entries)
	}
	if !strings.HasPrefix(discoveryPath(&r.options), r.options.StateDir+string(filepath.Separator)) {
		t.Fatal("discovery outside private state")
	}
	if _, err := r.userAction(t.Context(), &userRequest{Action: "install", Member: "bob"}); err == nil {
		t.Fatal("owner API bypassed isolated-worktree requirement")
	}
}

func TestRuntimeIncrementalUserReadsAndInitializeMetadata(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	first, err := r.ReadUserSince(t.Context(), "", 1)
	if err != nil || len(first) != 1 {
		t.Fatal("initial user page", first, err)
	}
	if err := r.PostUser(t.Context(), "next", nil); err != nil {
		t.Fatal(err)
	}
	next, err := r.ReadUserSince(t.Context(), first[0].ID, 1)
	if err != nil || len(next) != 1 || next[0].Text != "next" {
		t.Fatal("incremental user page", next, err)
	}
	if _, err := r.ReadUserSince(t.Context(), "missing", 1); err == nil {
		t.Fatal("bad cursor accepted")
	}
	w := httptest.NewRecorder()
	(&Server{}).initialize(w, &request{ID: json.RawMessage(`1`), Params: json.RawMessage(
		`{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{},"_meta":{"trace":"ok"}}`)})
	if strings.Contains(w.Body.String(), `"error"`) {
		t.Fatal("initialize metadata rejected", w.Body)
	}
}

func TestRuntimeRevocationErrorsDoNotBlockRosterAndDropCredentials(t *testing.T) {
	f := newRuntimeFixture(t)
	journal, err := channel.Open(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	manager, err := f.provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{
		options: RuntimeOptions{Provider: f.provider}, manager: manager,
		journal: journal, Tokens: NewTokens(), credentials: make(map[string]runtimeCredential),
	}
	r.Service, err = NewService(&Options{
		Team: manager.Team.Name, Journal: journal,
		Members: r.members, ReadOnly: r.readOnly, Transport: r, Instructions: r.instructions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, _ := r.ReadUser(t.Context())
	path := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(path, []byte(`broken`), 0o600)
	identity := Identity{Team: f.manager.Team.Name, Member: "bob"}
	token, _ := r.Tokens.Issue(identity, true)
	r.credentialMu.Lock()
	r.credentials["bob"] = runtimeCredential{identity: identity, token: token, config: path}
	r.credentialMu.Unlock()
	f.mu.Lock()
	f.manager.Team.Members[0].Role = "master"
	f.mu.Unlock()
	if err := r.Step(t.Context()); err == nil {
		t.Fatal("cleanup error lost")
	}
	if _, ok := r.Tokens.Authenticate(token); ok {
		t.Fatal("cleanup error retained token")
	}
	r.credentialMu.Lock()
	remaining := len(r.credentials)
	r.credentialMu.Unlock()
	if remaining != 0 {
		t.Fatal("cleanup error retained revoked credential")
	}
	after, _ := r.ReadUser(t.Context())
	if len(after) != len(before)+1 {
		t.Fatal("cleanup error blocked roster broadcast")
	}
}
