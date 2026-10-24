package mcp

import (
	"context"
	"fmt"
	"slices"

	"github.com/qivs/agent-deck/internal/channel"
)

func memberID(member *Member) string {
	return member.ID
}

func recordVisible(record *channel.Record, name, id string) bool {
	return !record.Private || (record.Sender == name && record.SenderID == id) ||
		(record.Audience[name] == id && slices.Contains(record.Recipients, name))
}

func (s *Service) bindAudience(record *channel.Record) {
	record.Audience = make(map[string]string)
	for _, member := range s.options.Members() {
		if member.Name == record.Sender {
			record.SenderID = memberID(&member)
		}
		if slices.Contains(record.Recipients, member.Name) {
			record.Audience[member.Name] = memberID(&member)
		}
	}
}

func settlementIndex(records []channel.Record) map[string]bool {
	result := make(map[string]bool)
	for i := range records {
		r := &records[i]
		if r.Kind == "ack" || r.Kind == "delivered" {
			result[r.Message+"\x00"+r.SenderID] = true
		}
	}
	return result
}

func (s *Service) inheritHops(ctx context.Context, member string, record *channel.Record) error {
	records, err := s.options.Journal.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read delivery hop state: %w", err)
	}
	delivered := make(map[string]bool)
	current, memberErr := s.member(Identity{Team: s.options.Team, Member: member})
	if memberErr != nil {
		return memberErr
	}
	for i := range records {
		r := &records[i]
		if r.Sender == member && r.SenderID == current.ID &&
			(r.Kind == "delivered" || r.Kind == "ack") {
			delivered[r.Message] = true
		}
	}
	for i := range records {
		r := &records[i]
		if delivered[r.ID] && r.Kind == "message" && r.Hops >= record.Hops {
			record.Hops, record.Thread = r.Hops+1, r.Thread
		}
	}
	return nil
}
