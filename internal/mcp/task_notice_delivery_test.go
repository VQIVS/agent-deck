package mcp

import (
	"path/filepath"
	"testing"

	"github.com/vqivs/agent-deck/internal/channel"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestWorkflowEventJournalIdempotence(t *testing.T) {
	transport := newFakeTransport()
	service := newTestService(t, transportOptions(transport))
	notice := team.TaskNotice{To: "bob", Task: "T-1", Text: "Accepted work awaits release", Event: "done:T-1:completion"}
	for range 2 {
		if err := service.Notify(t.Context(), []team.TaskNotice{notice}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := service.options.Journal.Snapshot(t.Context())
	if err != nil || len(records) != 1 {
		t.Fatal("duplicate workflow notice", len(records), err)
	}
	// Reopen the on-disk journal and construct another service: no in-memory
	// deduplication state from the initial service may influence this check.
	path := filepath.Join(t.TempDir(), "restart-journal")
	journal, err := channel.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	options := service.options
	options.Journal = journal
	first, err := NewService(&options)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Notify(t.Context(), []team.TaskNotice{notice}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = channel.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	options.Journal = journal
	restarted, err := NewService(&options)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Notify(t.Context(), []team.TaskNotice{notice}); err != nil {
		t.Fatal(err)
	}
	records, err = journal.Snapshot(t.Context())
	if err != nil || len(records) != 1 {
		t.Fatal("restart duplicated workflow event", len(records), err)
	}
	previousMembers := options.Members
	options.Members = func() []Member {
		members := append([]Member(nil), previousMembers()...)
		for i := range members {
			if members[i].Name == "bob" {
				members[i].ID = "bob-membership-2"
			}
		}
		return members
	}
	replacement, err := NewService(&options)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Notify(t.Context(), []team.TaskNotice{notice}); err != nil {
		t.Fatal(err)
	}
	records, err = journal.Snapshot(t.Context())
	if err != nil || len(records) != 2 || records[1].Audience["bob"] != "bob-membership-2" {
		t.Fatal("replacement did not receive distinct event", records, err)
	}
}
