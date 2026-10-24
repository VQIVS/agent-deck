package team

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vqivs/agent-deck/internal/gitinfo"
)

func git(ctx context.Context, dir string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	c.Env = gitinfo.Environment()
	b, e := c.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(b)), e)
	}
	return strings.TrimSpace(string(b)), nil
}

func Workspace(t *Team, m *Member) (directory, branchName string, err error) {
	branch := m.BranchTemplate
	if branch == "" {
		branch = "{team}/{member}"
	}
	branch = strings.ReplaceAll(strings.ReplaceAll(branch, "{team}", Slug(t.Name)), "{member}", Slug(m.Name))
	if strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") || strings.ContainsAny(branch, " ~^:?*[\\") ||
		strings.Contains(branch, "@{") ||
		strings.HasSuffix(branch, "/") {
		return "", "", fmt.Errorf("invalid branch template")
	}
	dir := filepath.Join(filepath.Dir(t.Repo), filepath.Base(t.Repo)+"-"+Slug(t.Name)+"-"+Slug(m.Name))
	return dir, branch, nil
}

func CreateWorktree(ctx context.Context, t *Team, m *Member) (directory, branchName string, err error) {
	if strings.HasPrefix(t.Base, "-") {
		return "", "", fmt.Errorf("base must not start with '-' ")
	}
	dir, branch, e := Workspace(t, m)
	if e != nil {
		return "", "", e
	}
	_, e = git(ctx, t.Repo, "worktree", "add", "-b", branch, dir, t.Base)
	return dir, branch, e
}

func RemoveWorktree(ctx context.Context, t *Team, m *Member, force bool) error {
	if !m.Owned || m.Mode != "worktree" {
		return nil
	}
	if !force {
		if _, e := git(ctx, t.Repo, "merge-base", "--is-ancestor", m.Branch, t.Base); e != nil {
			return fmt.Errorf("branch not merged into base; select delete worktree anyway")
		}
		if dirty, e := git(ctx, m.Dir, "status", "--porcelain"); e != nil || dirty != "" {
			return fmt.Errorf("worktree dirty or unavailable; select delete worktree anyway")
		}
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, m.Dir)
	_, e := git(ctx, t.Repo, args...)
	return e
}
