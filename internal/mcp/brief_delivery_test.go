package mcp

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/channel"
	"github.com/vqivs/agent-deck/internal/team"
)

func briefFixture(t *testing.T) (*Service, *team.Manager, team.BoardStore, team.TaskNotice) {
	t.Helper()
	manager := &team.Manager{Team: team.Team{Name: "pool", Members: []team.Member{
		{ID: "member-1", Name: "worker", Role: "worker", Session: "session-1"},
	}}}
	store := team.BoardStore{Root: t.TempDir(), Team: "pool"}
	task := team.Task{ID: "T-1", Title: "Work", Assignee: "worker", State: "in_progress"}
	task.Paths = []string{"internal/example/*"}
	task.Criteria = []team.TaskCriterion{{ID: "c1", Text: "Works"}}
	state := team.BoardState{Version: 2, Board: team.TaskBoard{Version: 2, Tasks: []team.Task{task}}}
	if err := store.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	journal, err := channel.Open(filepath.Join(t.TempDir(), "channel.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	service, err := NewService(&Options{
		Team: "pool", Journal: journal,
		Members:       func() []Member { return []Member{{ID: "member-1", Name: "worker", Role: "worker"}} },
		BriefProvider: func(context.Context) (*team.Manager, team.BoardStore, error) { return manager, store, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	notice := team.TaskNotice{
		To: "worker", Task: task.ID, Text: team.TaskBrief(&task),
		Brief: team.BriefDeliveryKey(&task, &manager.Team),
	}
	return service, manager, store, notice
}

func TestBriefDeliveryConcurrentRetryAndRestart(t *testing.T) {
	service, _, store, notice := briefFixture(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := service.Notify(t.Context(), []team.TaskNotice{notice}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	records, err := service.options.Journal.Snapshot(t.Context())
	if err != nil || len(records) != 1 || !records[0].Private {
		t.Fatalf("duplicate or nonprivate briefs: %+v, %v", records, err)
	}
	state, err := store.Load(t.Context())
	if err != nil || !team.BriefDelivered(&state.Board.Tasks[0], notice.Brief) || len(state.Board.Tasks[0].History) != 1 {
		t.Fatalf("delivery not recorded once: %+v %v", state, err)
	}
	// Simulate crash after journal append but before board save by restoring missing history.
	state.Board.Tasks[0].History = nil
	if err := store.Save(t.Context(), &state, state.Revision); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(&service.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Notify(t.Context(), []team.TaskNotice{notice}); err != nil {
		t.Fatal(err)
	}
	records, _ = service.options.Journal.Snapshot(t.Context())
	state, _ = store.Load(t.Context())
	if len(records) != 1 || !team.BriefDelivered(&state.Board.Tasks[0], notice.Brief) {
		t.Fatal("restart duplicated append or failed to recover record")
	}
}

func TestBriefDeliveryFailureNeverRecordsAndOtherNoticesSurvive(t *testing.T) {
	service, _, store, notice := briefFixture(t)
	bad := notice
	bad.Brief = "stale-assignment"
	if err := service.Notify(t.Context(), []team.TaskNotice{bad, {To: "worker", Text: "ordinary"}}); err == nil {
		t.Fatal("stale assignment accepted")
	}
	records, _ := service.options.Journal.Snapshot(t.Context())
	if len(records) != 1 || records[0].Text != "ordinary" {
		t.Fatal("brief failure dropped unrelated notice")
	}
	if err := service.options.Journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.Notify(t.Context(), []team.TaskNotice{notice}); err == nil {
		t.Fatal("failed append accepted")
	}
	state, _ := store.Load(t.Context())
	if team.BriefDelivered(&state.Board.Tasks[0], notice.Brief) {
		t.Fatal("failed journal append recorded delivery")
	}
}

func TestBriefIdleRecoveryAndNewSession(t *testing.T) {
	service, manager, store, notice := briefFixture(t)
	state, _ := store.Load(t.Context())
	workers := []team.SchedulerWorker{{Name: "worker", State: "busy"}}
	if got := team.MissingBriefs(&state.Board, &manager.Team, workers); len(got) != 0 {
		t.Fatal("busy worker received recovery brief")
	}
	workers[0].State = "idle"
	if got := team.MissingBriefs(&state.Board, &manager.Team, workers); len(got) != 1 {
		t.Fatal("old in-progress task not recovered")
	}
	if err := service.Notify(t.Context(), []team.TaskNotice{notice}); err != nil {
		t.Fatal(err)
	}
	state, _ = store.Load(t.Context())
	if got := team.MissingBriefs(&state.Board, &manager.Team, workers); len(got) != 0 {
		t.Fatal("recorded brief retried")
	}
	manager.Team.Members[0].Session = "session-2"
	got := team.MissingBriefs(&state.Board, &manager.Team, workers)
	if len(got) != 1 || got[0].Brief == notice.Brief {
		t.Fatal("replacement session did not get new brief")
	}
	state.Board.Tasks[0].History = append(state.Board.Tasks[0].History,
		team.TaskChange{Field: "assignee", From: "other", To: "worker", At: time.Now()})
	if team.BriefDeliveryKey(&state.Board.Tasks[0], &manager.Team) == got[0].Brief {
		t.Fatal("reassignment reused previous brief identity")
	}
}

func TestBriefReadOnlyAndMutedDoNotAppend(t *testing.T) {
	service, manager, store, notice := briefFixture(t)
	manager.ReadOnly = true
	if err := service.Notify(t.Context(), []team.TaskNotice{notice}); err == nil {
		t.Fatal("read-only manager permitted brief")
	}
	manager.ReadOnly = false
	service.options.ReadOnly = func() bool { return true }
	if err := service.Notify(t.Context(), []team.TaskNotice{notice}); err == nil {
		t.Fatal("read-only service permitted brief")
	}
	service.options.ReadOnly = nil
	manager.Team.Members[0].Muted = true
	if err := service.Notify(t.Context(), []team.TaskNotice{notice}); err == nil {
		t.Fatal("muted member permitted brief")
	}
	records, _ := service.options.Journal.Snapshot(t.Context())
	state, _ := store.Load(t.Context())
	if len(records) != 0 || team.BriefDelivered(&state.Board.Tasks[0], notice.Brief) {
		t.Fatal("authorization failure caused delivery side effect")
	}
}
