package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vqivs/agent-deck/internal/team"
)

func reportTool() map[string]any {
	text := map[string]any{"type": "string"}
	return tool("submit_report", "Submit a structured worker report; identity is derived from authentication",
		map[string]any{
			"summary":     text,
			"checklist":   map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"tests":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"open_issues": map[string]any{"type": "array", "items": text},
		}, []string{"summary", "checklist"})
}

func (r *Runtime) submitReport(ctx context.Context, identity Identity, raw json.RawMessage) (any, error) {
	manager, err := r.current(ctx)
	if err != nil {
		return nil, err
	}
	if manager.ReadOnly || r.readOnly() || identity.Team != manager.Team.Name {
		return nil, fmt.Errorf("report submission is disabled")
	}
	index, err := manager.MemberIndex(identity.Member)
	if err != nil {
		return nil, fmt.Errorf("find report member: %w", err)
	}
	member := &manager.Team.Members[index]
	if member.Role != "worker" || member.Muted || (r.options.Muted != nil && r.options.Muted(member.Name)) {
		return nil, fmt.Errorf("report submission requires an unmuted worker")
	}
	report, err := team.DecodeDoneReport(raw)
	if err != nil {
		return nil, fmt.Errorf("decode submitted report: %w", err)
	}
	if report.Member != "" || report.Session != "" {
		return nil, fmt.Errorf("report identity must not be caller supplied")
	}
	report.Member, report.Session = member.Name, member.Session
	if err := report.Validate(manager.Team.DefinitionOfDone); err != nil {
		return nil, fmt.Errorf("validate submitted report: %w", err)
	}
	result, err := r.options.SubmitReport(ctx, manager, member.Name, &report)
	if err != nil {
		return nil, fmt.Errorf("persist submitted report: %w", err)
	}
	return r.notifyReport(ctx, identity, manager, result), nil
}

func (r *Runtime) notifyReport(ctx context.Context, identity Identity, manager *team.Manager, result any) any {
	if record, ok := result.(team.ReportRecord); ok {
		for i := range manager.Team.Members {
			master := &manager.Team.Members[i]
			if master.Role != "master" {
				continue
			}
			err := ChannelReportNotifier(r.Service)(context.WithoutCancel(ctx), identity, master.Name, &record)
			if err != nil {
				return ReportResult{Record: record, NotificationError: err.Error()}
			}
			return ReportResult{Record: record, NotificationQueued: true}
		}
	}
	return result
}
