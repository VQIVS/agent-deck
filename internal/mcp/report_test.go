package mcp

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/channel"
	"github.com/vqivs/agent-deck/internal/team"
)

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
	record := team.ReportRecord{Member: "worker", RecordedAt: time.Now(), ReportedDone: true}
	notify := ChannelReportNotifier(service)
	if err := notify(t.Context(), Identity{Team: "team", Member: "worker"}, "lead", &record); err != nil {
		t.Fatal("report not queued", err)
	}
	records, err := journal.ReadMessages(t.Context(), "lead", "lead-id", "", 100)
	if err != nil || len(records) != 1 || !records[0].Private || records[0].Recipients[0] != "lead" {
		t.Fatal("missing durable private notification", records, err)
	}
}
