package team

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/server"
)

// LeaseRules builds ordered session rules; the repository must be the Git worktree root.
func LeaseRules(repo string, members []Member, member *Member) ([]api.Rule, error) {
	if !filepath.IsAbs(repo) {
		return nil, fmt.Errorf("lease rules need an absolute repository root")
	}
	if err := ValidateLeases(members); err != nil {
		return nil, err
	}
	if err := Leases(member.Leases).Validate(); err != nil {
		return nil, err
	}
	if member.Role != "master" && member.Role != "worker" {
		return nil, fmt.Errorf("lease rules need master or worker role")
	}
	rules := append([]api.Rule{
		{Permission: "*", Pattern: "*", Action: "ask"},
		{Permission: "agent_deck_*", Pattern: "*", Action: "allow"},
	}, baseLeaseRules(member)...)
	rules = append(rules, selfInvocationRules()...)
	if member.Role == "worker" {
		rules = append(rules, api.Rule{Permission: "edit", Pattern: "*", Action: "deny"})
		action := member.Permissions["edit"]
		if action == "" {
			action = "ask"
		}
		rules = appendEditLeases(rules, member.Leases, action)
		if member.Capability == "release" {
			rules = append(rules, gitDenyRules(releaseDeniedGit)...)
			rules = append(rules, releaseWorkspaceRules()...)
		} else {
			rules = append(rules, workerGitRules()...)
		}
	} else {
		action := member.Permissions["edit"]
		if action == "" {
			action = "allow"
		}
		rules = append(rules, api.Rule{Permission: "edit", Pattern: "*", Action: action})
		for i := range members {
			other := &members[i]
			if other.Name != member.Name {
				rules = appendEditLeases(rules, other.Leases, "deny")
			}
		}
	}
	privateRules, err := workerPrivateRules()
	if err != nil {
		return nil, err
	}
	return append(rules, privateRules...), nil
}

func workerPrivateRules() ([]api.Rule, error) {
	state, err := server.StateRoot()
	if err != nil {
		return nil, fmt.Errorf("resolve worker private state policy: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve worker home policy: %w", err)
	}
	var rules []api.Rule
	for _, root := range []string{DefaultRoot(), filepath.Dir(DefaultRoot()), state} {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve worker private root: %w", err)
		}
		paths := []string{filepath.ToSlash(absolute)}
		if resolved, err := filepath.EvalSymlinks(absolute); err == nil && resolved != absolute {
			paths = append(paths, filepath.ToSlash(resolved))
		}
		if relative, err := filepath.Rel(home, absolute); err == nil && !strings.HasPrefix(relative, "..") {
			for _, prefix := range []string{"~/", "$HOME/", "${HOME}/"} {
				paths = append(paths, prefix+filepath.ToSlash(relative))
			}
		}
		for _, path := range paths {
			for _, pattern := range []string{path, path + "/*"} {
				rules = append(rules,
					api.Rule{Permission: "read", Pattern: pattern, Action: "deny"},
					api.Rule{Permission: "external_directory", Pattern: pattern, Action: "deny"})
			}
			rules = append(rules, api.Rule{Permission: "bash", Pattern: "*" + path + "*", Action: "deny"})
		}
	}
	return rules, nil
}

func baseLeaseRules(member *Member) []api.Rule {
	keys := make([]string, 0, len(member.Permissions))
	for permission := range member.Permissions {
		keys = append(keys, permission)
	}
	sort.Strings(keys)
	rules := make([]api.Rule, 0, len(keys)+5)
	for _, permission := range keys {
		rules = append(rules, api.Rule{Permission: permission, Pattern: "*", Action: member.Permissions[permission]})
	}
	// Search tools only read, so they follow read; todo tools touch nothing outside the session.
	if read, ok := member.Permissions["read"]; ok {
		for _, search := range []string{"grep", "glob", "list"} {
			rules = append(rules, api.Rule{Permission: search, Pattern: "*", Action: read})
		}
	}
	for _, todo := range []string{"todowrite", "todoread"} {
		rules = append(rules, api.Rule{Permission: todo, Pattern: "*", Action: "allow"})
	}
	return rules
}

func appendEditLeases(rules []api.Rule, leases []string, action string) []api.Rule {
	for _, lease := range leases {
		rules = append(rules, api.Rule{Permission: "edit", Pattern: lease, Action: action})
	}
	return rules
}

// releaseDeniedGit keeps a release worker away from history rewrites and from anything that
// switches or discards the shared checkout others are editing; tagging, committing its own files,
// fast-forward pushes and temporary worktrees stay allowed.
var releaseDeniedGit = append(strings.Fields("reset checkout switch stash rebase restore clean rm mv am apply "+
	"cherry-pick revert symbolic-ref init clone config gc"),
	"push* --force", "push* -f", "push* +", "branch* -D", "branch* -d", "branch* --delete", "tag* -d", "tag* --delete")

func workerGitRules() []api.Rule {
	return gitDenyRules(strings.Fields("add commit push reset checkout switch stash rebase merge restore clean " +
		"am apply cherry-pick revert rm mv branch tag update-ref symbolic-ref worktree init clone config gc"))
}

func gitDenyRules(commands []string) []api.Rule {
	rules := make([]api.Rule, 0, len(commands)*8)
	for _, command := range commands {
		for _, prefix := range []string{
			"git ", "* git ", "*/git ", "git -C * ", "git -c * ",
			"git --* ", "*git --no-pager ", "*git ",
		} {
			rules = append(rules, api.Rule{Permission: "bash", Pattern: prefix + command + "*", Action: "deny"})
		}
	}
	return rules
}

// ReleaseWorkspace is the prefix for a release worker's temporary worktrees; /private/tmp is the
// resolved form of /tmp on macOS.
const ReleaseWorkspace = "/tmp/agent-deck-release-"

func releaseWorkspaceRules() []api.Rule {
	roots := []string{ReleaseWorkspace, "/private" + ReleaseWorkspace}
	rules := make([]api.Rule, 0, len(roots)+1)
	for _, root := range roots {
		rules = append(rules, api.Rule{Permission: "external_directory", Pattern: root + "*", Action: "allow"})
	}
	// Edit patterns are relative to the session root, so match the changelog inside any release worktree.
	return append(rules, api.Rule{Permission: "edit", Pattern: api.ReleaseChangelogEdit, Action: "allow"})
}

// selfInvocationRules deny running agent-deck's control commands from a pool session. They match
// the command word followed by a control subcommand, not any path that merely contains
// "agent-deck" (the repository itself is usually named that).
func selfInvocationRules() []api.Rule {
	subcommands := []string{"team", "task", "up", "down", "supervise"}
	prefixes := []string{"agent-deck ", "*/agent-deck ", "* agent-deck ", "*;agent-deck ", "*(agent-deck "}
	rules := make([]api.Rule, 0, len(subcommands)*len(prefixes))
	for _, sub := range subcommands {
		for _, prefix := range prefixes {
			rules = append(rules, api.Rule{Permission: "bash", Pattern: prefix + sub + "*", Action: "deny"})
		}
	}
	return rules
}
