package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/vqivs/agent-deck/internal/team"
)

// ReportOptions supplies trusted current policy and a durable notification enqueue callback.
// Snapshot must return an owned snapshot; callbacks must not reenter this adapter.
type ReportOptions struct {
	Store    team.ReportStore
	Snapshot func(context.Context) (team.Team, team.DefinitionOfDone, error)
	ReadOnly func() bool
	Observe  func(context.Context, *team.Member) (*team.ReportMetadata, error)
	Notify   func(context.Context, Identity, string, *team.ReportRecord) error
}

// ReportAdapter validates authenticated submit_report calls before persistence or notification.
type ReportAdapter struct {
	mu      sync.Mutex
	options ReportOptions
}

// ReportResult distinguishes persistence from durable notification enqueue, never prompt delivery.
type ReportResult struct {
	Record             team.ReportRecord `json:"record"`
	NotificationQueued bool              `json:"notification_queued"`
	NotificationError  string            `json:"notification_error,omitempty"`
}

func NewReportAdapter(options *ReportOptions) (*ReportAdapter, error) {
	if options == nil || options.Store.Root == "" || options.Snapshot == nil || options.Notify == nil {
		return nil, fmt.Errorf("report adapter requires store, trusted snapshot and durable notifier")
	}
	return &ReportAdapter{options: *options}, nil
}

func (a *ReportAdapter) Submit(ctx context.Context, identity Identity,
	arguments json.RawMessage,
) (ReportResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.readOnly() {
		return ReportResult{}, fmt.Errorf("read-only: report submission disabled")
	}
	t, definition, err := a.options.Snapshot(ctx)
	if err != nil {
		return ReportResult{}, fmt.Errorf("read trusted report policy: %w", err)
	}
	if !slices.Equal(definition, t.DefinitionOfDone) {
		return ReportResult{}, fmt.Errorf("report callback definition differs from current team policy")
	}
	worker, master, err := reportParticipants(&t, identity)
	if err != nil {
		return ReportResult{}, err
	}
	payload, err := team.DecodeDoneReport(arguments)
	if err != nil {
		return ReportResult{}, fmt.Errorf("decode report tool arguments: %w", err)
	}
	if payload.Member != worker.Name || payload.Session != worker.Session {
		return ReportResult{}, fmt.Errorf("report must match authenticated worker and current session")
	}
	if err := payload.Validate(definition); err != nil {
		return ReportResult{}, fmt.Errorf("validate report tool arguments: %w", err)
	}
	var metadata *team.ReportMetadata
	if a.options.Observe != nil {
		metadata, err = a.options.Observe(ctx, worker)
		if err != nil {
			return ReportResult{}, fmt.Errorf("observe report metadata: %w", err)
		}
	}
	if err := a.revalidate(ctx, identity, worker.Session, master, definition); err != nil {
		return ReportResult{}, err
	}
	record, err := a.options.Store.Submit(ctx, &t, identity.Member, definition, &payload, metadata)
	if err != nil {
		return ReportResult{}, fmt.Errorf("persist worker report: %w", err)
	}
	result := ReportResult{Record: record}
	if err := a.options.Notify(ctx, identity, master, &record); err != nil {
		result.NotificationError = "Report saved; master notification was not queued: " + err.Error()
		return result, nil
	}
	result.NotificationQueued = true
	return result, nil
}

func (a *ReportAdapter) readOnly() bool {
	return a.options.Store.ReadOnly || (a.options.ReadOnly != nil && a.options.ReadOnly())
}

func (a *ReportAdapter) revalidate(ctx context.Context, identity Identity, session, master string,
	definition team.DefinitionOfDone,
) error {
	current, currentDefinition, err := a.options.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("refresh report policy: %w", err)
	}
	worker, currentMaster, err := reportParticipants(&current, identity)
	if err != nil || worker.Session != session || currentMaster != master ||
		!slices.Equal(currentDefinition, definition) || !slices.Equal(current.DefinitionOfDone, definition) {
		return fmt.Errorf("report binding or trusted policy changed during submission")
	}
	if a.options.ReadOnly != nil && a.options.ReadOnly() {
		return fmt.Errorf("read-only: report submission disabled")
	}
	return nil
}

func reportParticipants(t *team.Team, identity Identity) (*team.Member, string, error) {
	if identity.Team != t.Name {
		return nil, "", fmt.Errorf("report identity belongs to another team")
	}
	var worker *team.Member
	master := ""
	masters := 0
	for i := range t.Members {
		member := &t.Members[i]
		if member.Name == identity.Member && member.Role == "worker" && member.Session != "" {
			worker = member
		}
		if member.Role == "master" {
			master = member.Name
			masters++
		}
	}
	if worker == nil || masters != 1 {
		return nil, "", fmt.Errorf("report needs a current bound worker and exactly one master")
	}
	return worker, master, nil
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
