package server

import "testing"

func TestStateRootRejectsRelativeAndMissingHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative")
	if _, err := StateRoot(); err == nil {
		t.Fatal("relative state accepted")
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := StateRoot(); err == nil {
		t.Fatal("missing home accepted")
	}
}
