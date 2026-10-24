package team

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

// PolicyFingerprint hashes the actual ordered creation rules, including non-pool permission policy.
func (t *Team) PolicyFingerprint(member *Member) string {
	if t == nil || member == nil || member.Capability == "inherited" {
		return ""
	}
	rules := baseLeaseRules(member)
	if member.ControlDir != "" || t.LeasePool || HasLeases(t.Members) {
		var err error
		rules, err = LeaseRules(t.Repo, t.Members, member)
		if err != nil {
			return "invalid"
		}
	}
	if member.ControlDir != "" {
		var err error
		rules, err = api.ControlSessionRules(member.Dir, rules)
		if err != nil {
			return "invalid"
		}
	}
	data, err := json.Marshal(rules)
	if err != nil {
		return "invalid"
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
