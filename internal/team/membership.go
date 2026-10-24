package team

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// EnsureMembershipIDs preserves saved membership IDs and generates UUIDs for new members.
// Previous must be the last persisted private roster, never caller-supplied identity claims.
func EnsureMembershipIDs(members, previous []Member) error {
	ids := make(map[string]bool, len(previous))
	for i := range previous {
		ids[previous[i].ID] = previous[i].ID != ""
	}
	seen := make(map[string]bool, len(members))
	for i := range members {
		member := &members[i]
		if member.ID != "" && !ids[member.ID] {
			return fmt.Errorf("new member %q cannot supply its own membership ID", member.Name)
		}
		if member.ID == "" {
			generated, err := membershipUUID()
			if err != nil {
				return err
			}
			member.ID = generated
		}
		if seen[member.ID] {
			return fmt.Errorf("duplicate membership ID")
		}
		seen[member.ID] = true
	}
	return nil
}

func membershipUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate membership UUID: %w", err)
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	id := hex.EncodeToString(bytes[:])
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:], nil
}
