package team

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	api "github.com/qivs/agent-deck/internal/opencode"
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
	rules := baseLeaseRules(member)
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
	rules := make([]api.Rule, 0, len(commands))
	for _, command := range commands {
		// Wildcards include global git options, shell wrappers, and absolute executable paths.
		rules = append(rules, api.Rule{Permission: "bash", Pattern: "*git*" + command + "*", Action: "deny"})
	}
	return rules
}
