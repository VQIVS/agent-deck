package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRepository(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(
			Environment(),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.invalid",
		)
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	write := func(name, text string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); e != nil {
			t.Fatal(e)
		}
	}
	git("init", "--initial-branch=main")
	write("tracked.txt", "initial\n")
	write("rename.txt", "rename me\n")
	git("add", ".")
	git("-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-m", "initial")
	base := git("rev-parse", "HEAD")
	git("checkout", "-b", "feature")
	write("tracked.txt", "committed\n")
	git("add", ".")
	git("-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-m", "feature work")
	check := func(base string, want int) {
		t.Helper()
		v := Read(t.Context(), dir, base)
		if v.Error != "" || v.Branch != "feature" || v.Subject != "feature work" || !v.HasBase || v.Ahead != want {
			t.Fatalf("base=%q: %+v", base, v)
		}
	}
	check("main", 1)
	check("", 1) // default main fallback
	git("branch", "--set-upstream-to=main", "feature")
	check("", 1)
	// Use a different upstream tip to prove upstream wins over default main.
	git("branch", "upstream-tip", "HEAD")
	git("branch", "--set-upstream-to=upstream-tip", "feature")
	check("", 0)
	check("main", 1)
	git("branch", "--unset-upstream")
	git("update-ref", "refs/remotes/origin/trunk", base)
	git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	check("", 1)
	write("tracked.txt", "dirty\n")
	write("new file.txt", "untracked\n")
	git("mv", "rename.txt", "renamed file.txt")
	v := Read(t.Context(), dir, "main")
	if v.Dirty != 3 {
		t.Fatalf("expected modified + untracked + rename: %+v", v)
	}
	if v = Read(t.Context(), dir, "missing-base"); !strings.Contains(v.Error, "unavailable") || v.HasBase {
		t.Fatal(v)
	}
}

func TestReadNonRepository(t *testing.T) {
	v := Read(t.Context(), t.TempDir(), "")
	if v.Branch != "" || v.Error != "" || v.HasBase || v.Dirty != 0 || v.String() != "" {
		t.Fatal(v)
	}
}
