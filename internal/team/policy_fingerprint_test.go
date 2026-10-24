package team

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

func TestPolicyFingerprintNoLeasesMatchesCreationRules(t *testing.T) {
	m := Member{Name: "worker", Role: "worker", Capability: "coder", Permissions: Preset("coder")}
	team := Team{Members: []Member{m}}
	rules := baseLeaseRules(&m)
	data, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(data))
	first := team.PolicyFingerprint(&m)
	if first == "" || first != want {
		t.Fatal("no-lease fingerprint does not describe actual creation rules")
	}
	m.Permissions["edit"] = "deny"
	if team.PolicyFingerprint(&m) == first {
		t.Fatal("permission changes did not invalidate fingerprint")
	}
	second := team.PolicyFingerprint(&m)
	m.Model, m.System, m.Brief = "other/model", "changed system", "changed brief"
	m.Session, m.Dir = "new-session", "/another"
	if team.PolicyFingerprint(&m) != second {
		t.Fatal("non-permission fields invalidated policy fingerprint")
	}
}

func TestPolicyFingerprintPoolAndInheritance(t *testing.T) {
	m := Member{Name: "worker", Role: "worker", Capability: "coder", Permissions: Preset("coder")}
	team := Team{Repo: t.TempDir(), LeasePool: true, Members: []Member{m}}
	pool := team.PolicyFingerprint(&m)
	if pool == "" || pool == "invalid" {
		t.Fatal("valid pool policy missing fingerprint")
	}
	team.LeasePool = false
	if team.PolicyFingerprint(&m) == pool {
		t.Fatal("pool reset/edit/git restrictions not represented")
	}
	m.Capability = "inherited"
	if team.PolicyFingerprint(&m) != "" || team.PolicyFingerprint(nil) != "" {
		t.Fatal("unverified inherited or missing policy became trusted")
	}
	m.Capability = "coder"
	team.LeasePool, team.Repo = true, "relative"
	if team.PolicyFingerprint(&m) != "invalid" {
		t.Fatal("invalid rules received usable fingerprint")
	}
}
