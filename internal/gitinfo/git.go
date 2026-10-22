package gitinfo

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Info summarizes read-only worktree metadata for a dashboard footer.
type Info struct {
	Branch, Subject, Error string
	Ahead, Dirty           int
	HasBase                bool
}

func Read(ctx context.Context, dir, base string) Info {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	run := func(args ...string) (string, error) {
		c := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
		c.Env = append(Environment(), "GIT_OPTIONAL_LOCKS=0")
		b, e := c.Output()
		if len(args) > 0 && args[0] == "status" {
			return string(b), e
		}
		return strings.TrimSpace(string(b)), e
	}
	var v Info
	var err error
	if ok, e := run("rev-parse", "--is-inside-work-tree"); e != nil || ok != "true" {
		return v
	}
	v.Branch, err = run("symbolic-ref", "--short", "HEAD")
	if err != nil {
		v.Branch, _ = run("rev-parse", "--short", "HEAD")
	}
	if v.Branch == "" {
		v.Error = "git unavailable"
		return v
	}
	v.Subject, _ = run("log", "-1", "--format=%s")
	// -z avoids counting quoted filenames or rename destinations as extra files.
	status, e := run("status", "--porcelain=v1", "-z")
	if e != nil {
		v.Error = "status unavailable"
	} else {
		v.Dirty = dirtyCount(status)
	}
	base = resolveBase(base, run)
	if base != "" {
		n, e := run("rev-list", "--count", base+"..HEAD")
		if e == nil {
			v.Ahead, _ = strconv.Atoi(n)
			v.HasBase = true
		} else {
			v.Error = fmt.Sprintf("base %s unavailable", base)
		}
	}
	return v
}

func dirtyCount(status string) int {
	count := 0
	items := strings.Split(status, "\x00")
	for i := 0; i < len(items); i++ {
		if items[i] == "" {
			continue
		}
		count++
		if len(items[i]) > 1 && strings.ContainsAny(items[i][:2], "RC") {
			i++
		}
	}
	return count
}

func resolveBase(base string, run func(...string) (string, error)) string {
	if base == "" {
		base, _ = run("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	}
	if base == "" {
		base, _ = run("symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	}
	if base == "" {
		for _, candidate := range []string{"origin/main", "origin/master", "main", "master"} {
			if _, e := run("rev-parse", "--verify", candidate); e == nil {
				base = candidate
				break
			}
		}
	}
	return base
}

func (i Info) String() string {
	if i.Branch == "" {
		return i.Error
	}
	ahead := ""
	if i.HasBase {
		ahead = fmt.Sprintf(" ↑%d", i.Ahead)
	}
	return fmt.Sprintf("%s%s · %d dirty · %s %s", i.Branch, ahead, i.Dirty, i.Subject, i.Error)
}
