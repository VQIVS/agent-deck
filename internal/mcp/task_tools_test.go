package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func taskToolsFixture(t *testing.T) (*TaskTools, *[]team.TaskNotice) {
	t.Helper()
	manager := &team.Manager{Team: team.Team{Name: "pool", Members: []team.Member{
		{Name: "lead", Role: "master"}, {Name: "ana", Role: "worker"}, {Name: "rev", Role: "worker"},
	}}}
	store := team.BoardStore{Root: t.TempDir(), Team: "pool"}
	sent := &[]team.TaskNotice{}
	tools := &TaskTools{
		Provider: func(context.Context) (*team.Manager, team.BoardStore, error) { return manager, store, nil },
		Notify: func(_ context.Context, notices []team.TaskNotice) error {
			*sent = append(*sent, notices...)
			return nil
		},
	}
	return tools, sent
}

func callTask(t *testing.T, tools *TaskTools, member, name, arguments string) (any, error) {
	t.Helper()
	return tools.Call(t.Context(), Identity{Team: "pool", Member: member}, name, json.RawMessage(arguments))
}

func TestTaskToolsMasterPlansWorkerProgresses(t *testing.T) {
	tools, sent := taskToolsFixture(t)
	if _, err := callTask(t, tools, "ana", "create_task", `{"title":"x","criteria":["a"],"paths":["a/*"]}`); err == nil {
		t.Fatal("worker created a task")
	}
	created := `{"title":"Add flag","criteria":["parsed","documented"],"paths":["cmd/*"],"ready":true,` +
		`"estimate":"45m","reviewer":"rev"}`
	if _, err := callTask(t, tools, "lead", "create_task", created); err != nil {
		t.Fatal(err)
	}
	if _, err := callTask(t, tools, "lead", "set_assignee", `{"id":"T-1","assignee":"ana"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callTask(t, tools, "lead", "move_task", `{"id":"T-1","state":"in_progress"}`); err == nil {
		t.Fatal("master started a ready task directly")
	}
	manager, store, _ := tools.Provider(t.Context())
	_, notices, err := team.ChangeTasks(t.Context(), store, &manager.Team, team.AutomationActor, time.Now(),
		func(b *team.TaskBoard, m team.TaskMutation) error { return b.MoveTask("T-1", "in_progress", m) })
	if err != nil || len(notices) != 1 || notices[0].To != "ana" || !strings.Contains(notices[0].Text, "[c2] documented") {
		t.Fatalf("start notices=%+v err=%v", notices, err)
	}
	if _, err := callTask(t, tools, "rev", "update_my_task", `{"id":"T-1","checked":["c1"]}`); err == nil {
		t.Fatal("non-assignee updated task")
	}
	progress := `{"id":"T-1","checked":["c1","c2"],"comment":"done, tests pass","state":"review"}`
	if _, err := callTask(t, tools, "ana", "update_my_task", progress); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(*sent, func(n team.TaskNotice) bool { return n.To == "rev" }) {
		t.Fatalf("reviewer not notified: %+v", *sent)
	}
	mine, err := callTask(t, tools, "rev", "my_tasks", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if list := mine.([]taskSummary); len(list) != 1 || list[0].Progress != "2/2" || list[0].State != "review" {
		t.Fatalf("reviewer view=%+v", list)
	}
	selfReview := `{"id":"T-1","decision":"accept","comment":"lgtm"}`
	if _, err := callTask(t, tools, "ana", "review_task", selfReview); err == nil {
		t.Fatal("assignee accepted its own review")
	}
	if _, err := callTask(t, tools, "rev", "review_task", `{"id":"T-1","decision":"accept","comment":"ok"}`); err != nil {
		t.Fatal(err)
	}
}

func TestTaskToolsSplitCancelsOriginal(t *testing.T) {
	tools, _ := taskToolsFixture(t)
	big := `{"title":"Big","criteria":["all"],"paths":["a/*"]}`
	if _, err := callTask(t, tools, "lead", "create_task", big); err != nil {
		t.Fatal(err)
	}
	split := `{"id":"T-1","parts":[{"title":"One","criteria":["x"],"paths":["a/one/*"]},` +
		`{"title":"Two","criteria":["y"],"paths":["a/two/*"],"dependencies":["T-2"]}]}`
	if _, err := callTask(t, tools, "lead", "split_task", split); err != nil {
		t.Fatal(err)
	}
	all, err := callTask(t, tools, "lead", "list_tasks", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, task := range all.([]taskSummary) {
		states[task.ID] = task.State
	}
	if states["T-1"] != team.TaskCancelled || states["T-2"] != "backlog" || states["T-3"] != "backlog" {
		t.Fatalf("states=%v", states)
	}
}

func TestSystemNoticeWakesIdleMember(t *testing.T) {
	f := newFakeTransport()
	s := newTestService(t, transportOptions(f))
	notice := []team.TaskNotice{{To: "bob", Task: "T-1", Text: "You are assigned task T-1"}}
	if err := s.Notify(t.Context(), notice); err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts["bob"]) != 1 || !strings.Contains(f.prompts["bob"][0], "You are assigned task T-1") {
		t.Fatalf("prompts=%v", f.prompts)
	}
	if len(f.prompts["alice"]) != 0 {
		t.Fatal("private notice reached another member")
	}
	reply := Post{Text: "on it", Mentions: []string{"alice"}}
	if _, err := s.Post(t.Context(), Identity{"test", "bob"}, reply, false); err != nil {
		t.Fatalf("member could not answer after a notice: %v", err)
	}
}
