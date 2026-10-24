package opencode

import "testing"

func TestControlSessionRulesRebaseWithoutBroadeningWorker(t *testing.T) {
	rules := []Rule{
		{Permission: "edit", Pattern: "*", Action: "deny"},
		{Permission: "edit", Pattern: "internal/ui/**", Action: "allow"},
		{Permission: "external_directory", Pattern: "/private/state/*", Action: "deny"},
	}
	got, err := ControlSessionRules("/private/repo", rules)
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Pattern != "private/repo/internal/ui/**" || got[1].Action != "allow" {
		t.Fatal(got)
	}
	if got[len(got)-1] != rules[2] {
		t.Fatal("private guard reordered", got)
	}
	if rules[1].Pattern != "internal/ui/**" {
		t.Fatal("input mutated")
	}
	for _, rule := range got {
		if rule.Permission == "edit" && rule.Action == "allow" && rule.Pattern != "private/repo/internal/ui/**" {
			t.Fatal("worker allow broadened", rule)
		}
	}
}

func TestControlSessionExternalDenyPrecedence(t *testing.T) {
	rules := []Rule{
		{Permission: "external_directory", Pattern: "*", Action: "ask"},
		{Permission: "external_directory", Pattern: "*", Action: "deny"},
		{Permission: "edit", Pattern: "*", Action: "deny"},
		{Permission: "edit", Pattern: "owned/*", Action: "allow"},
		{Permission: "external_directory", Pattern: "/repo/private/*", Action: "deny"},
	}
	got, err := ControlSessionRules("/repo", rules)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != rules[0] || got[1] != rules[1] || got[len(got)-1] != rules[4] {
		t.Fatal("base policy or private deny order lost", got)
	}
	if got[len(got)-2].Action != "allow" || got[len(got)-2].Pattern != "/repo/*" {
		t.Fatal("repo allowance missing before private guard", got)
	}
}

func TestControlSessionMasterRestrictedToCheckout(t *testing.T) {
	got, err := ControlSessionRules("/repo", []Rule{
		{Permission: "edit", Pattern: "*", Action: "allow"},
		{Permission: "edit", Pattern: "owned/**", Action: "deny"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Action != "deny" || got[1].Pattern != "repo/*" || got[2].Pattern != "repo/owned/**" {
		t.Fatal(got)
	}
}

func TestControlRulesKeepReleaseChangelogPattern(t *testing.T) {
	rules, err := ControlSessionRules("/repo", []Rule{
		{Permission: "edit", Pattern: ReleaseChangelogEdit, Action: "allow"},
		{Permission: "edit", Pattern: "*.go", Action: "allow"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rules[0].Pattern != ReleaseChangelogEdit || rules[1].Pattern != "repo/*.go" {
		t.Fatalf("rules=%+v", rules)
	}
}
