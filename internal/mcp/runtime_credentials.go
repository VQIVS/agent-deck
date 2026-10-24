package mcp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vqivs/agent-deck/internal/gitinfo"
	"github.com/vqivs/agent-deck/internal/team"
)

type runtimeCredential struct {
	memberID string
	identity Identity
	dir      string
	session  string
	role     string
	config   string
	entry    string
	token    string
	lockPath string
}

// InstallCredential explicitly installs a private directory credential for one isolated worktree.
// Directory credentials are not a sandbox against peers able to read each other's files.
func (r *Runtime) InstallCredential(ctx context.Context, name string) error {
	r.credentialMu.Lock()
	defer r.credentialMu.Unlock()
	manager, err := r.current(ctx)
	if err != nil {
		return err
	}
	if manager.ReadOnly {
		return fmt.Errorf("read-only: credential installation disabled")
	}
	index, err := manager.MemberIndex(name)
	if err != nil {
		return fmt.Errorf("find credential member: %w", err)
	}
	m := &manager.Team.Members[index]
	if err := r.credentialDirectory(ctx, manager, m); err != nil {
		return err
	}
	oldName, old, exists := r.findCredential(m.ID, name)
	if exists {
		r.Tokens.Revoke(old.identity)
		delete(r.credentials, oldName)
		if err := removeRuntimeConfig(&old); err != nil {
			return err
		}
	}
	identity := Identity{Team: manager.Team.Name, Member: name}
	token, err := r.Tokens.Issue(identity, true)
	if err != nil {
		return err
	}
	credential := runtimeCredential{
		memberID: m.ID,
		identity: identity, dir: m.SessionDir(), session: m.Session, role: m.Role,
		config: filepath.Join(m.SessionDir(), "opencode.json"),
		entry:  "agent_deck_" + team.Slug(manager.Team.Name), token: token,
	}
	credential.lockPath = runtimeStatePath(r.options.StateDir, credential.config, "config.lock")
	if err := installRuntimeConfig(&credential, r.URL); err != nil {
		r.Tokens.Revoke(identity)
		return err
	}
	r.credentials[name] = credential
	return nil
}

func isolatedMember(ctx context.Context, manager *team.Manager, member *team.Member) error {
	if member.Mode != "worktree" || member.Dir == "" || !filepath.IsAbs(member.Dir) {
		return fmt.Errorf("credentials require an isolated worktree; shared/adopt members are unsupported")
	}
	if member.Session == "" {
		return fmt.Errorf("credentials require a current member session")
	}
	if member.Muted {
		return fmt.Errorf("muted member cannot receive credentials")
	}
	dir, err := filepath.EvalSymlinks(member.Dir)
	if err != nil {
		return fmt.Errorf("resolve credential directory: %w", err)
	}
	repo, err := filepath.EvalSymlinks(manager.Team.Repo)
	if err != nil || dir == repo {
		return fmt.Errorf("credential worktree must differ from the team checkout")
	}
	if err := distinctRuntimeDirectories(manager, member, dir); err != nil {
		return err
	}
	return verifyRuntimeWorktree(ctx, dir, repo)
}

func distinctRuntimeDirectories(manager *team.Manager, member *team.Member, dir string) error {
	for i := range manager.Team.Members {
		other := &manager.Team.Members[i]
		if other.Name == member.Name || other.Dir == "" {
			continue
		}
		otherDir, err := filepath.EvalSymlinks(other.Dir)
		if err != nil {
			return fmt.Errorf("resolve peer credential directory: %w", err)
		}
		if dir == otherDir || strings.HasPrefix(dir, otherDir+string(filepath.Separator)) ||
			strings.HasPrefix(otherDir, dir+string(filepath.Separator)) {
			return fmt.Errorf("credential directory overlaps another member directory")
		}
	}
	return nil
}

func verifyRuntimeWorktree(ctx context.Context, dir, repo string) error {
	top, err := runtimeGit(ctx, dir, "rev-parse", "--show-toplevel").Output()
	if err != nil || strings.TrimSpace(string(top)) != dir {
		return fmt.Errorf("credential directory must be a git worktree root")
	}
	common, err := runtimeGit(ctx, dir,
		"rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return fmt.Errorf("inspect credential worktree: %w", err)
	}
	repoCommon, err := runtimeGit(ctx, repo,
		"rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil || strings.TrimSpace(string(common)) != strings.TrimSpace(string(repoCommon)) {
		return fmt.Errorf("credential worktree must belong to the team repository")
	}
	if err := runtimeGit(ctx, dir, "check-ignore", "-q", "--", "opencode.json").Run(); err != nil {
		return fmt.Errorf("credential config must be ignored by git before installation")
	}
	return nil
}

func runtimeGit(ctx context.Context, dir string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	command.Env = append(gitinfo.Environment(), "GIT_OPTIONAL_LOCKS=0")
	return command
}

func (r *Runtime) RevokeCredential(ctx context.Context, name string) error {
	r.credentialMu.Lock()
	defer r.credentialMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	manager, err := r.current(ctx)
	if err != nil {
		return err
	}
	if manager.ReadOnly {
		return fmt.Errorf("read-only: credential revocation disabled")
	}
	index, indexErr := manager.MemberIndex(name)
	id := ""
	if indexErr == nil {
		id = manager.Team.Members[index].ID
	}
	key, credential, exists := r.findCredential(id, name)
	if !exists {
		return nil
	}
	r.Tokens.Revoke(credential.identity)
	delete(r.credentials, key)
	return removeRuntimeConfig(&credential)
}

func (r *Runtime) revokeChanged(manager *team.Manager) error {
	r.credentialMu.Lock()
	defer r.credentialMu.Unlock()
	var failures []error
	for name := range r.credentials {
		credential := r.credentials[name]
		member := credentialMember(manager, &credential, name)
		if member != nil {
			if (member.Mode == "worktree" || member.ControlDir != "") && member.SessionDir() == credential.dir &&
				member.Session == credential.session && member.Role == credential.role &&
				member.Name == credential.identity.Member && !manager.ReadOnly {
				continue
			}
		}
		r.Tokens.Revoke(credential.identity)
		delete(r.credentials, name)
		if err := removeRuntimeConfig(&credential); err != nil {
			failures = append(failures, err)
		} else if member == nil || member.ControlDir != credential.dir {
			// Only a removed member or a moved control directory loses its directory; a rename or
			// rebind keeps it, because the member's session still lives there.
			r.removeEmptyWorkflowControl(&credential)
		}
	}
	return errors.Join(failures...)
}

func (r *Runtime) findCredential(id, name string) (string, runtimeCredential, bool) {
	for key := range r.credentials {
		credential := r.credentials[key]
		if (id != "" && credential.memberID == id) || (credential.memberID == "" && key == name) {
			return key, credential, true
		}
	}
	return "", runtimeCredential{}, false
}

func credentialMember(manager *team.Manager, credential *runtimeCredential, name string) *team.Member {
	for i := range manager.Team.Members {
		member := &manager.Team.Members[i]
		if (credential.memberID != "" && member.ID == credential.memberID) ||
			(credential.memberID == "" && member.Name == name) {
			return member
		}
	}
	return nil
}
