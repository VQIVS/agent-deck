package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func diffRepository(t *testing.T) (root string, run func(...string), put func(string, string)) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(Environment(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "--initial-branch=main")
	return dir, git, write
}

func TestFileDiffPreservesStagedAndUnstaged(t *testing.T) {
	dir, git, write := diffRepository(t)
	write("file.txt", "original\n")
	git("add", ".")
	git("-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-m", "initial")
	write("file.txt", "staged\n")
	git("add", ".")
	write("file.txt", "original\n")
	diff, err := FileDiff(t.Context(), dir, "file.txt")
	if err != nil || !strings.Contains(diff.Text, "Staged changes") ||
		!strings.Contains(diff.Text, "Unstaged changes") || !strings.Contains(diff.Text, "+staged") {
		t.Fatalf("diff=%+v err=%v", diff, err)
	}
}

func TestFileDiffUnbornUntrackedAndBinary(t *testing.T) {
	dir, git, write := diffRepository(t)
	write("staged.txt", "new staged\n")
	git("add", ".")
	write("untracked.txt", "new text\n")
	write("binary.bin", "\x00binary")
	write("large.txt", strings.Repeat("a", (1<<20)+1))
	for _, tc := range []struct {
		path, text        string
		binary, truncated bool
	}{
		{"staged.txt", "+new staged", false, false},
		{"untracked.txt", "+new text", false, false},
		{"binary.bin", "", true, false},
		{"large.txt", "+aaa", false, true},
	} {
		diff, err := FileDiff(t.Context(), dir, tc.path)
		if err != nil || diff.Binary != tc.binary || diff.Truncated != tc.truncated ||
			!strings.Contains(diff.Text, tc.text) || len(diff.Text) > 1<<20 {
			t.Fatalf("%s: %+v err=%v", tc.path, diff, err)
		}
	}
}

func TestChangedFilesRenameAndUnsafePaths(t *testing.T) {
	files, err := parseChangedFiles([]byte("R  new name\x00old name\x00?? newline\nfile\x00"))
	if err != nil || len(files) != 2 || files[0].OldPath != "old name" || !files[0].Staged ||
		files[1].Path != "newline\nfile" || !files[1].Untracked {
		t.Fatalf("%+v %v", files, err)
	}
	dir := t.TempDir()
	for _, path := range []string{"../outside", "/absolute", ".git/config", ".", "a/../b"} {
		if _, err := safeDiffPath(dir, path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeDiffPath(dir, "link/file"); err == nil {
		t.Fatal("accepted symlink traversal")
	}
}

func TestChangedFilesInventoryAndLiteralPaths(t *testing.T) {
	dir, git, write := diffRepository(t)
	write("rename.txt", "rename text\n")
	write("deleted.txt", "deleted text\n")
	git("add", ".")
	git("-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-m", "initial")
	git("mv", "rename.txt", "renamed.txt")
	if err := os.Remove(filepath.Join(dir, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	write("[literal].txt", "literal text\n")
	root, files, err := ChangedFiles(t.Context(), dir)
	if err != nil || len(files) != 3 || root == "" {
		t.Fatalf("root=%s files=%+v err=%v", root, files, err)
	}
	for _, path := range []string{"renamed.txt", "deleted.txt", "[literal].txt"} {
		diff, err := FileDiff(t.Context(), dir, path)
		if err != nil || diff.Text == "" {
			t.Fatalf("%s: %+v %v", path, diff, err)
		}
	}
	if _, err := FileDiff(t.Context(), dir, "unchanged.txt"); err == nil {
		t.Fatal("accepted path outside inventory")
	}
}

func TestTrackedBinaryAndTruncatedDiff(t *testing.T) {
	dir, git, write := diffRepository(t)
	write("binary.bin", "\x00old")
	write("large.txt", "old\n")
	git("add", ".")
	git("-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-m", "initial")
	write("binary.bin", "\x00new")
	write("large.txt", strings.Repeat("new line\n", 140000))
	for _, tc := range []struct {
		path              string
		binary, truncated bool
	}{
		{"binary.bin", true, false}, {"large.txt", false, true},
	} {
		diff, err := FileDiff(t.Context(), dir, tc.path)
		if err != nil || diff.Binary != tc.binary || diff.Truncated != tc.truncated || len(diff.Text) > 1<<20 {
			t.Fatalf("%s: binary=%v truncated=%v bytes=%d err=%v", tc.path,
				diff.Binary, diff.Truncated, len(diff.Text), err)
		}
	}
}
