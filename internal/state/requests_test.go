package state

import (
	"strings"
	"testing"
	"time"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func TestWaitingEvents(t *testing.T) {
	s := New()
	now := time.Now()
	s.SetStatus("busy", now)
	asks := event(
		"permission.asked",
		`{"sessionID":"session-a","id":"per_a","permission":"bash","patterns":["go test *"]}`,
	)
	if !s.Apply(asks, "session-a", now) || s.DisplayStatus() != "WAITING" ||
		!strings.Contains(s.WaitingText(), "go test *") {
		t.Fatal(s)
	}
	if s.Apply(asks, "session-a", now.Add(time.Second)) || len(s.Permissions) != 1 {
		t.Fatal("duplicate request")
	}
	s.Apply(
		event("session.status", `{"sessionID":"session-a","status":{"type":"busy"}}`),
		"session-a",
		now.Add(time.Second),
	)
	if s.DisplayStatus() != "WAITING" || !s.DisplaySince().Equal(now) {
		t.Fatal("busy hid waiting")
	}
	s.Apply(event("permission.replied", `{"sessionID":"session-other","requestID":"per_a"}`), "session-a", now)
	if !s.Waiting() {
		t.Fatal("cross-session event")
	}
	s.Apply(
		event(
			"question.v2.asked",
			`{"sessionID":"session-a","id":"que_a","questions":[{"header":"Target","question":"Which target?"}]}`,
		),
		"session-a",
		now,
	)
	s.Apply(event("permission.replied", `{"sessionID":"session-a","requestID":"per_a"}`), "session-a", now)
	if !s.Waiting() || !strings.Contains(s.WaitingText(), "Which target?") {
		t.Fatal("question lost")
	}
	s.Apply(event("question.v2.rejected", `{"sessionID":"session-a","requestID":"que_a"}`), "session-a", now)
	if s.DisplayStatus() != "BUSY" {
		t.Fatal(s)
	}
	if !s.Apply(
		event("permission.v2.asked", `{"sessionID":"session-a","id":"per_b","action":"edit","resources":["*.go"]}`),
		"session-a",
		now,
	) ||
		!strings.Contains(s.WaitingText(), "edit · *.go") {
		t.Fatal(s.WaitingText())
	}
	s.Apply(event("permission.v2.replied", `{"sessionID":"session-a","requestID":"per_b"}`), "session-a", now)
	if s.Waiting() {
		t.Fatal("still waiting")
	}
}

func TestWaitingSnapshot(t *testing.T) {
	s := New()
	now := time.Now()
	p := []api.Permission{{ID: "per_a", Permission: "edit"}}
	if !s.UpdateRequests(p, nil, now) || s.UpdateRequests(p, nil, now.Add(time.Second)) {
		t.Fatal("transition notifications")
	}
	s.UpdateRequests(nil, nil, now)
	if s.Waiting() {
		t.Fatal("stale request")
	}
}
