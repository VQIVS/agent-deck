package state

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

func event(kind, props string) api.Event {
	return api.Event{Type: kind, Properties: json.RawMessage(props)}
}

func TestReduce(t *testing.T) {
	s := New()
	now := time.Now()
	if !s.Apply(event("session.status", `{"sessionID":"session-a","status":{"type":"busy"}}`), "session-a", now) ||
		s.Status != "BUSY" {
		t.Fatal(s)
	}
	s.Apply(
		event("message.part.updated", `{"part":{"sessionID":"session-a","id":"p","type":"text","text":"hello"}}`),
		"session-a",
		now,
	)
	e := event("message.part.delta", `{"sessionID":"session-a","partID":"p","field":"text","delta":" world"}`)
	e.ID = "evt_once"
	s.Apply(e, "session-a", now)
	s.Apply(e, "session-a", now)
	if s.Tail() != "hello world\n" {
		t.Fatal(s.Tail())
	}
	s.Apply(event("session.status", `{"sessionID":"session-other","status":{"type":"idle"}}`), "session-a", now)
	if s.Status != "BUSY" {
		t.Fatal("cross-session leak")
	}
	s.Apply(event("future.unknown", `{"sessionID":"session-a"}`), "session-a", now)
	s.Apply(event("session.error", `{"sessionID":"session-a","error":{"message":"oops"}}`), "session-a", now)
	if s.Status != "ERROR" || !strings.Contains(s.Error, "oops") {
		t.Fatal(s)
	}
	s.Apply(event("session.idle", `{"sessionID":"session-a"}`), "session-a", now.Add(time.Minute))
	if s.Error != "" || s.Status != "IDLE" || !s.Since.Equal(now.Add(time.Minute)) {
		t.Fatal(s)
	}
	s.Apply(event("message.part.removed", `{"sessionID":"session-a","partID":"p"}`), "session-a", now)
	if len(s.Parts) != 0 {
		t.Fatal(s)
	}
}

func TestBoundedAndTool(t *testing.T) {
	s := New()
	for i := 0; i < MaxParts+20; i++ {
		s.Upsert(&api.Part{ID: fmt.Sprint(i), Type: "text", Text: strings.Repeat("a", 30000)})
	}
	if len(s.Parts) != MaxParts || len(s.Parts[0].Text) != 24000 {
		t.Fatal("unbounded")
	}
	s.Apply(
		event(
			"message.part.updated",
			`{"sessionID":"session-a","part":{"id":"tool","type":"tool","tool":"bash",
"state":{"status":"running","input":{"command":"go test"}}}}`,
		),
		"session-a",
		time.Now(),
	)
	if !strings.Contains(s.Tool(), "go test") {
		t.Fatal(s.Tool())
	}
}
