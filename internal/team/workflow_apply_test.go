package team

import (
	"context"
	"strings"
	"testing"
	"time"
)

func workflowTeam() *Team {
	return &Team{Name: "pool", Members: []Member{
		{Name: "lead", Role: "master"}, {Name: "ana", Role: "worker"}, {Name: "rev", Role: "worker"},
	}}
}

func noticeFor(notices []TaskNotice, to, contains string) bool {
	for _, n := range notices {
		if n.To == to && strings.Contains(n.Text, contains) {
			return true
		}
	}
	return false
}

func TestResolveTaskActorUsesRoster(t *testing.T) {
	team := workflowTeam()
	actor, err := team.ResolveTaskActor("lead")
	if err != nil || actor.Role != "master" || actor.Member != "lead" {
		t.Fatalf("actor=%+v err=%v", actor, err)
	}
	if _, err := team.ResolveTaskActor("intruder"); err == nil {
		t.Fatal("unknown member resolved")
	}
}

func TestChangeTasksFlowAndNotices(t *testing.T) {
	ctx := context.Background()
	team := workflowTeam()
	store := BoardStore{Root: t.TempDir(), Team: team.Name}
	lead, _ := team.ResolveTaskActor("lead")
	ana, _ := team.ResolveTaskActor("ana")
	rev, _ := team.ResolveTaskActor("rev")
	now := time.Now()
	change := func(actor TaskActor, fn func(*TaskBoard, TaskMutation) error) []TaskNotice {
		t.Helper()
		_, notices, err := ChangeTasks(ctx, store, team, actor, now, fn)
		if err != nil {
			t.Fatal(err)
		}
		return notices
	}
	change(lead, func(b *TaskBoard, m TaskMutation) error {
		task := &Task{
			ID: "T-1", Title: "Add flag", Paths: Leases{"cmd/*"}, Reviewer: "rev",
			Criteria: []TaskCriterion{{ID: "c1", Text: "flag parsed"}},
		}
		if err := b.CreateTask(task, m); err != nil {
			return err
		}
		return b.MoveTask("T-1", "ready", m)
	})
	notices := change(lead, func(b *TaskBoard, m TaskMutation) error { return b.SetAssignee("T-1", "ana", m) })
	if noticeFor(notices, "ana", "You are assigned") {
		t.Fatal("brief sent before the task started")
	}
	notices = change(AutomationActor, func(b *TaskBoard, m TaskMutation) error {
		return b.MoveTask("T-1", "in_progress", m)
	})
	if !noticeFor(notices, "ana", "You are assigned task T-1") {
		t.Fatalf("brief missing on start: %+v", notices)
	}
	notices = change(ana, func(b *TaskBoard, m TaskMutation) error { return b.MoveTask("T-1", "review", m) })
	if !noticeFor(notices, "rev", "ready for your review") || !noticeFor(notices, "lead", "moved to review") {
		t.Fatalf("review notices missing: %+v", notices)
	}
	notices = change(rev, func(b *TaskBoard, m TaskMutation) error {
		return b.ReviewTask("T-1", "changes", "handle empty flag", m)
	})
	if !noticeFor(notices, "ana", "requested changes") {
		t.Fatalf("changes notice missing: %+v", notices)
	}
	notices = change(ana, func(b *TaskBoard, m TaskMutation) error { return b.CommentTask("T-1", "fixed\nall", m) })
	if noticeFor(notices, "ana", "") || !noticeFor(notices, "lead", "member-written text") ||
		!noticeFor(notices, "lead", "> fixed\n> all") {
		t.Fatalf("comment notices wrong: %+v", notices)
	}
	change(ana, func(b *TaskBoard, m TaskMutation) error { return b.MoveTask("T-1", "review", m) })
	notices = change(rev, func(b *TaskBoard, m TaskMutation) error { return b.ReviewTask("T-1", "accept", "ok", m) })
	if !noticeFor(notices, "lead", "is now done") {
		t.Fatalf("done notice missing: %+v", notices)
	}
	state, err := store.Load(ctx)
	if err != nil || state.Board.Tasks[0].State != "done" || state.Revision != 8 {
		t.Fatalf("persisted state=%+v err=%v", state.Board.Tasks, err)
	}
}

func TestChangeTasksRejectsWorkerPlanning(t *testing.T) {
	team := workflowTeam()
	store := BoardStore{Root: t.TempDir(), Team: team.Name}
	ana, _ := team.ResolveTaskActor("ana")
	_, _, err := ChangeTasks(context.Background(), store, team, ana, time.Now(), func(b *TaskBoard, m TaskMutation) error {
		return b.CreateTask(&Task{ID: "T-1", Title: "x", Paths: Leases{"a/*"}}, m)
	})
	if err == nil {
		t.Fatal("worker created a task")
	}
	if _, loadErr := store.Load(context.Background()); loadErr == nil {
		t.Fatal("failed change was persisted")
	}
}

func TestWorkflowInstructionsOnlyWithTools(t *testing.T) {
	team := workflowTeam()
	if workflowInstructions(team, &team.Members[0]) != "" {
		t.Fatal("manual added without tools")
	}
	team.LeasePool = true
	if !strings.Contains(workflowInstructions(team, &team.Members[0]), "you are the master") ||
		!strings.Contains(workflowInstructions(team, &team.Members[1]), "you are a worker") {
		t.Fatal("role manuals missing")
	}
}

func TestAutomationActorAssignsButNeverReviews(t *testing.T) {
	board, m := workflowFixture(t)
	auto := m
	auto.Actor = AutomationActor
	if err := board.MoveTask("task", "ready", auto); err != nil {
		t.Fatal(err)
	}
	if err := board.SetAssignee("task", "worker", auto); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask("task", "in_progress", auto); err != nil {
		t.Fatal(err)
	}
	worker := m
	worker.Actor = TaskActor{Member: "worker", Role: "worker"}
	if err := board.MoveTask("task", "review", worker); err != nil {
		t.Fatal(err)
	}
	if err := board.ReviewTask("task", "accept", "auto", auto); err == nil {
		t.Fatal("automation accepted a review")
	}
	forged := m
	forged.Actor = TaskActor{Member: "intruder", Automation: true}
	if err := board.CommentTask("task", "hi", forged); err == nil {
		t.Fatal("forged automation actor accepted")
	}
	last := board.Tasks[0].History
	if !strings.Contains(last[len(last)-2].Actor, "agent-deck") {
		t.Fatalf("automation not attributed: %+v", last)
	}
}

func TestCommentWakesAreBounded(t *testing.T) {
	ctx := context.Background()
	team := workflowTeam()
	store := BoardStore{Root: t.TempDir(), Team: team.Name}
	lead, _ := team.ResolveTaskActor("lead")
	ana, _ := team.ResolveTaskActor("ana")
	steps := []struct {
		actor TaskActor
		fn    func(*TaskBoard, TaskMutation) error
	}{
		{lead, func(b *TaskBoard, m TaskMutation) error {
			_, err := b.PlanTask(&TaskSpec{
				Title: "x", Criteria: []string{"a"}, Paths: []string{"a/*"}, Ready: true,
				Assignee: "ana", Reviewer: "rev",
			}, m)
			return err
		}},
		{AutomationActor, func(b *TaskBoard, m TaskMutation) error { return b.MoveTask("T-1", "in_progress", m) }},
	}
	for _, step := range steps {
		if _, _, err := ChangeTasks(ctx, store, team, step.actor, time.Now(), step.fn); err != nil {
			t.Fatal(err)
		}
	}
	woken := 0
	for i := range 6 {
		actor := []TaskActor{ana, lead}[i%2]
		_, notices, err := ChangeTasks(ctx, store, team, actor, time.Now(), func(b *TaskBoard, m TaskMutation) error {
			return b.CommentTask("T-1", "ping", m)
		})
		if err != nil {
			t.Fatal(err)
		}
		woken += len(notices)
	}
	if woken != maxCommentWakes {
		t.Fatalf("comment wakes=%d, want %d", woken, maxCommentWakes)
	}
}

func TestChangeTasksRequiresRosterParticipants(t *testing.T) {
	team := workflowTeam()
	store := BoardStore{Root: t.TempDir(), Team: team.Name}
	lead, _ := team.ResolveTaskActor("lead")
	plan := func(b *TaskBoard, m TaskMutation) error {
		_, err := b.PlanTask(&TaskSpec{Title: "x", Criteria: []string{"a"}, Paths: []string{"a/*"}, Reviewer: "ghost"}, m)
		return err
	}
	_, _, err := ChangeTasks(context.Background(), store, team, lead, time.Now(), plan)
	if err == nil {
		t.Fatal("unknown reviewer accepted")
	}
}

func TestOneActiveTaskPerWorkerAndToolStartGuard(t *testing.T) {
	board := &TaskBoard{Version: 2}
	m := TaskMutation{Actor: TaskActor{Member: "lead", Role: "master"}, Now: time.Now()}
	for _, title := range []string{"one", "two"} {
		if _, err := board.PlanTask(&TaskSpec{
			Title: title, Criteria: []string{"a"}, Paths: []string{title + "/*"},
			Ready: true, Assignee: "ana", Reviewer: "rev",
		}, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := board.moveByTool("T-1", "in_progress", m); err == nil {
		t.Fatal("master started a ready task directly")
	}
	if err := board.moveByTool("T-2", "blocked", m); err != nil {
		t.Fatal(err)
	}
	if err := board.moveByTool("T-2", "in_progress", m); err == nil {
		t.Fatal("blocked detour started an unstarted task")
	}
	auto := m
	auto.Actor = AutomationActor
	if err := board.MoveTask("T-1", "in_progress", auto); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask("T-2", "in_progress", auto); err == nil {
		t.Fatal("worker got two active tasks")
	}
}

func TestReservedMemberNames(t *testing.T) {
	for _, name := range []string{"agent-deck", "User"} {
		tm := example()
		tm.Members[1].Name = name
		if err := tm.Validate(); err == nil {
			t.Fatalf("reserved name %q accepted", name)
		}
	}
}

func TestPlannedTaskWaitsForBusyAssigneeAndReassignNotifies(t *testing.T) {
	board := &TaskBoard{Version: 2}
	m := TaskMutation{Actor: TaskActor{Member: "lead", Role: "master"}, Now: time.Unix(10000, 0)}
	if _, err := board.PlanTask(&TaskSpec{
		Title: "x", Criteria: []string{"a"}, Paths: []string{"x/*"},
		Ready: true, Assignee: "alice", Reviewer: "bob",
	}, m); err != nil {
		t.Fatal(err)
	}
	input := &SchedulingInput{
		Board: board, Policy: SchedulingPolicy{MaxWorkers: 4, Cooldown: time.Minute}, Now: time.Unix(10000, 0),
		Workers: []SchedulerWorker{{Name: "alice", Role: "worker", State: "busy"}},
	}
	plan, err := PlanSchedule(input)
	if err != nil || len(plan.Proposals) != 0 || plan.Blocked["T-1"] != "waiting for alice" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	auto := m
	auto.Actor = AutomationActor
	if err := board.MoveTask("T-1", "in_progress", auto); err != nil {
		t.Fatal(err)
	}
	started := cloneBoard(board)
	if err := board.SetAssignee("T-1", "carol", m); err != nil {
		t.Fatal(err)
	}
	team := &Team{Members: []Member{
		{Name: "lead", Role: "master"}, {Name: "alice", Role: "worker"}, {Name: "carol", Role: "worker"},
	}}
	notices := TaskNotices(&started, board, team, m.Actor)
	if !noticeFor(notices, "alice", "You were unassigned") {
		t.Fatalf("old assignee not told: %+v", notices)
	}
}
