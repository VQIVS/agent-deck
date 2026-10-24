package team

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	api "github.com/qivs/agent-deck/internal/opencode"
	"github.com/qivs/agent-deck/internal/server"
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
	rules := append([]api.Rule{{Permission: "*", Pattern: "*", Action: "ask"}}, baseLeaseRules(member)...)
	for _, pattern := range []string{"agent-deck*", "*/agent-deck*", "* agent-deck*"} {
		rules = append(rules, api.Rule{Permission: "bash", Pattern: pattern, Action: "deny"})
	}
	if member.Role == "worker" {
		rules = append(rules, api.Rule{Permission: "edit", Pattern: "*", Action: "deny"})
		action := member.Permissions["edit"]
		if action == "" {
			action = "ask"
		}
		rules = appendEditLeases(rules, member.Leases, action)
		rules = append(rules, workerGitRules()...)
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
	rules := make([]api.Rule, 0, len(keys))
	for _, permission := range keys {
		rules = append(rules, api.Rule{Permission: permission, Pattern: "*", Action: member.Permissions[permission]})
	}
	return rules
}

func appendEditLeases(rules []api.Rule, leases []string, action string) []api.Rule {
	for _, lease := range leases {
		rules = append(rules, api.Rule{Permission: "edit", Pattern: lease, Action: action})
	}
	return rules
}

func workerGitRules() []api.Rule {
	commands := strings.Fields("add commit push reset checkout switch stash rebase merge restore clean " +
		"am apply cherry-pick revert rm mv branch tag update-ref symbolic-ref worktree init clone config gc")
	rules := make([]api.Rule, 0, len(commands)*5)
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
