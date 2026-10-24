package team

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAutoReconcileRecoversExistingIdleBrief(t *testing.T) {
	busy := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			if busy {
				_, _ = w.Write([]byte(`{"ses_worker":{"type":"busy"}}`))
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
		case strings.HasSuffix(r.URL.Path, "/message"), r.URL.Path == "/permission", r.URL.Path == "/question":
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/session":
			_, _ = w.Write([]byte(`[{"id":"ses_worker","title":"worker"}]`))
		default:
			_, _ = w.Write([]byte(`{"id":"ses_worker","title":"worker"}`))
		}
	}))
	defer server.Close()
	store, board := poolRuntimeFixture(t, server.URL)
	current, _, err := store.Load(t.Context(), "pool")
	if err != nil {
		t.Fatal(err)
	}
	worker := current.Members[1].Name
	task := Task{
		ID: "T-1", Title: "Existing work", Assignee: worker, Worker: worker, State: "in_progress",
		Paths: []string{"internal/example/*"}, Criteria: []TaskCriterion{{ID: "c1", Text: "Works"}},
	}
	state := BoardState{
		Version: 2, Board: TaskBoard{Version: 2, Tasks: []Task{task}},
		Policy: SchedulingPolicy{Approval: "off"},
	}
	if err := board.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	pool, err := OpenPool(t.Context(), store, "pool", board)
	if err != nil {
		t.Fatal(err)
	}
	notices, err := pool.AutoReconcile(t.Context())
	if err != nil || len(notices) != 1 || notices[0].Brief == "" {
		t.Fatalf("lost initial brief not recovered: %+v %v", notices, err)
	}
	busy = true
	if got, err := pool.AutoReconcile(t.Context()); err != nil || len(got) != 0 {
		t.Fatalf("busy worker briefed: %+v %v", got, err)
	}
	busy = false
	if err := RecordBriefDelivery(t.Context(), board, &current, &notices[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := pool.AutoReconcile(t.Context()); err != nil || len(got) != 0 {
		t.Fatalf("recorded delivery repeated: %+v %v", got, err)
	}
}
