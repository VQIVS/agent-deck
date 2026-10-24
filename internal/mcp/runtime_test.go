package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qivs/agent-deck/internal/team"
)

type runtimeFixture struct {
	mu      sync.Mutex
	manager team.Manager
	busy    bool
	waiting string
	prompts []map[string]any
}

func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
	f := &runtimeFixture{busy: true}
	dir := t.TempDir()
	f.manager.Team = team.Team{
		Name: "runtime-test", Rules: "team rules", Model: "provider/default", Repo: dir, Base: "main",
		Members: []team.Member{{
			Name: "bob", Role: "worker", Session: "session-bob", Dir: dir,
			Agent: "build", Model: "provider/member", System: "worker rules", Mode: "shared",
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Query().Get("directory") != dir {
			t.Error("missing directory scope", r.URL)
			http.Error(w, "directory missing", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			fmt.Fprint(w, `[{"id":"session-bob"}]`)
		case "/session/status":
			if f.busy {
				fmt.Fprint(w, `{"session-bob":{"type":"busy"}}`)
			} else {
				fmt.Fprint(w, `{}`)
			}
		case "/permission", "/question":
			if f.waiting == r.URL.Path {
				fmt.Fprint(w, `[{"id":"request","sessionID":"session-bob"}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
		case "/session/session-bob/prompt_async":
			var prompt map[string]any
			if err := json.NewDecoder(r.Body).Decode(&prompt); err != nil {
				t.Error(err)
			}
			f.prompts = append(f.prompts, prompt)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Error("unexpected API request", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	f.manager.Team.Server = server.URL
	return f
}

func (f *runtimeFixture) provider(ctx context.Context) (*team.Manager, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	manager := f.manager
	manager.Team.Members = append([]team.Member(nil), f.manager.Team.Members...)
	return &manager, nil
}

func startTestRuntime(t *testing.T, f *runtimeFixture, configure ...func(*RuntimeOptions)) *Runtime {
	t.Helper()
	options := &RuntimeOptions{
		Provider: f.provider, JournalPath: filepath.Join(t.TempDir(), "channel"), Interval: time.Hour,
	}
	for _, apply := range configure {
		apply(options)
	}
	r, err := StartRuntime(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRuntimeTransportAndUserAPI(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	if r.Endpoint() != r.URL || !strings.HasPrefix(r.Endpoint(), "http://127.0.0.1:") {
		t.Fatal("invalid endpoint", r.Endpoint())
	}
	if err := r.PostUser(t.Context(), "user message", []string{"bob"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.prompts) != 0 {
		t.Fatal("busy member interrupted")
	}
	f.busy = false
	f.mu.Unlock()
	for _, waiting := range []string{"/permission", "/question"} {
		f.mu.Lock()
		f.waiting = waiting
		f.mu.Unlock()
		busy, err := r.Busy(t.Context(), "bob")
		if err != nil || !busy {
			t.Fatalf("waiting %s not busy: %v, %v", waiting, busy, err)
		}
		if err := r.Send(t.Context(), "bob", "do not send"); err == nil {
			t.Fatal("Send did not recheck waiting state")
		}
	}
	f.mu.Lock()
	f.waiting = ""
	f.mu.Unlock()
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.prompts) != 1 {
		t.Fatalf("prompts: %v", f.prompts)
	}
	prompt := f.prompts[0]
	f.mu.Unlock()
	if prompt["agent"] != "build" || !strings.Contains(prompt["system"].(string), "team rules") ||
		!strings.Contains(prompt["system"].(string), "worker rules") {
		t.Fatal("prompt options lost", prompt)
	}
	model := prompt["model"].(map[string]any)
	if model["modelID"] != "member" || model["providerID"] != "provider" {
		t.Fatal("member model lost", model)
	}
	records, err := r.ReadUser(t.Context())
	if err != nil || len(records) != 2 || records[1].Sender != "user" {
		t.Fatalf("user read: %v, %v", records, err)
	}
	if err := r.PostUser(t.Context(), "bad", []string{"unknown"}); err == nil {
		t.Fatal("invalid user mention accepted")
	}
}

func TestRuntimeSingleOwnerShutdownAndReadOnly(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	second, err := StartRuntime(t.Context(), &RuntimeOptions{Provider: f.provider, JournalPath: r.options.JournalPath})
	if err == nil {
		second.Close()
		t.Fatal("second journal owner allowed")
	}
	f.mu.Lock()
	f.manager.ReadOnly = true
	f.mu.Unlock()
	if err := r.PostUser(t.Context(), "blocked", nil); err == nil {
		t.Fatal("fresh read-only policy bypassed")
	}
	if err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.InstallCredential(t.Context(), "bob"); err == nil {
		t.Fatal("read-only credential install allowed")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal("close not idempotent", err)
	}
	for _, address := range []string{"0.0.0.0:0", ":0", "localhost:0"} {
		if listener, err := listenRuntime(t.Context(), address); err == nil {
			listener.Close()
			t.Fatal("nonexplicit-loopback listener allowed", address)
		}
	}
}

func TestRuntimeReportAdapterDerivesIdentity(t *testing.T) {
	f := newRuntimeFixture(t)
	f.manager.Team.DefinitionOfDone = team.DefinitionOfDone{{ID: "tests", Text: "test", Required: true}}
	called := false
	callback := func(
		_ context.Context, m *team.Manager, member string, report *team.DoneReport,
	) (any, error) {
		called = true
		if member != "bob" || report.Member != "bob" || report.Session != "session-bob" || m.Team.Name != "runtime-test" {
			t.Fatal("report identity not derived", report)
		}
		return "stored", nil
	}
	r := startTestRuntime(t, f, func(o *RuntimeOptions) { o.SubmitReport = callback })
	raw := json.RawMessage(`{"summary":"done","checklist":[{"id":"tests","complete":true,"evidence":"go test"}]}`)
	if _, err := r.submitReport(t.Context(), Identity{"runtime-test", "bob"}, raw); err != nil || !called {
		t.Fatal("report callback", err)
	}
	forged := json.RawMessage(`{"member":"bob","summary":"done","checklist":[]}`)
	if _, err := r.submitReport(t.Context(), Identity{"runtime-test", "bob"}, forged); err == nil {
		t.Fatal("caller supplied identity accepted")
	}
}

func TestRuntimeConfigMergeAndRevoke(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	original := `{"model":"provider/model","mcp":{"user_server":{"type":"remote","url":"https://example.com"}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &runtimeCredential{dir: dir, config: path, entry: "agent_deck_test", token: "secret"}
	if err := installRuntimeConfig(c, "http://127.0.0.1:1234/mcp"); err != nil {
		t.Fatal(err)
	}
	config, _, err := loadRuntimeConfig(path)
	if err != nil || string(config["model"]) != `"provider/model"` {
		t.Fatal("user configuration lost", config, err)
	}
	servers, _ := configServers(config)
	if len(servers) != 2 || !ownedRuntimeEntry(servers[c.entry], c.token) {
		t.Fatal("MCP merge failed", servers)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("credential config is not private", info.Mode())
	}
	if err := removeRuntimeConfig(c); err != nil {
		t.Fatal(err)
	}
	config, _, _ = loadRuntimeConfig(path)
	servers, _ = configServers(config)
	if len(servers) != 1 || servers["user_server"] == nil {
		t.Fatal("revocation changed user server", servers)
	}
}

func TestRuntimeCredentialRefusalsPreserveConfig(t *testing.T) {
	f := newRuntimeFixture(t)
	r := startTestRuntime(t, f)
	if err := r.InstallCredential(t.Context(), "bob"); err == nil {
		t.Fatal("shared member credential allowed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	c := &runtimeCredential{dir: dir, config: path, entry: "agent_deck_test", token: "secret"}
	for _, content := range []string{`{"mcp":{"agent_deck_test":{"type":"remote","url":"user"}}}`, `// comment
{}`} {
		os.WriteFile(path, []byte(content), 0o600)
		if err := installRuntimeConfig(c, "http://127.0.0.1:1/mcp"); err == nil {
			t.Fatal("ambiguous config overwritten")
		}
		got, _ := os.ReadFile(path)
		if string(got) != content {
			t.Fatal("refused config changed")
		}
	}
	os.Remove(path)
	os.WriteFile(filepath.Join(dir, "opencode.jsonc"), []byte("{}"), 0o600)
	if err := installRuntimeConfig(c, "http://127.0.0.1:1/mcp"); err == nil {
		t.Fatal("coexisting JSONC ignored")
	}
}
