package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qivs/agent-deck/internal/channel"
	"github.com/qivs/agent-deck/internal/team"
)

func reportAdapterFixture(t *testing.T) (*ReportOptions, json.RawMessage) {
	t.Helper()
	options := &ReportOptions{
		Store: team.ReportStore{Root: filepath.Join(t.TempDir(), "reports")},
		Snapshot: func(context.Context) (team.Team, team.DefinitionOfDone, error) {
			definition := team.DefinitionOfDone{{ID: "tests", Text: "Run tests", Required: true}}
			return team.Team{Name: "team", DefinitionOfDone: definition, Members: []team.Member{
				{Name: "lead", Role: "master", Session: "ses_lead"},
				{Name: "worker", Role: "worker", Session: "ses_worker"},
			}}, definition, nil
		},
		Notify: func(context.Context, Identity, string, *team.ReportRecord) error { return nil },
	}
	return options, json.RawMessage(`{"member":"worker","session":"ses_worker","summary":"done",` +
		`"checklist":[{"id":"tests","complete":true}],` +
		`"tests":[{"criterion_id":"tests","command":"go test","outcome":"pass"}]}`)
}

func TestReportAdapterGuards(t *testing.T) {
	for _, name := range []string{"foreign", "master", "read-only", "stale", "forged-metadata", "role-change"} {
		t.Run(name, func(t *testing.T) {
			options, arguments := reportAdapterFixture(t)
			identity := Identity{Team: "team", Member: "worker"}
			switch name {
			case "foreign":
				identity.Team = "other"
			case "master":
				identity.Member = "lead"
			case "read-only":
				options.ReadOnly = func() bool { return true }
			case "stale":
				arguments = json.RawMessage(`{"member":"worker","session":"old","summary":"done","checklist":[]}`)
			case "forged-metadata":
				arguments = json.RawMessage(`{"independently_observed":{"commit":"fake"}}`)
			case "role-change":
				snapshot := options.Snapshot
				options.Snapshot = func(ctx context.Context) (team.Team, team.DefinitionOfDone, error) {
					tm, definition, err := snapshot(ctx)
					tm.Members[1].Role = "master"
					return tm, definition, err
				}
			}
			notified := false
			options.Notify = func(context.Context, Identity, string, *team.ReportRecord) error {
				notified = true
				return nil
			}
			adapter, err := NewReportAdapter(options)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := adapter.Submit(t.Context(), identity, arguments); err == nil {
				t.Fatal("invalid submission accepted")
			}
			if _, err := os.Stat(options.Store.Root); !os.IsNotExist(err) || notified {
				t.Fatal("rejected submission caused side effects", err)
			}
		})
	}
}

func TestReportAdapterObservedAndNotificationFailure(t *testing.T) {
	options, arguments := reportAdapterFixture(t)
	options.Observe = func(context.Context, *team.Member) (*team.ReportMetadata, error) {
		return &team.ReportMetadata{ObservedAt: time.Now(), Commit: "independent", Dirty: true}, nil
	}
	options.Notify = func(ctx context.Context, identity Identity, master string, record *team.ReportRecord) error {
		tm, _, _ := options.Snapshot(ctx)
		if _, err := options.Store.Load(ctx, &tm, identity.Member); err != nil {
			t.Fatal("notification ran before durable report save", err)
		}
		if master != "lead" || record.Observed.Commit != "independent" {
			t.Fatal("notification identity or metadata lost")
		}
		return fmt.Errorf("queue unavailable")
	}
	adapter, err := NewReportAdapter(options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Submit(t.Context(), Identity{Team: "team", Member: "worker"}, arguments)
	if err != nil || result.NotificationQueued || result.NotificationError == "" || !result.Record.ReportedDone {
		t.Fatal("persistence and enqueue failure not distinguished", result, err)
	}
}

func TestReportNotificationDurablyQueuesForMaster(t *testing.T) {
	journal, err := channel.Open(filepath.Join(t.TempDir(), "channel.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	service, err := NewService(&Options{
		Team: "team", Journal: journal,
		Members: func() []Member {
			return []Member{{ID: "lead-id", Name: "lead", Role: "master"}, {ID: "worker-id", Name: "worker", Role: "worker"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	options, arguments := reportAdapterFixture(t)
	options.Notify = ChannelReportNotifier(service)
	adapter, err := NewReportAdapter(options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Submit(t.Context(), Identity{Team: "team", Member: "worker"}, arguments)
	if err != nil || !result.NotificationQueued {
		t.Fatal("report not queued", err)
	}
	records, err := journal.Read(t.Context(), "lead", "")
	if err != nil || len(records) != 1 || !records[0].Private || records[0].Recipients[0] != "lead" {
		t.Fatal("missing durable private notification", records, err)
	}
}

func TestReportAdapterRejectsChangedPolicyDuringObservation(t *testing.T) {
	options, arguments := reportAdapterFixture(t)
	snapshot := options.Snapshot
	changed := false
	options.Observe = func(context.Context, *team.Member) (*team.ReportMetadata, error) {
		changed = true
		return nil, nil
	}
	options.Snapshot = func(ctx context.Context) (team.Team, team.DefinitionOfDone, error) {
		tm, definition, err := snapshot(ctx)
		if changed {
			tm.Members[1].Session = "replacement"
		}
		return tm, definition, err
	}
	adapter, err := NewReportAdapter(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Submit(t.Context(), Identity{Team: "team", Member: "worker"}, arguments); err == nil {
		t.Fatal("binding changed during observation accepted")
	}
	if _, err := os.Stat(options.Store.Root); !os.IsNotExist(err) {
		t.Fatal("stale report persisted", err)
	}
}
