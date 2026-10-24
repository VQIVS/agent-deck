package team

import "testing"

func TestAllowlistExactCommands(t *testing.T) {
	a, err := NewCommandAllowlist([]string{"go test ./internal/team/...", "git status --short", "make check"})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"go test ./internal/team/...", "git  status '--short'", "make\tcheck"} {
		if !a.Allows(command) {
			t.Errorf("rejected exact argv: %q", command)
		}
	}
	for _, command := range []string{
		"go test ./...", "go test ./internal/team/... -exec bad", "git status --short --help", "make check install",
		"go test ./internal/team/...; rm -rf .", "make check && evil", "make check | evil", "make check > file",
		"make $(evil)", "make `evil`", "make ${target}", "make check\nevil", "make check &", "make <(evil)",
		"make {check,install}", "make *", "make check # comment", "A=1 make check", "/bin/make check",
		"env make check", "sh -c 'make check'", "make 'check", "make check\\", "make \"check\x01\"",
	} {
		if a.Allows(command) {
			t.Errorf("allowed unsafe or different command: %q", command)
		}
	}
	if !a.AllowsRequest("bash", []string{"make check", "git status --short"}, false) {
		t.Fatal("complete allowed request rejected")
	}
	if a.AllowsRequest("external_directory", []string{"make check"}, false) ||
		a.AllowsRequest("bash", nil, false) || a.AllowsRequest("bash", []string{"make check"}, true) ||
		a.AllowsRequest("bash", []string{"make check", "evil"}, false) {
		t.Fatal("request guard bypassed")
	}
}

func TestAllowlistInvalidConfiguration(t *testing.T) {
	for _, command := range []string{"", " ", "''", "go test *", "make check; evil", "A=1 make check"} {
		if _, err := NewCommandAllowlist([]string{command}); err == nil {
			t.Errorf("invalid configuration accepted: %q", command)
		}
	}
	if (CommandAllowlist{}).Allows("make check") {
		t.Fatal("empty allowlist allowed a command")
	}
}
