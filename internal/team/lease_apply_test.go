package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/gitinfo"
	api "github.com/vqivs/agent-deck/internal/opencode"
)

func leasedManager(t *testing.T, server string) *Manager {
	t.Helper()
	repo := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "-C", repo, "init", "--initial-branch=main")
	cmd.Env = gitinfo.Environment()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(b), err)
	}
	tm := example()
	tm.Repo, tm.Server = repo, server
	for i := range tm.Members {
		tm.Members[i].Dir = repo
		tm.Members[i].Capability = "coder"
		tm.Members[i].Permissions = Preset("coder")
	}
	tm.Members[0].Session = "master"
	tm.Members[1].Session = "worker"
	tm.Members[1].Leases = []string{"internal/tests/**"}
	m := &Manager{Store: Store{Root: t.TempDir()}, Team: tm}
	if err := m.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestApplyLeasesPreservesSessionAndSkipsUnchanged(t *testing.T) {
	var rules []api.Rule
	var prompts []string
	creates, patches := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches++
			var body struct{ Permission []api.Rule }
			_ = json.NewDecoder(r.Body).Decode(&body)
			rules = body.Permission
			if r.URL.Path != "/session/worker" {
				t.Error("patched wrong session")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch {
		case r.URL.Path == "/session" && r.Method == http.MethodPost:
			var body struct{ Permission []api.Rule }
			_ = json.NewDecoder(r.Body).Decode(&body)
			rules = body.Permission
			creates++
			fmt.Fprintf(w, `{"id":"fresh-%d"}`, creates)
		case r.URL.Path == "/session":
			fmt.Fprint(w, `[{"id":"worker"},{"id":"master"}]`)
		case r.URL.Path == "/session/status":
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/permission" || r.URL.Path == "/question" || strings.HasSuffix(r.URL.Path, "/message"):
			fmt.Fprint(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/prompt_async"):
			var body struct{ System string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			prompts = append(prompts, body.System)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected mutation: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m := leasedManager(t, server.URL)
	if err := m.ApplyLeases(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if creates != 0 || patches != 1 || m.Team.Members[1].Session != "worker" {
		t.Fatal("apply did not preserve session")
	}
	assertLeaseRule(t, rules, "*", "*", "ask")
	assertLeaseRule(t, rules, "edit", "*", "deny")
	assertLeaseRule(t, rules, "edit", "internal/tests/**", "allow")
	assertLeaseRule(t, rules, "bash", "git commit*", "deny")
	loaded, _, err := m.Store.Load(t.Context(), m.Team.Name)
	if err != nil || loaded.Members[1].Session != "worker" || len(prompts) != 0 {
		t.Fatal("session or context changed", err)
	}
	if err := m.ApplyLeases(t.Context(), 1); err != nil || patches != 1 {
		t.Fatal("unchanged policy patched again", err)
	}
	m.Team.Members[1].Session = ""
	if err := m.Start(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	assertLeaseRule(t, rules, "edit", "internal/tests/**", "allow")
	if m.Team.LeaseStatus(&m.Team.Members[1]) != "leases applied" {
		t.Fatal("fresh binding not marked applied")
	}
	m.Team.Members[1].Leases = nil
	if m.Team.LeaseStatus(&m.Team.Members[1]) != "leases PENDING apply" {
		t.Fatal("revoked policy not marked pending")
	}
	if err := m.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := m.creationRules(t.Context(), &m.Team.Members[1])
	if err != nil || !m.Team.LeasePool {
		t.Fatal("pool disabled when final lease removed", err)
	}
	assertLeaseRule(t, current, "edit", "*", "deny")
	assertLeaseRule(t, current, "bash", "git commit*", "deny")
}

func assertLeaseRule(t *testing.T, rules []api.Rule, permission, pattern, action string) {
	t.Helper()
	for _, rule := range rules {
		if rule.Permission == permission && rule.Pattern == pattern && rule.Action == action {
			return
		}
	}
	t.Fatalf("missing rule %s %s %s", permission, pattern, action)
}

func TestApplyLeasesGuards(t *testing.T) {
	m := leasedManager(t, "http://127.0.0.1:1")
	m.ReadOnly = true
	if err := m.ApplyLeases(t.Context(), 1); err == nil {
		t.Fatal("read-only apply accepted")
	}
	m.ReadOnly = false
	m.Team.Members[1].Capability = "inherited"
	if err := m.ApplyLeases(t.Context(), 1); err == nil {
		t.Fatal("inherited policy silently replaced")
	}
	m.Team.Members[1].Capability = "coder"
	if err := m.ApplyLeases(t.Context(), 1); err == nil {
		t.Fatal("offline member replaced")
	}
}
