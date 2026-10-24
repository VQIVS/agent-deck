package gitinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObserveTaskReadOnlyGitArgumentsAndEnvironment(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "commands")
	script := `#!/bin/sh
if [ -n "$GIT_DIR" ] || [ -n "$GIT_INDEX_FILE" ]; then exit 91; fi
printf '%s\n' "$*" >> "$PROBE_LOG"
case "$*" in
 *'rev-parse --verify --end-of-options'*) printf '%040d\n' 1 ;;
 *'rev-parse --verify HEAD'*) printf '%040d\n' 2 ;;
 *'symbolic-ref'*) printf 'feature\n' ;;
 *'rev-list'*) printf '2\n' ;;
 *'log --format'*) printf '%040d\000touch owned\000\n' 2 ;;
 *) exit 92 ;;
esac
`
	os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PROBE_LOG", log)
	t.Setenv("GIT_DIR", "wrong")
	t.Setenv("GIT_INDEX_FILE", "wrong")
	facts, err := ObserveTask(t.Context(), t.TempDir(), "main", []string{"internal/ui/**", "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Branch != "feature" || facts.Ahead != 2 || len(facts.Commits) != 1 ||
		facts.Commits[0].Subject != "touch owned" {
		t.Fatal("wrong observed facts", facts)
	}
	commands, _ := os.ReadFile(log)
	if !strings.Contains(string(commands), "-- :(top)internal/ui/** :(top)README.md") {
		t.Fatal("missing bounded pathspec separation", string(commands))
	}
}

func TestTaskPathspecsRejectInjectionAndTraversal(t *testing.T) {
	for _, pattern := range []string{
		"../escape", "a/../b", "/absolute", ":(exclude)*", "--all", ".git/config", "a\x00b", "a[bc]",
	} {
		if _, err := taskPathspecs([]string{pattern}); err == nil {
			t.Fatal("unsafe path accepted", pattern)
		}
	}
	if _, err := ObserveTask(t.Context(), t.TempDir(), "--all", []string{"ok"}); err == nil {
		t.Fatal("option ref accepted")
	}
	if _, err := taskPathspecs(nil); err == nil {
		t.Fatal("empty paths allowed unscoped history")
	}
}
