package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowRootIgnoresWorkingDirectory(t *testing.T) {
	r := &Runtime{options: RuntimeOptions{StateDir: t.TempDir()}}
	first := r.workflowRoot("agent-deck")
	t.Chdir(t.TempDir())
	if second := r.workflowRoot("agent-deck"); second != first {
		t.Fatalf("root changed with cwd: %s vs %s", first, second)
	}
}

func TestValidateControlAcceptsLegacyRootsInsideState(t *testing.T) {
	state := t.TempDir()
	r := &Runtime{options: RuntimeOptions{StateDir: state}}
	legacy := filepath.Join(r.stateRoot(), strings.Repeat("ab", 32)+".controls")
	control := filepath.Join(legacy, strings.Repeat("cd", 16), "member-1")
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacy, 0o711); err != nil {
		t.Fatal(err)
	}
	if err := r.validateControl("member-1", control); err != nil {
		t.Fatalf("legacy control rejected: %v", err)
	}
	if err := r.validateControl("member-2", control); err == nil {
		t.Fatal("control accepted for another member")
	}
	outside := filepath.Join(t.TempDir(), strings.Repeat("ab", 32)+".controls", strings.Repeat("cd", 16), "member-1")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.validateControl("member-1", outside); err == nil {
		t.Fatal("control outside the state directory accepted")
	}
}
