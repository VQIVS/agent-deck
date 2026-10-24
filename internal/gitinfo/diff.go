package gitinfo

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ChangedFile describes one root-relative porcelain status entry.
type ChangedFile struct {
	Path      string
	OldPath   string
	Status    string
	Staged    bool
	Untracked bool
}

// Diff contains bounded textual changes, or identifies binary content without exposing it.
type Diff struct {
	Text      string
	Binary    bool
	Truncated bool
}

func ChangedFiles(ctx context.Context, dir string) (root string, files []ChangedFile, err error) {
	root, err = diffRoot(ctx, dir)
	if err != nil {
		return "", nil, err
	}
	data, truncated, err := diffGit(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return root, nil, err
	}
	if truncated {
		return root, nil, fmt.Errorf("changed-file inventory exceeds 1 MiB")
	}
	files, err = parseChangedFiles(data)
	return root, files, err
}

func parseChangedFiles(data []byte) ([]ChangedFile, error) {
	entries := bytes.Split(data, []byte{0})
	files := make([]ChangedFile, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) == 0 {
			continue
		}
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, fmt.Errorf("invalid Git porcelain status entry")
		}
		status := string(entry[:2])
		file := ChangedFile{
			Path: string(entry[3:]), Status: status,
			Staged: entry[0] != ' ' && entry[0] != '?', Untracked: status == "??",
		}
		if strings.ContainsAny(status, "RC") {
			i++
			if i >= len(entries) || len(entries[i]) == 0 {
				return nil, fmt.Errorf("git rename status missing original path")
			}
			file.OldPath = string(entries[i])
		}
		files = append(files, file)
	}
	return files, nil
}

func diffRoot(ctx context.Context, dir string) (string, error) {
	data, _, err := diffGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve diff repository: %w", err)
	}
	root := strings.TrimSuffix(string(data), "\n")
	if root == "" {
		return "", fmt.Errorf("git repository root is empty")
	}
	return root, nil
}

type diffBuffer struct {
	bytes.Buffer
	truncated bool
}

func (b *diffBuffer) Write(data []byte) (int, error) {
	length := len(data)
	remaining := (1 << 20) - b.Len()
	if length > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(data)
	return length, nil
}

func diffGit(ctx context.Context, dir string, args ...string) (data []byte, truncated bool, err error) {
	base := make([]string, 0, 12+len(args))
	base = append(base,
		"--no-optional-locks", "--literal-pathspecs", "-C", dir,
		"-c", "core.fsmonitor=false", "-c", "core.quotePath=false", "-c", "core.pager=cat",
	)
	command := exec.CommandContext(ctx, "git", append(base, args...)...)
	command.Env = append(Environment(), "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat")
	var output diffBuffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return nil, false, fmt.Errorf("read Git %s: %w", args[0], err)
	}
	return output.Bytes(), output.truncated, nil
}
