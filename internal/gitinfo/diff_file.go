package gitinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func FileDiff(ctx context.Context, dir, path string) (Diff, error) {
	root, files, err := ChangedFiles(ctx, dir)
	if err != nil {
		return Diff{}, err
	}
	absolute, err := safeDiffPath(root, path)
	if err != nil {
		return Diff{}, err
	}
	var selected *ChangedFile
	for i := range files {
		if files[i].Path == path {
			selected = &files[i]
			break
		}
	}
	if selected == nil {
		return Diff{}, fmt.Errorf("path is not a currently changed file")
	}
	if selected.Untracked {
		return untrackedDiff(ctx, absolute, path)
	}
	paths := []string{path}
	if selected.OldPath != "" {
		if _, err := safeDiffPath(root, selected.OldPath); err != nil {
			return Diff{}, err
		}
		paths = append(paths, selected.OldPath)
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--find-renames"}
	staged := append(append([]string(nil), args...), "--cached", "--")
	staged = append(staged, paths...)
	first, err := trackedDiff(ctx, root, staged)
	if err != nil {
		return Diff{}, err
	}
	unstaged := append(append([]string(nil), args...), "--")
	unstaged = append(unstaged, paths...)
	second, err := trackedDiff(ctx, root, unstaged)
	if err != nil {
		return Diff{}, err
	}
	if first.Text != "" {
		first.Text = "Staged changes\n" + first.Text
	}
	if second.Text != "" {
		second.Text = "Unstaged changes\n" + second.Text
	}
	return combineDiff(first, second), nil
}

func safeDiffPath(root, path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path ||
		strings.ContainsRune(path, 0) || path == "." {
		return "", fmt.Errorf("diff path must be clean and repository-relative")
	}
	current := root
	segments := strings.Split(filepath.ToSlash(path), "/")
	for _, segment := range segments {
		if segment == ".." || strings.EqualFold(segment, ".git") {
			return "", fmt.Errorf("diff path escapes repository or targets Git metadata")
		}
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect diff path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("diff path traverses a symlink")
		}
	}
	return current, nil
}

func trackedDiff(ctx context.Context, root string, args []string) (Diff, error) {
	data, truncated, err := diffGit(ctx, root, args...)
	if err != nil {
		return Diff{}, err
	}
	if bytes.Contains(data, []byte("Binary files ")) || bytes.Contains(data, []byte("GIT binary patch")) ||
		bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return Diff{Binary: true, Truncated: truncated}, nil
	}
	return Diff{Text: string(data), Truncated: truncated}, nil
}

func untrackedDiff(ctx context.Context, absolute, path string) (Diff, error) {
	if err := ctx.Err(); err != nil {
		return Diff{}, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return Diff{}, fmt.Errorf("open untracked diff: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return Diff{}, fmt.Errorf("inspect untracked diff: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Diff{}, fmt.Errorf("untracked diff requires regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return Diff{}, fmt.Errorf("read untracked diff: %w", err)
	}
	truncated := len(data) > 1<<20
	data = data[:min(len(data), 1<<20)]
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return Diff{Binary: true, Truncated: truncated}, nil
	}
	var text strings.Builder
	fmt.Fprintf(&text, "--- /dev/null\n+++ b/%s\n", path)
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if line != "" {
			text.WriteByte('+')
			text.WriteString(line)
		}
	}
	return combineDiff(Diff{}, Diff{Text: text.String(), Truncated: truncated}), nil
}

func combineDiff(first, second Diff) Diff {
	if first.Binary || second.Binary {
		return Diff{Binary: true, Truncated: first.Truncated || second.Truncated}
	}
	text := first.Text + second.Text
	truncated := first.Truncated || second.Truncated || len(text) > 1<<20
	return Diff{Text: text[:min(len(text), 1<<20)], Truncated: truncated}
}
