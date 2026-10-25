package mcp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/vqivs/agent-deck/internal/team"
)

// notifyWorkflowEvent is serialized by Service.Notify. Event identity includes the
// concrete recipient binding so a replacement release-manager can receive its queue.
func (s *Service) notifyWorkflowEvent(ctx context.Context, notice *team.TaskNotice, key string) error {
	member, err := s.member(Identity{Team: s.options.Team, Member: notice.To})
	if err != nil || member.Muted {
		return fmt.Errorf("workflow notice recipient absent or muted")
	}
	id := fmt.Sprintf("workflow:%x", sha256.Sum256([]byte(key+"\x00"+memberID(&member))))
	records, err := s.options.Journal.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read workflow notice journal: %w", err)
	}
	for i := range records {
		if records[i].ID == id {
			return nil
		}
	}
	post := Post{Text: notice.Text, Mentions: []string{notice.To}}
	if len(post.Text) > s.options.MaxBytes {
		post.Text = strings.ToValidUTF8(post.Text[:s.options.MaxBytes], "")
	}
	record, err := s.message(ctx, Identity{Team: s.options.Team, Member: systemSender}, post, post.Mentions, true)
	if err != nil {
		return err
	}
	record.ID, record.Time, record.Hops, record.SenderID = id, s.options.Now().UTC(), 0, systemSender
	s.bindAudience(&record)
	if err := s.options.Journal.Append(ctx, &record); err != nil {
		return fmt.Errorf("append workflow notice: %w", err)
	}
	return nil
}
