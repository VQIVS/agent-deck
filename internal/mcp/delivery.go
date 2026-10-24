package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/qivs/agent-deck/internal/channel"
)

// Transport checks live state and submits prompts without aborting busy sessions.
// Send must preserve the member's agent/model options; callbacks must not reenter Service.
type Transport interface {
	Busy(context.Context, string) (bool, error)
	Send(context.Context, string, string) error
}

func deliveryMessage(record *channel.Record) bool {
	return record.Kind == "message" || record.Kind == "roster"
}

func settled(records []channel.Record, message, member string) bool {
	for i := range records {
		r := &records[i]
		if r.Message == message && r.Sender == member && (r.Kind == "ack" || r.Kind == "delivered") {
			return true
		}
	}
	return false
}

func (s *Service) acknowledge(ctx context.Context, member string, visible []channel.Record) error {
	records, err := s.options.Journal.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read channel acknowledgment state: %w", err)
	}
	for i := range visible {
		r := &visible[i]
		if !deliveryMessage(r) || !slices.Contains(r.Recipients, member) || settled(records, r.ID, member) {
			continue
		}
		if err := s.event(ctx, "ack", member, r); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) event(ctx context.Context, kind, member string, message *channel.Record) error {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("generate channel event id: %w", err)
	}
	r := channel.Record{
		ID: hex.EncodeToString(b), Kind: kind, Thread: message.Thread, Sender: member,
		Recipients: []string{message.Sender}, Private: true, Message: message.ID,
		Hops: message.Hops, Time: s.options.Now().UTC(),
	}
	if err := s.options.Journal.Append(ctx, &r); err != nil {
		return fmt.Errorf("persist channel %s: %w", kind, err)
	}
	return nil
}

// Deliver serializes sends and reads, preserving queued intent across process restarts.
// A crash after Send but before the delivered event can resend the same message IDs.
func (s *Service) Deliver(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.options.ReadOnly != nil && s.options.ReadOnly() {
		return nil
	}
	if s.options.Transport == nil {
		return fmt.Errorf("channel delivery transport is not configured")
	}
	records, err := s.options.Journal.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read channel delivery queue: %w", err)
	}
	for _, member := range s.options.Members() {
		if member.Muted {
			continue
		}
		pending := s.pending(records, member.Name)
		if len(pending) == 0 {
			continue
		}
		if err := s.deliverMember(ctx, member.Name, pending); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) pending(records []channel.Record, member string) []channel.Record {
	var pending []channel.Record
	bytes := 0
	for i := range records {
		r := &records[i]
		if !deliveryMessage(r) || !slices.Contains(r.Recipients, member) || settled(records, r.ID, member) {
			continue
		}
		if r.Kind == "message" {
			sender, err := s.member(Identity{Team: s.options.Team, Member: r.Sender})
			if err != nil || sender.Muted || (r.Hops > s.options.MaxHops && sender.Role != "master") {
				continue
			}
		}
		pending = append(pending, *r)
		bytes += len(r.Text)
		if len(pending) >= 16 || bytes >= s.options.MaxBytes*4 {
			break
		}
	}
	return pending
}

func (s *Service) deliverMember(ctx context.Context, member string, messages []channel.Record) error {
	busy, err := s.options.Transport.Busy(ctx, member)
	if err != nil {
		return fmt.Errorf("check channel recipient %q: %w", member, err)
	}
	if busy {
		return nil
	}
	instructions, err := s.options.Instructions(ctx, member)
	if err != nil {
		return fmt.Errorf("build channel recipient instructions %q: %w", member, err)
	}
	var prompt strings.Builder
	prompt.WriteString(instructions)
	prompt.WriteString("\nTeam channel delivery. Deduplicate by message id; retries may repeat IDs.\n")
	for i := range messages {
		b, err := json.Marshal(&messages[i])
		if err != nil {
			return fmt.Errorf("encode channel delivery: %w", err)
		}
		prompt.Write(b)
		prompt.WriteByte('\n')
		if err := s.event(ctx, "attempt", member, &messages[i]); err != nil {
			return err
		}
	}
	if err := s.options.Transport.Send(ctx, member, prompt.String()); err != nil {
		return fmt.Errorf("deliver channel messages to %q: %w", member, err)
	}
	for i := range messages {
		if err := s.event(ctx, "delivered", member, &messages[i]); err != nil {
			return err
		}
	}
	return nil
}

// BroadcastRoster is a privileged integration path; MCP callers cannot invoke it.
func (s *Service) BroadcastRoster(ctx context.Context, text string) (channel.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.options.ReadOnly != nil && s.options.ReadOnly() {
		return channel.Record{}, fmt.Errorf("channel posting is disabled")
	}
	if strings.TrimSpace(text) == "" || len(text) > s.options.MaxBytes {
		return channel.Record{}, fmt.Errorf("roster must contain text and fit %d bytes", s.options.MaxBytes)
	}
	now := s.options.Now()
	count, err := s.recent(ctx, "", now)
	if err != nil {
		return channel.Record{}, err
	}
	if count >= s.options.Rate {
		return channel.Record{}, fmt.Errorf("roster rate limit reached")
	}
	post := Post{Text: text}
	for _, member := range s.options.Members() {
		post.Mentions = append(post.Mentions, member.Name)
	}
	r, err := s.message(ctx, Identity{Team: s.options.Team, Member: "agent-deck"}, post, post.Mentions, false)
	if err != nil {
		return channel.Record{}, err
	}
	r.Kind, r.Time, r.Hops = "roster", now.UTC(), 0
	if err := s.options.Journal.Append(ctx, &r); err != nil {
		return channel.Record{}, fmt.Errorf("broadcast roster: %w", err)
	}
	return r, nil
}
