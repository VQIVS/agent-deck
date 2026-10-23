package state

import (
	"encoding/json"
	"testing"
	"time"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func TestStallIgnoresPollsAndRepeatedParts(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewWithClock(func() time.Time { return now }, time.Minute)
	s.SetStatus("busy", now)
	part := event("message.part.updated", `{"part":{"sessionID":"s","id":"p","type":"text","text":"hello"}}`)
	s.Apply(part, "s", now)
	now = now.Add(time.Minute)
	s.SetStatus("busy", now)
	s.Apply(part, "s", now)
	if s.DisplayStatus() != "STALLED" || !s.LastEvent.Equal(now) || s.LastActivity.Equal(now) {
		t.Fatalf("poll or duplicate part hid stall: %+v", s)
	}
	s.UpdateRequests([]api.Permission{{ID: "permission"}}, nil, now)
	if s.DisplayStatus() != "WAITING" || s.Stalled(now) {
		t.Fatal("waiting must win")
	}
	s.UpdateRequests(nil, nil, now)
	delta := event("message.part.delta", `{"sessionID":"s","partID":"p","field":"text","delta":"!"}`)
	s.Apply(delta, "s", now)
	if s.DisplayStatus() != "BUSY" || !s.LastActivity.Equal(now) {
		t.Fatal("meaningful delta must clear stall")
	}
	s.SetStatus("idle", now)
	now = now.Add(time.Hour)
	if s.DisplayStatus() != "IDLE" {
		t.Fatal("idle must never stall")
	}
}

func TestSnapshotDoesNotRefreshUnchangedActivity(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewWithClock(func() time.Time { return now }, time.Minute)
	s.SetStatus("busy", now)
	messages := []api.Message{{Parts: []api.Part{{ID: "p", Type: "text", Text: "hello"}}}}
	s.Snapshot(messages)
	now = now.Add(time.Minute)
	s.Snapshot(messages)
	if s.DisplayStatus() != "STALLED" {
		t.Fatal("snapshot poll hid stall")
	}
	messages[0].Parts[0].Text = "changed"
	s.Snapshot(messages)
	if s.DisplayStatus() != "BUSY" {
		t.Fatal("changed snapshot should restore activity")
	}
}

func TestContextUsesLatestAssistantAndMayDecrease(t *testing.T) {
	var messages []api.Message
	err := json.Unmarshal([]byte(`[
		{"info":{"id":"new","role":"assistant","time":{"created":200},"tokens":{"input":20,"cache":{"read":30}}}},
		{"info":{"id":"old","role":"assistant","time":{"created":100},"tokens":{"input":900}}},
		{"info":{"id":"user","role":"user","time":{"created":300},"tokens":{"input":999}}}
	]`), &messages)
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Snapshot(messages)
	if s.ContextTokens != 50 {
		t.Fatalf("context = %d", s.ContextTokens)
	}
	s.Apply(event("message.updated", `{"info":{"id":"compact","sessionID":"s","role":"assistant",
		"time":{"created":400},"tokens":{"input":5,"cache":{"read":2}}}}`), "s", time.Now())
	if s.ContextTokens != 7 {
		t.Fatalf("compacted context = %d", s.ContextTokens)
	}
	s.Apply(event("message.updated", `{"info":{"id":"other","sessionID":"other","role":"assistant",
		"time":{"created":500},"tokens":{"input":999}}}`), "s", time.Now())
	if s.ContextTokens != 7 {
		t.Fatal("cross-session context leak")
	}
}

func TestStallDisabledAndBusyEpoch(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewWithClock(func() time.Time { return now }, 0)
	s.SetStatus("busy", now)
	now = now.Add(time.Hour)
	if s.Stalled(now) {
		t.Fatal("zero threshold must disable stalls")
	}
	s.StallAfter = time.Minute
	s.SetStatus("idle", now)
	s.SetStatus("busy", now)
	if s.Stalled(now) {
		t.Fatal("new busy epoch inherited old activity")
	}
}
