package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func TestLeaseValidation(t *testing.T) {
	for _, pattern := range []string{"internal/team/lease*.go", "docs/**", "a?b", "*"} {
		if err := (Leases{pattern}).Validate(); err != nil {
			t.Errorf("valid pattern %q: %v", pattern, err)
		}
	}
	for _, pattern := range []string{
		"", "/tmp/file", "../file", "a/../file", "./file", ".", "a/", ".git/config",
		"a/.git/**", "a//b", "a\\b", "a\nb", " a", "a[bc]", "C:/file",
	} {
		if err := (Leases{pattern}).Validate(); err == nil {
			t.Errorf("accepted invalid pattern %q", pattern)
		}
	}
}

func TestLeaseOverlap(t *testing.T) {
	for _, pair := range [][2]string{
		{"internal/**", "internal/team/*.go"},
		{"a*.go", "ab.go"},
		{"a?b", "acb"},
		{"*", "README.md"},
		{"same.go", "same.go"},
		{"internal/a*", "internal/ab*"},
	} {
		members := []Member{{Name: "one", Leases: []string{pair[0]}}, {Name: "two", Leases: []string{pair[1]}}}
		if err := ValidateLeases(members); err == nil {
			t.Errorf("accepted overlapping leases %q", pair)
		}
	}
	members := []Member{
		{Name: "one", Leases: []string{"internal/team/**", "internal/team/lease*.go"}},
		{Name: "two", Leases: []string{"internal/ui/**"}},
	}
	if err := ValidateLeases(members); err != nil {
		t.Fatalf("disjoint members and same-member overlap: %v", err)
	}
}

func TestLeaseRules(t *testing.T) {
	master := Member{Name: "master", Role: "master", Permissions: Preset("full")}
	worker := Member{Name: "worker", Role: "worker", Permissions: Preset("full"), Leases: []string{"internal/a*.go"}}
	members := []Member{master, worker}
	rules, err := LeaseRules("/repo", members, &worker)
	if err != nil {
		t.Fatal(err)
	}
	assertLeaseAction(t, rules, "edit", "internal/a.go", "allow")
	assertLeaseAction(t, rules, "edit", "internal/deep/a.go", "deny")
	assertLeaseAction(t, rules, "edit", "README.md", "deny")
	assertLeaseAction(t, rules, "bash", "go test ./internal/team/...", "allow")
	assertLeaseAction(t, rules, "bash", "git status", "allow")
	for _, command := range []string{
		"git add .", "git -C /repo commit -m x", "/usr/bin/git push", "env X=1 git reset --hard",
	} {
		assertLeaseAction(t, rules, "bash", command, "deny")
	}
	rules, err = LeaseRules("/repo", members, &master)
	if err != nil {
		t.Fatal(err)
	}
	assertLeaseAction(t, rules, "edit", "internal/a.go", "deny")
	assertLeaseAction(t, rules, "edit", "README.md", "allow")
	assertLeaseAction(t, rules, "bash", "git commit -m x", "allow")
}

func TestLeaseEditReset(t *testing.T) {
	worker := Member{Name: "worker", Role: "worker", Permissions: Preset("coder"), Leases: []string{"old/**"}}
	old, err := LeaseRules("/repo", []Member{worker}, &worker)
	if err != nil {
		t.Fatal(err)
	}
	worker.Leases = []string{"new/**"}
	current, err := LeaseRules("/repo", []Member{worker}, &worker)
	if err != nil {
		t.Fatal(err)
	}
	combined := slices.Concat(old, current)
	assertLeaseAction(t, combined, "edit", "old/file.go", "deny")
	assertLeaseAction(t, combined, "edit", "new/file.go", "allow")
	master := Member{Name: "master", Role: "master", Capability: "inherited"}
	worker.Leases = []string{"old/**"}
	old, err = LeaseRules("/repo", []Member{master, worker}, &master)
	if err != nil {
		t.Fatal(err)
	}
	worker.Leases = []string{"new/**"}
	current, err = LeaseRules("/repo", []Member{master, worker}, &master)
	if err != nil {
		t.Fatal(err)
	}
	combined = slices.Concat(old, current)
	assertLeaseAction(t, combined, "edit", "old/file.go", "allow")
	assertLeaseAction(t, combined, "edit", "new/file.go", "deny")
	for _, rule := range current {
		if rule.Permission == "edit" && rule.Action == "allow" && strings.HasPrefix(rule.Pattern, "/") {
			t.Fatalf("unexpected absolute edit alias: %+v", rule)
		}
	}
}

func TestLeaseWorkerEditCeiling(t *testing.T) {
	for _, test := range []struct {
		name        string
		permissions map[string]string
		want        string
	}{
		{name: "reviewer", permissions: Preset("reviewer"), want: "deny"},
		{name: "ask", permissions: map[string]string{"edit": "ask"}, want: "ask"},
		{name: "coder", permissions: Preset("coder"), want: "allow"},
		{name: "full", permissions: Preset("full"), want: "allow"},
		{name: "inherited", want: "ask"},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker := Member{
				Name: "worker", Role: "worker", Permissions: test.permissions, Leases: []string{"owned/**"},
			}
			rules, err := LeaseRules("/repo", []Member{worker}, &worker)
			if err != nil {
				t.Fatal(err)
			}
			assertLeaseAction(t, rules, "edit", "owned/file.go", test.want)
			assertLeaseAction(t, rules, "edit", "other/file.go", "deny")
		})
	}
}

func TestWorkerPrivateRootDenies(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	worker := Member{Name: "worker", Role: "worker", Permissions: Preset("full"), Leases: []string{"owned/**"}}
	rules, err := LeaseRules("/repo", []Member{worker}, &worker)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{
		DefaultRoot(), filepath.Join(home, ".local", "state", "agent-deck"),
		"~/.config/agent-deck/teams", "$HOME/.local/state/agent-deck", "${HOME}/.config/agent-deck/teams",
	} {
		assertLeaseAction(t, rules, "read", root+"/secret.json", "deny")
		assertLeaseAction(t, rules, "external_directory", root+"/boards/pool", "deny")
		assertLeaseAction(t, rules, "bash", "cat "+root+"/secret.json", "deny")
	}
	assertLeaseAction(t, rules, "read", "/repo/internal/team/config.go", "allow")
	assertLeaseAction(t, rules, "bash", "go test ./internal/team/...", "allow")
}

func TestMasterPrivateRootDenies(t *testing.T) {
	master := Member{Name: "master", Role: "master", Permissions: Preset("full")}
	rules, err := LeaseRules("/repo", []Member{master}, &master)
	if err != nil {
		t.Fatal(err)
	}
	root := DefaultRoot()
	assertLeaseAction(t, rules, "read", root+"/team.yaml", "deny")
	assertLeaseAction(t, rules, "external_directory", root+"/boards/pool", "deny")
	for _, rule := range rules {
		if rule.Permission == "edit" && filepath.IsAbs(rule.Pattern) {
			t.Fatal("unverified absolute edit deny", rule)
		}
	}
	assertLeaseAction(t, rules, "bash", "cat "+root+"/team.yaml", "deny")
	assertLeaseAction(t, rules, "bash", "go test ./internal/team/...", "allow")
}

func assertLeaseAction(t *testing.T, rules []api.Rule, permission, target, want string) {
	t.Helper()
	action := "ask"
	for _, rule := range rules {
		if wildcardMatch(rule.Permission, permission) && wildcardMatch(rule.Pattern, target) {
			action = rule.Action
		}
	}
	if action != want {
		t.Errorf("%s %q: got %s, want %s", permission, target, action, want)
	}
}

func TestLeaseResetOverridesUnknownOldPermissions(t *testing.T) {
	worker := Member{Name: "worker", Role: "worker", Permissions: Preset("coder"), Leases: []string{"new/**"}}
	rules, err := LeaseRules("/repo", []Member{worker}, &worker)
	if err != nil {
		t.Fatal(err)
	}
	old := []api.Rule{
		{Permission: "custom", Pattern: "*", Action: "allow"},
		{Permission: "edit", Pattern: "old/**", Action: "allow"},
		{Permission: "webfetch", Pattern: "*", Action: "deny"},
	}
	combined := slices.Concat(old, rules)
	assertLeaseAction(t, combined, "custom", "anything", "ask")
	assertLeaseAction(t, combined, "edit", "old/file", "deny")
	assertLeaseAction(t, combined, "edit", "new/file", "allow")
	assertLeaseAction(t, combined, "webfetch", "https://example.com", "allow")
}

func wildcardMatch(pattern, target string) bool {
	expression := regexp.QuoteMeta(pattern)
	expression = strings.ReplaceAll(expression, `\*`, ".*")
	expression = strings.ReplaceAll(expression, `\?`, ".")
	matched, _ := regexp.MatchString("(?s)^"+expression+"$", target)
	return matched
}

func TestWorkerGitCommandBoundaries(t *testing.T) {
	rules := workerGitRules()
	for _, command := range []string{
		"git commit -m x", "cd a && git add .", "/usr/bin/git push",
		"git -C /x reset --hard", "git -c user.name=x commit -m x",
		"git --no-pager commit -m x", `sh -c "git commit -m x"`,
	} {
		assertLeaseAction(t, rules, "bash", command, "deny")
	}
	for _, command := range []string{
		"git diff -- internal/team", "git log --format=%h",
		"go test ./internal/gitinfo/ -run TestName", "git status --branch", "git show HEAD",
	} {
		assertLeaseAction(t, rules, "bash", command, "ask")
	}
}

func TestPoolSessionsCannotInvokePrivilegedCLI(t *testing.T) {
	for _, role := range []string{"master", "worker"} {
		member := Member{Name: role, Role: role, Permissions: Preset("full")}
		rules, err := LeaseRules("/repo", []Member{member}, &member)
		if err != nil {
			t.Fatal(err)
		}
		for _, command := range []string{
			"agent-deck team approve --id pool:x", "/usr/local/bin/agent-deck team board --file x", "true && agent-deck up",
		} {
			assertLeaseAction(t, rules, "bash", command, "deny")
		}
	}
}

func TestApplyLeasePermissions(t *testing.T) {
	calls := 0
	rules := []api.Rule{{Permission: "edit", Pattern: "internal/**", Action: "allow"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPatch || r.URL.EscapedPath() != "/session/ses_test%2Fid" ||
			r.URL.Query().Get("directory") != "/repo with space" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		var body struct {
			Permission []api.Rule `json:"permission"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Permission) != 1 ||
			body.Permission[0] != rules[0] {
			t.Errorf("permission body: %+v, %v", body, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	ctx := context.Background()
	if err := api.New(server.URL, false).ApplyPermissions(ctx, "/repo with space", "ses_test/id", rules); err != nil {
		t.Fatal(err)
	}
	if err := api.New(server.URL, true).ApplyPermissions(ctx, "/repo", "ses_test", rules); err == nil {
		t.Fatal("read-only PATCH accepted")
	}
	if err := api.New(server.URL, false).ApplyPermissions(ctx, "/repo", "", rules); err == nil {
		t.Fatal("empty session accepted")
	}
	if calls != 1 {
		t.Fatalf("got %d requests, want one", calls)
	}
}

func TestLeaseRulesAllowOwnTeamTools(t *testing.T) {
	reviewer := Member{Name: "rev", Role: "worker", Permissions: Preset("reviewer")}
	rules, err := LeaseRules("/repo", []Member{reviewer}, &reviewer)
	if err != nil {
		t.Fatal(err)
	}
	assertLeaseAction(t, rules, "agent_deck_pool_my_tasks", "*", "allow")
	assertLeaseAction(t, rules, "other_server_tool", "*", "ask")
}

func TestLeaseRulesSearchFollowsRead(t *testing.T) {
	coder := Member{Name: "w", Role: "worker", Permissions: Preset("coder")}
	rules, err := LeaseRules("/repo", []Member{coder}, &coder)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"grep", "glob", "list", "todowrite"} {
		assertLeaseAction(t, rules, tool, "*", "allow")
	}
	coder.Permissions["read"] = "ask"
	rules, err = LeaseRules("/repo", []Member{coder}, &coder)
	if err != nil {
		t.Fatal(err)
	}
	assertLeaseAction(t, rules, "grep", "*", "ask")
}

func TestReleaseWorkerGitRules(t *testing.T) {
	release := Member{
		Name: "rm", Role: "worker", Capability: "release", Permissions: Preset("release"),
		Leases: []string{"CHANGELOG.md"},
	}
	rules, err := LeaseRules("/repo", []Member{release}, &release)
	if err != nil {
		t.Fatal(err)
	}
	for _, allowed := range []string{
		"git tag -a v0.2.6 -m 'add-docs'", "git push origin main", "git push origin refs/tags/v0.2.6",
		"git commit CHANGELOG.md -m 'chore: prepare changelog'", "git fetch . feat/workflow:main",
		"git push origin feat/x-fix", "git worktree add /tmp/rel v0.2.6", "goreleaser release --clean",
	} {
		assertLeaseAction(t, rules, "bash", allowed, "allow")
	}
	for _, denied := range []string{
		"git push --force origin main", "git push origin main -f", "git push origin +main", "git reset --hard",
		"git checkout main", "git tag -d v0.2.5", "git branch -D feat/x", "git clean -fdx", "git stash",
	} {
		assertLeaseAction(t, rules, "bash", denied, "deny")
	}
	assertLeaseAction(t, rules, "edit", "CHANGELOG.md", "allow")
	assertLeaseAction(t, rules, "edit", "internal/ui/x.go", "deny")
	assertLeaseAction(t, rules, "external_directory", "/private/tmp/agent-deck-release-abc/*", "allow")
	assertLeaseAction(t, rules, "external_directory", "/Users/x/other/*", "ask")
	if prompt := LeasePrompt([]Member{release}); !strings.Contains(prompt, "release worker rm") ||
		!strings.Contains(prompt, "Release workers are the exception") {
		t.Fatalf("release prompt: %s", prompt)
	}
}
