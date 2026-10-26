package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestReviewAndCommentsWakeAssigneeAndMasterOnce(t *testing.T) {
	tools, _ := taskToolsFixture(t)
	transport := newFakeTransport()
	service := newTestService(t, func(o *Options) {
		transportOptions(transport)(o)
		o.Team = "pool"
		o.Members = func() []Member {
			return []Member{{Name: "lead", Role: "master"}, {Name: "ana", Role: "worker"}, {Name: "rev", Role: "worker"}}
		}
	})
	var emitted []team.TaskNotice
	tools.Notify = func(ctx context.Context, notices []team.TaskNotice) error {
		emitted = append(emitted, notices...)
		return service.Notify(ctx, notices)
	}
	call := func(member, name, payload string) {
		t.Helper()
		if _, err := tools.Call(t.Context(), Identity{Team: "pool", Member: member}, name,
			json.RawMessage(payload)); err != nil {
			t.Fatal(err)
		}
	}
	call("lead", "create_task", `{"title":"work","criteria":["check"],"paths":["src/**"],`+
		`"assignee":"ana","reviewer":"rev","ready":true}`)
	manager, store, err := tools.Provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = team.ChangeTasks(t.Context(), store, &manager.Team, team.AutomationActor, tools.now(),
		func(board *team.TaskBoard, mutation team.TaskMutation) error {
			return board.MoveTask("T-1", "in_progress", mutation)
		})
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		call("rev", "comment_task", `{"id":"T-1","text":"independent review evidence"}`)
	}
	call("ana", "update_my_task", `{"id":"T-1","state":"review"}`)
	call("rev", "review_task", `{"id":"T-1","decision":"accept","comment":"accepted exact change"}`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.Notify(ctx, emitted); err == nil {
		t.Fatal("canceled enqueue was not reported")
	}
	if err := service.Notify(t.Context(), emitted); err != nil {
		t.Fatal(err)
	}
	transport.fail = true
	if err := service.Deliver(t.Context()); err == nil {
		t.Fatal("failed delivery was not reported")
	}
	transport.fail = false
	transport.prompts = make(map[string][]string)
	service, err = NewService(&service.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Notify(t.Context(), emitted); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.Deliver(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	for _, member := range []string{"ana", "lead"} {
		text := strings.Join(transport.prompts[member], "\n")
		if got := strings.Count(text, "independent review evidence"); got != 5 {
			t.Fatalf("%s: comments lost or duplicated: %d", member, got)
		}
		if got := strings.Count(text, "accepted exact change"); got != 1 {
			t.Fatalf("%s: review result lost or duplicated: %d", member, got)
		}
	}
}
