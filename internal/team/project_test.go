package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testProject = `version: 1
name: example
capacity: 2
approval: allowlist
safe_commands:
  - git status --short
members:
  - name: master
    role: master
    capability: coder
    system: Coordinate the work
  - name: reviewer
    role: worker
    capability: reviewer
`

func TestProjectMergeKeepsBindingsAndAppliesPolicy(t *testing.T) {
	p, err := ParseProject([]byte(testProject))
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	local := Member{
		Name: "Master", Role: "master", Mode: "shared", Dir: repo,
		Session: "local-session", Branch: "local-branch", SystemFile: "/old/system",
		Capability: "full", Permissions: Preset("full"),
	}
	bindings := ProjectBindings{
		Repo: repo, Server: "http://127.0.0.1:9999", Base: "main",
		Members: map[string]Member{"master": local, "stale": {
			Name: "extra", Role: "worker", Mode: "shared", Dir: repo,
			Capability: "coder", Permissions: Preset("coder"), Session: "keep-me",
		}},
	}
	merged, err := p.Merge(bindings)
	if err != nil {
		t.Fatal(err)
	}
	master := merged.Members[0]
	if len(merged.Members) != 2 || master.Session != local.Session || master.Branch != local.Branch ||
		master.Role != "master" || master.Capability != "full" || master.SystemFile != "/old/system" ||
		master.ProjectSystem != "Coordinate the work" || merged.Base != "main" || merged.Approval != "" {
		t.Fatalf("bad merge: %+v", merged)
	}
	if merged.Members[1].Session != "keep-me" {
		t.Fatal("unmentioned member lost")
	}
	if bindings.Members["master"].SystemFile != "/old/system" {
		t.Fatal("merge mutated local input")
	}
	p.Base = "develop"
	merged, err = p.Merge(bindings)
	if err != nil || merged.Base != "main" {
		t.Fatalf("private base should win: %v", err)
	}
}

func TestProjectRejectsPrivateFields(t *testing.T) {
	for _, field := range []string{"repo: /machine/path", "server: http://localhost:4096", "session: ses_x"} {
		if _, err := ParseProject([]byte(testProject + field + "\n")); err == nil {
			t.Errorf("private project field accepted: %s", field)
		}
	}
	for _, field := range []string{"session: ses_x", "dir: /tmp/work", "system_file: /private/file", "branch: private"} {
		data := strings.Replace(testProject, "    system: Coordinate the work", "    "+field, 1)
		if _, err := ParseProject([]byte(data)); err == nil {
			t.Errorf("private member field accepted: %s", field)
		}
	}
	for _, data := range []string{
		testProject + "---\nversion: 1\n", testProject + "base: /machine/path\n",
		strings.Replace(testProject, "approval: allowlist", "approval: always", 1),
		strings.Replace(testProject, "name: reviewer", "name: master", 1),
		strings.Replace(testProject, "capability: reviewer", "capability: typo", 1),
	} {
		if _, err := ParseProject([]byte(data)); err == nil {
			t.Errorf("invalid project accepted: %s", data)
		}
	}
}

func TestLoadProject(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "agent-deck.yaml")
	if err := os.WriteFile(filename, []byte(testProject), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProject(context.Background(), filename); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadProject(ctx, filename); err == nil {
		t.Fatal("canceled load succeeded")
	}
}
