package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/gitinfo"
	"github.com/vqivs/agent-deck/internal/team"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// Repository setup is isolated; production initialization only reads Git metadata.
	command := exec.CommandContext(t.Context(), "git", "-C", repo, "init", "--initial-branch=main")
	command.Env = gitinfo.Environment()
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatal(string(data), err)
	}
	return repo
}

func TestInitPrivateTeamWithoutLiveResources(t *testing.T) {
	repo := initTestRepo(t)
	subdir := filepath.Join(repo, "sub")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := projectGit(t.Context(), repo, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	args := []string{"--dir", subdir, "--team", "sample", "--template", "review-pair", "--server", "http://127.0.0.1:1"}
	if err := initCommand(t.Context(), args, &out); err != nil {
		t.Fatal(err)
	}
	store := team.Store{Root: team.DefaultRoot()}
	tm, _, err := store.Load(t.Context(), "sample")
	if err != nil || tm.Base != "main" || len(tm.Members) != 2 {
		t.Fatalf("initialized team: %+v, %v", tm, err)
	}
	if tm.Members[1].Capability != "reviewer" || tm.Members[1].Permissions["edit"] != "deny" ||
		tm.Members[1].System == "" || tm.Members[1].Brief == "" {
		t.Fatal("template settings lost", tm.Members[1])
	}
	root, err := filepath.EvalSymlinks(repo)
	if err != nil || tm.Repo != root {
		t.Fatal("did not resolve Git root", tm.Repo, err)
	}
	for i := range tm.Members {
		member := &tm.Members[i]
		if member.Session != "" || member.Owned || member.Mode != "shared" || member.Dir != tm.Repo {
			t.Fatal("unexpected implicit resources", member)
		}
	}
	info, err := os.Stat(store.Path("sample"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("private team file permissions", err)
	}
	after, err := projectGit(t.Context(), repo, "status", "--porcelain")
	if err != nil || before != after {
		t.Fatal("initialization changed repository", err)
	}
	if err := initCommand(t.Context(), args, &out); err == nil {
		t.Fatal("existing team overwritten")
	}
}

func TestInitExplicitBaseAndModelWin(t *testing.T) {
	repo := initTestRepo(t)
	var out bytes.Buffer
	args := []string{"--dir", repo, "--team", "flags", "--base", "develop", "--model", "private/model"}
	if err := initCommand(t.Context(), args, &out); err != nil {
		t.Fatal(err)
	}
	tm, _, err := (team.Store{Root: team.DefaultRoot()}).Load(t.Context(), "flags")
	if err != nil || tm.Base != "develop" || tm.Model != "private/model" {
		t.Fatal(tm, err)
	}
}

func TestInitGuardsAndTemplateListing(t *testing.T) {
	repo := initTestRepo(t)
	var out bytes.Buffer
	if err := initCommand(t.Context(), []string{"--list-templates", "--read-only"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "bugfix-tester") {
		t.Fatal("templates not listed")
	}
	for _, args := range [][]string{
		{"--dir", repo, "--read-only"},
		{"--dir", repo, "--template", "unknown"},
		{"--dir", t.TempDir()},
		{"unexpected"},
	} {
		if err := initCommand(t.Context(), args, &out); err == nil {
			t.Fatal("invalid initialization accepted", args)
		}
	}
	if _, err := os.Stat(team.DefaultRoot()); !os.IsNotExist(err) {
		t.Fatal("failed initialization created private team directory", err)
	}
}

func TestInitPortableProject(t *testing.T) {
	repo := initTestRepo(t)
	project := filepath.Join(t.TempDir(), "agent-deck.yaml")
	data := "version: 1\nname: portable\ncapacity: 1\nmembers:\n" +
		"  - name: lead\n    role: master\n    capability: coder\n"
	if err := os.WriteFile(project, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := initCommand(t.Context(), []string{"--dir", repo, "--project", project}, &out); err != nil {
		t.Fatal(err)
	}
	store := team.Store{Root: team.DefaultRoot()}
	tm, _, err := store.Load(t.Context(), "portable")
	if err != nil || tm.Members[0].Name != "lead" {
		t.Fatal("portable policy not applied", err)
	}
}

func TestInitAutodetectProjectAndExplicitPriority(t *testing.T) {
	repo := initTestRepo(t)
	data := "version: 1\nname: automatic\ncapacity: 1\napproval: allowlist\n" +
		"safe_commands: [go test ./internal/team/...]\nmembers:\n" +
		"  - name: lead\n    role: master\n    capability: coder\n"
	if err := os.WriteFile(filepath.Join(repo, "agent-deck.yaml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := initCommand(t.Context(), []string{"--dir", repo}, &out); err != nil {
		t.Fatal(err)
	}
	store := team.Store{Root: team.DefaultRoot()}
	if _, _, err := store.Load(t.Context(), "automatic"); err != nil {
		t.Fatal("autodetected policy not loaded", err)
	}
	explicit := filepath.Join(t.TempDir(), "explicit.yaml")
	if err := os.WriteFile(explicit, []byte(strings.Replace(data, "automatic", "explicit", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initCommand(t.Context(), []string{"--dir", repo, "--project", explicit}, &out); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(t.Context(), "explicit"); err != nil {
		t.Fatal("explicit policy did not take priority", err)
	}
}
