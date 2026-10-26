package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vqivs/agent-deck/internal/team"
)

// WorkflowIdentity binds a private control directory to a shared code checkout.
// Callers must explicitly persist a new session binding; existing sessions are not moved.
type WorkflowIdentity struct {
	MemberID        string
	ControlDir      string
	Repo            string
	PreviousSession string
	PreviousDir     string
}

// workflowRoot hashes the team name itself; it must not depend on the process working directory,
// or a dashboard and the supervisor would disagree about every member's control path.
func (r *Runtime) workflowRoot(teamName string) string {
	return filepath.Join(r.stateRoot(), fmt.Sprintf("%x.controls", sha256.Sum256([]byte(teamName))))
}

func (r *Runtime) stateRoot() string {
	root := r.options.StateDir
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root
}

// controlRoot returns the control root of an existing control directory if it is a
// "<sha256>.controls" folder directly inside this runtime's private state directory. Older
// versions derived that hash from the process working directory, so any such root is accepted.
func (r *Runtime) controlRoot(control string) (string, error) {
	root := filepath.Dir(filepath.Dir(control))
	name := filepath.Base(root)
	hash, found := strings.CutSuffix(name, ".controls")
	if _, err := hex.DecodeString(hash); err != nil || !found || len(hash) != 64 || filepath.Dir(root) != r.stateRoot() {
		return "", fmt.Errorf("workflow control path does not match membership")
	}
	return root, nil
}

func (r *Runtime) validateControl(id, control string) error {
	root, err := r.controlRoot(control)
	if err != nil {
		return err
	}
	return validateWorkflowControl(root, id, control)
}

func (r *Runtime) credentialDirectory(ctx context.Context, manager *team.Manager, member *team.Member) error {
	if member.ControlDir == "" {
		return fmt.Errorf("member %q has no workflow identity; set it up before installing credentials", member.Name)
	}
	if member.ID == "" || member.Session == "" || member.Muted {
		return fmt.Errorf("workflow control directory does not match current membership")
	}
	return r.validateControl(member.ID, member.ControlDir)
}

func PrepareWorkflowIdentity(ctx context.Context, root, memberID, repo string) (WorkflowIdentity, error) {
	if err := ctx.Err(); err != nil {
		return WorkflowIdentity{}, err
	}
	if memberID == "" || strings.ContainsAny(memberID, "/\\") || memberID == "." || memberID == ".." {
		return WorkflowIdentity{}, fmt.Errorf("workflow identity requires a stable path-safe member ID")
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(repo) {
		return WorkflowIdentity{}, fmt.Errorf("workflow roots must be absolute")
	}
	if err := controlAncestry(root); err != nil {
		return WorkflowIdentity{}, err
	}
	return prepareWorkflowPaths(root, memberID, repo)
}

func controlAncestry(root string) error {
	for path := filepath.Clean(root); ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			resolved, resolveErr := filepath.EvalSymlinks(path)
			// macOS system aliases are canonicalized; user-controlled identity aliases are refused.
			if resolveErr != nil || (path != "/var" && path != "/tmp") || resolved == path {
				return fmt.Errorf("workflow control ancestry must not contain symlinks")
			}
		}
		if filepath.Dir(path) == path {
			return nil
		}
	}
}

func prepareWorkflowPaths(root, memberID, repo string) (WorkflowIdentity, error) {
	code, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return WorkflowIdentity{}, fmt.Errorf("resolve shared checkout: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return WorkflowIdentity{}, fmt.Errorf("create workflow identity root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return WorkflowIdentity{}, fmt.Errorf("resolve workflow identity root: %w", err)
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return WorkflowIdentity{}, fmt.Errorf("generate workflow control parent: %w", err)
	}
	parent := filepath.Join(root, hex.EncodeToString(bytes))
	control := filepath.Join(parent, memberID)
	if code == control || strings.HasPrefix(control, code+string(filepath.Separator)) {
		return WorkflowIdentity{}, fmt.Errorf("control directory must be outside the shared checkout")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return WorkflowIdentity{}, fmt.Errorf("create workflow identity root: %w", err)
	}
	if err := os.Chmod(root, 0o711); err != nil {
		return WorkflowIdentity{}, fmt.Errorf("protect workflow parent: %w", err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		return WorkflowIdentity{}, fmt.Errorf("create random workflow parent: %w", err)
	}
	if err := os.MkdirAll(control, 0o700); err != nil {
		return WorkflowIdentity{}, fmt.Errorf("create workflow control directory: %w", err)
	}
	if err := privateControlDirectory(control); err != nil {
		return WorkflowIdentity{}, err
	}
	return WorkflowIdentity{MemberID: memberID, ControlDir: control, Repo: code}, nil
}

func validateWorkflowControl(root, id, control string) error {
	if err := controlAncestry(control); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, control)
	if err != nil {
		return fmt.Errorf("resolve workflow control binding: %w", err)
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 2 || parts[1] != id || len(parts[0]) != 32 {
		return fmt.Errorf("workflow control path does not match membership")
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return fmt.Errorf("invalid workflow control parent: %w", err)
	}
	if err := privateControlDirectory(filepath.Dir(control)); err != nil {
		return err
	}
	return privateControlDirectory(control)
}

func (r *Runtime) removeEmptyWorkflowControl(credential *runtimeCredential) {
	if credential.memberID == "" {
		return
	}
	if err := r.validateControl(credential.memberID, credential.dir); err != nil {
		return
	}
	// Remove an empty owned config file only; never discard unrelated user settings.
	config, _, err := loadRuntimeConfig(credential.config)
	if err == nil {
		servers, serverErr := configServers(config)
		ownedOnly := true
		for key := range config {
			if key != "$schema" && key != "mcp" {
				ownedOnly = false
			}
		}
		if serverErr == nil && len(servers) == 0 && ownedOnly {
			_ = os.Remove(credential.config)
		}
	}
	if err := os.Remove(credential.dir); err == nil {
		_ = os.Remove(filepath.Dir(credential.dir))
	}
}

func privateControlDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect workflow control directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("workflow control directory must not be a symlink")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("protect workflow control directory: %w", err)
	}
	return nil
}

func (identity *WorkflowIdentity) Instructions() string {
	return fmt.Sprintf("Your session control directory is %q. The shared code checkout is %q. "+
		"Use absolute paths there; run shell commands with that checkout as workdir. "+
		"Do not create source files in the control directory. Previous transcript: session=%q directory=%q. "+
		"Team credentials provide cooperative same-user identity, not filesystem isolation.",
		identity.ControlDir, identity.Repo, identity.PreviousSession, identity.PreviousDir)
}
