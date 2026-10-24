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
