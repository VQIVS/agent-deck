package team

import (
	"fmt"
	"path"
	"strings"
)

// Leases lists repository-relative OpenCode wildcard patterns owned by one member.
type Leases []string

func (l Leases) Validate() error {
	for _, pattern := range l {
		if err := validateLease(pattern); err != nil {
			return err
		}
	}
	return nil
}

func validateLease(pattern string) error {
	if pattern == "" || strings.TrimSpace(pattern) != pattern || strings.ContainsAny(pattern, "\\\x00\r\n\t[]{}") {
		return fmt.Errorf("invalid lease %q: use a relative path with * or ? wildcards", pattern)
	}
	if path.IsAbs(pattern) || path.Clean(pattern) != pattern || strings.HasSuffix(pattern, "/") {
		return fmt.Errorf("invalid lease %q: use a clean repository-relative path", pattern)
	}
	for _, segment := range strings.Split(pattern, "/") {
		if segment == "." || segment == ".." || segment == ".git" || strings.Contains(segment, ":") {
			return fmt.Errorf("invalid lease %q: parent paths and git metadata are not allowed", pattern)
		}
	}
	return nil
}

// ValidateLeases rejects potentially overlapping ownership across different members.
func ValidateLeases(members []Member) error {
	for i := range members {
		member := &members[i]
		if err := Leases(member.Leases).Validate(); err != nil {
			return fmt.Errorf("member %q: %w", member.Name, err)
		}
		for j := range members[:i] {
			other := &members[j]
			if err := overlappingLeases(member, other); err != nil {
				return err
			}
		}
	}
	return nil
}

func overlappingLeases(member, other *Member) error {
	for _, left := range member.Leases {
		for _, right := range other.Leases {
			a, b := leasePrefix(left), leasePrefix(right)
			if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
				return fmt.Errorf("lease %q of %q overlaps lease %q of %q", left, member.Name, right, other.Name)
			}
		}
	}
	return nil
}

func leasePrefix(pattern string) string {
	if index := strings.IndexAny(pattern, "*?"); index >= 0 {
		return pattern[:index]
	}
	return pattern
}

// LeasePrompt describes every member's ownership and the shared-checkout rules.
func LeasePrompt(members []Member) string {
	var text strings.Builder
	text.WriteString("Shared-checkout leases (repository-relative; * also crosses /):\n")
	for i := range members {
		member := &members[i]
		leases := strings.Join(member.Leases, ", ")
		if leases == "" {
			leases = "none"
		}
		fmt.Fprintf(&text, "%s %s: %s\n", member.Role, member.Name, leases)
	}
	text.WriteString("Workers edit only their leases and never run git write commands. " +
		"Only the master commits. The master may edit paths not leased to another member. " +
		"Ask the master before editing another member's paths. Run package-scoped tests; " +
		"the master runs full checks when workers are idle.")
	return text.String()
}
