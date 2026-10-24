package gitinfo

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TaskCommit is independently observed Git metadata, never task completion evidence.
type TaskCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// TaskFacts contains bounded read-only observations for a task's checkout and paths.
type TaskFacts struct {
	Branch     string       `json:"branch"`
	Base       string       `json:"base"`
	Ahead      int          `json:"ahead"`
	Commits    []TaskCommit `json:"commits"`
	Truncated  bool         `json:"truncated"`
	ObservedAt time.Time    `json:"observed_at"`
}

func ObserveTask(ctx context.Context, dir, base string, paths []string) (TaskFacts, error) {
	if !filepath.IsAbs(dir) || base == "" || strings.HasPrefix(base, "-") || strings.ContainsAny(base, "\x00\r\n") {
		return TaskFacts{}, fmt.Errorf("task git observation requires absolute checkout and explicit safe base ref")
	}
	pathspecs, err := taskPathspecs(paths)
	if err != nil {
		return TaskFacts{}, err
	}
	run := func(args ...string) (string, error) { return taskGit(ctx, dir, args...) }
	baseID, err := run("rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return TaskFacts{}, fmt.Errorf("resolve task base: %w", err)
	}
	headID, err := run("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return TaskFacts{}, fmt.Errorf("resolve task HEAD: %w", err)
	}
	branch, err := run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		branch = "detached:" + headID
	}
	count, err := run("rev-list", "--count", baseID+".."+headID, "--")
	if err != nil {
		return TaskFacts{}, fmt.Errorf("count task branch commits: %w", err)
	}
	ahead, err := strconv.Atoi(count)
	if err != nil {
		return TaskFacts{}, fmt.Errorf("parse task ahead count: %w", err)
	}
	args := make([]string, 0, 5+len(pathspecs))
	args = append(args, "log", "--format=%H%x00%s%x00", "--max-count=101", baseID+".."+headID, "--")
	args = append(args, pathspecs...)
	log, err := run(args...)
	if err != nil {
		return TaskFacts{}, fmt.Errorf("read task commits: %w", err)
	}
	commits := parseTaskCommits(log)
	verifiedHead, err := run("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || verifiedHead != headID {
		return TaskFacts{}, fmt.Errorf("task checkout changed during Git observation")
	}
	return TaskFacts{
		Branch: branch, Base: baseID, Ahead: ahead, Commits: commits[:min(len(commits), 100)],
		Truncated: len(commits) > 100, ObservedAt: time.Now().UTC(),
	}, nil
}

func taskGit(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	command.Env = append(Environment(), "GIT_OPTIONAL_LOCKS=0")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(output)), nil
}

func taskPathspecs(paths []string) ([]string, error) {
	if len(paths) == 0 || len(paths) > 100 {
		return nil, fmt.Errorf("task git paths require 1 to 100 patterns")
	}
	result := make([]string, 0, len(paths))
	for _, pattern := range paths {
		if pattern == "" || path.IsAbs(pattern) || path.Clean(pattern) != pattern ||
			strings.ContainsAny(pattern, "\\\x00\r\n:[]{}") || strings.HasPrefix(pattern, "-") {
			return nil, fmt.Errorf("invalid task git path %q", pattern)
		}
		for _, segment := range strings.Split(pattern, "/") {
			if segment == ".." || segment == ".git" {
				return nil, fmt.Errorf("task git path escapes checkout")
			}
		}
		// Default Git pathspec wildcards cross slashes, matching the lease wildcard convention.
		result = append(result, ":(top)"+pattern)
	}
	return result, nil
}

func parseTaskCommits(log string) []TaskCommit {
	parts := strings.Split(log, "\x00")
	commits := make([]TaskCommit, 0, len(parts)/2)
	for i := 0; i+1 < len(parts); i += 2 {
		hash := strings.TrimSpace(parts[i])
		if hash != "" {
			commits = append(commits, TaskCommit{Hash: hash, Subject: parts[i+1]})
		}
	}
	return commits
}
