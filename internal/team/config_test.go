package team

import (
	"strings"
	"testing"
)

func TestDeprecatedAutomationKeysAreIgnoredWithoutRelaxingValidation(t *testing.T) {
	legacy := strings.Replace(adoptedTeam, "members:",
		"approval: retired-mode\nsafe_commands: ['anything']\nmembers:", 1)
	legacy = strings.Replace(legacy, "capability: inherited}", "capability: inherited, "+
		"nudge: {enabled: true, unknown: arbitrary}, nudge_status: {count: 99}, stall_after: obsolete-value}", 1)
	loaded, err := Parse([]byte(legacy))
	if err != nil {
		t.Fatal("deprecated automation configuration rejected", err, legacy)
	}
	if loaded.Name != "Example" || len(loaded.Members) != 2 {
		t.Fatal("current configuration lost", loaded)
	}
	if _, err := Parse([]byte(legacy + "unknown_current_field: true\n")); err == nil {
		t.Fatal("unknown current key accepted")
	}
	if _, err := Parse([]byte(legacy + "approval: duplicate\n")); err == nil {
		t.Fatal("duplicate deprecated key accepted")
	}
	loaded.Base = "-unsafe"
	if err := loaded.Validate(); err == nil {
		t.Fatal("base option guard lost")
	}
}
