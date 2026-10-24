package opencode

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ReleaseChangelogEdit lets a release worker edit the changelog inside its temporary release
// worktrees; it is root-independent and therefore never rebased onto the checkout.
const ReleaseChangelogEdit = "*agent-deck-release-*/CHANGELOG.md"

// ControlSessionRules rebases checkout-relative edit rules for a non-Git control session rooted at /.
// Absolute external-directory/private-root rules retain their order and meaning.
func ControlSessionRules(repo string, rules []Rule) ([]Rule, error) {
	if !filepath.IsAbs(repo) || filepath.Clean(repo) != repo || repo == string(filepath.Separator) {
		return nil, fmt.Errorf("control rules require a clean absolute checkout root")
	}
	result := make([]Rule, 0, len(rules)+3)
	var externalDenies []Rule
	for _, rule := range rules {
		if controlPrivateDeny(&rule) {
			externalDenies = append(externalDenies, rule)
			continue
		}
		if rule.Permission == "edit" && rule.Pattern != "*" && rule.Pattern != ReleaseChangelogEdit &&
			!filepath.IsAbs(rule.Pattern) {
			if strings.HasPrefix(rule.Pattern, "../") || strings.Contains(rule.Pattern, "/../") {
				return nil, fmt.Errorf("control edit pattern escapes checkout")
			}
			rule.Pattern = strings.TrimPrefix(filepath.ToSlash(filepath.Join(repo, rule.Pattern)), "/")
		}
		// A master's broad allow is restricted to the checkout, never the control/private tree.
		if rule.Permission == "edit" && rule.Pattern == "*" && rule.Action != "deny" {
			result = append(result, Rule{Permission: "edit", Pattern: "*", Action: "deny"})
			rule.Pattern = strings.TrimPrefix(filepath.ToSlash(repo), "/") + "/*"
		}
		result = append(result, rule)
	}
	result = append(result, Rule{Permission: "external_directory", Pattern: repo, Action: "allow"},
		Rule{Permission: "external_directory", Pattern: repo + "/*", Action: "allow"})
	result = append(result, externalDenies...)
	return result, nil
}

func controlPrivateDeny(rule *Rule) bool {
	return rule.Permission == "external_directory" && rule.Action == "deny" && filepath.IsAbs(rule.Pattern)
}
