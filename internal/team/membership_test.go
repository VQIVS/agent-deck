package team

import (
	"regexp"
	"testing"
)

func TestMembershipIdentityLifetime(t *testing.T) {
	members := []Member{{Name: "worker"}}
	if err := EnsureMembershipIDs(members, nil); err != nil {
		t.Fatal(err)
	}
	id := members[0].ID
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatal("invalid UUID", id)
	}
	previous := append([]Member(nil), members...)
	members[0].Session = "ses_started"
	if err := EnsureMembershipIDs(members, previous); err != nil || members[0].ID != id {
		t.Fatal("starting session changed membership ID", err)
	}
	members[0].Name = "renamed"
	if err := EnsureMembershipIDs(members, previous); err != nil || members[0].ID != id {
		t.Fatal("rename changed membership ID", err)
	}
	members[0].Name = "worker"
	readded := []Member{{Name: "worker"}}
	if err := EnsureMembershipIDs(readded, nil); err != nil || readded[0].ID == id {
		t.Fatal("same-name replacement reused old membership ID", err)
	}
	members[0].ID = "forged"
	if err := EnsureMembershipIDs(members, previous); err == nil {
		t.Fatal("membership ID replaced")
	}
}

func TestMembershipSameSaveReplacementAndLegacyMigration(t *testing.T) {
	previous := []Member{{Name: "worker"}}
	if err := EnsureMembershipIDs(previous, nil); err != nil {
		t.Fatal(err)
	}
	replacement := []Member{{Name: "worker"}}
	if err := EnsureMembershipIDs(replacement, previous); err != nil || replacement[0].ID == previous[0].ID {
		t.Fatal("same-save replacement inherited removed identity", err)
	}
	legacy := []Member{{Name: "legacy"}}
	migrated := []Member{{Name: "legacy", Session: "ses_existing"}}
	if err := EnsureMembershipIDs(migrated, legacy); err != nil || migrated[0].ID == "" {
		t.Fatal("legacy membership did not receive new identity", err)
	}
	duplicate := []Member{previous[0], previous[0]}
	duplicate[1].Name = "renamed-copy"
	if err := EnsureMembershipIDs(duplicate, previous); err == nil {
		t.Fatal("duplicate existing membership ID accepted")
	}
}
