package mcp

import (
	"context"
	"fmt"

	"github.com/vqivs/agent-deck/internal/team"
)

// ReportResult distinguishes persistence from durable notification enqueue, never prompt delivery.
type ReportResult struct {
	Record             team.ReportRecord `json:"record"`
	NotificationQueued bool              `json:"notification_queued"`
	NotificationError  string            `json:"notification_error,omitempty"`
}

// ChannelReportNotifier enqueues a private master notification through the guarded durable channel.
func ChannelReportNotifier(service *Service) func(context.Context, Identity, string, *team.ReportRecord) error {
	return func(ctx context.Context, identity Identity, master string, record *team.ReportRecord) error {
		if service == nil {
			return fmt.Errorf("report channel service unavailable")
		}
		text := fmt.Sprintf("Worker-reported completion report from %s (session %s); reported_done=%t. "+
			"Recorded at %s. Read the saved structured report; checklist and test outcomes are worker claims.",
			record.Member, record.Session, record.ReportedDone, record.RecordedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
		if _, err := service.Post(ctx, identity, Post{Member: master, Text: text}, true); err != nil {
			return fmt.Errorf("enqueue report notification: %w", err)
		}
		return nil
	}
}
