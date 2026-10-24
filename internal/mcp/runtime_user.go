package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/vqivs/agent-deck/internal/channel"
)

func (r *Runtime) Endpoint() string {
	return r.URL
}

func (r *Runtime) ReadUserSince(ctx context.Context, since string, limit int) ([]channel.Record, error) {
	records, err := r.journal.ReadUserMessages(ctx, since, limit)
	if err != nil {
		return nil, fmt.Errorf("read incremental user channel: %w", err)
	}
	return records, nil
}

// ReadUser is an explicit privileged UI view including DMs, bounded to the latest 1000 messages.
func (r *Runtime) ReadUser(ctx context.Context) ([]channel.Record, error) {
	records, err := r.journal.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("read user channel: %w", err)
	}
	result := make([]channel.Record, 0, min(len(records), 1000))
	for i := len(records) - 1; i >= 0 && len(result) < 1000; i-- {
		if deliveryMessage(&records[i]) {
			result = append(result, records[i])
		}
	}
	slices.Reverse(result)
	return result, nil
}

// PostUser derives the privileged sender in process, never from a bearer or tool argument.
func (r *Runtime) PostUser(ctx context.Context, text string, mentions []string) error {
	manager, err := r.current(ctx)
	if err != nil {
		return err
	}
	if manager.ReadOnly {
		return fmt.Errorf("read-only: user channel posting disabled")
	}
	s := r.Service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.options.ReadOnly != nil && s.options.ReadOnly() {
		return fmt.Errorf("user channel posting disabled")
	}
	if strings.TrimSpace(text) == "" || len(text) > s.options.MaxBytes {
		return fmt.Errorf("user message must contain text and fit %d bytes", s.options.MaxBytes)
	}
	post := Post{Text: text, Mentions: mentions}
	recipients, err := s.recipients(post, false)
	if err != nil {
		return err
	}
	now := s.options.Now()
	count, err := s.recent(ctx, "user", now)
	if err != nil {
		return err
	}
	if count >= s.options.Rate {
		return fmt.Errorf("user channel rate limit reached")
	}
	record, err := s.message(ctx, Identity{Team: manager.Team.Name, Member: "user"}, post, recipients, false)
	if err != nil {
		return err
	}
	s.bindAudience(&record)
	record.SenderID, record.Time, record.Hops = "user", now.UTC(), 0
	if err := r.journal.Append(ctx, &record); err != nil {
		return fmt.Errorf("post user channel: %w", err)
	}
	return nil
}
