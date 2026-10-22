package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDoctorFailuresAndFixes(t *testing.T) {
	d := diagnostics{
		command: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("missing") },
		health:  func(context.Context) error { return errors.New("offline") },
		models:  func(context.Context) (bool, error) { return false, nil },
		write:   func(context.Context) error { return errors.New("read only") },
	}
	var output bytes.Buffer
	if !printFindings(&output, d.check(t.Context(), true)) {
		t.Fatal("expected failure")
	}
	commands := []string{"brew install opencode", "opencode serve", "opencode auth login", "brew install git"}
	for _, command := range commands {
		if !strings.Contains(output.String(), command) {
			t.Fatalf("missing fix %q: %s", command, output.String())
		}
	}
	d.command = func(context.Context, string, ...string) ([]byte, error) { return []byte("1.19.0\n"), nil }
	d.health = func(context.Context) error { return nil }
	d.models = func(context.Context) (bool, error) { return true, nil }
	d.write = func(context.Context) error { return nil }
	if printFindings(&output, d.check(t.Context(), true)) {
		t.Fatal("healthy setup failed")
	}
	d.command = func(context.Context, string, ...string) ([]byte, error) { return []byte("1.18.9"), nil }
	if !printFindings(&output, d.check(t.Context(), false)) {
		t.Fatal("old version accepted")
	}
}

func TestSupportedVersion(t *testing.T) {
	for text, want := range map[string]bool{
		"1.19.0": true, "v1.19.1\n": true, "1.18.99": false, "dev": false, "1.19": false,
		"2.broken.0": false, "1.20.-1": false,
	} {
		if got := supportedVersion(text); got != want {
			t.Errorf("supportedVersion(%q) = %t", text, got)
		}
	}
}
