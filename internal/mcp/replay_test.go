package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qivs/agent-deck/internal/channel"
)

func TestAcknowledgmentAndRateSurviveServiceRestart(t *testing.T) {
	f := newFakeTransport()
	s := newTestService(t, func(o *Options) {
		transportOptions(f)(o)
		o.Rate = 1
	})
	ctx := t.Context()
	alice, bob := Identity{"test", "alice"}, Identity{"test", "bob"}
	if _, err := s.Post(ctx, alice, Post{Text: "read me", Member: "bob"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, bob, ""); err != nil {
		t.Fatal(err)
	}
	s, err := NewService(&s.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(ctx); err != nil || len(f.prompts) != 0 {
		t.Fatalf("ack lost on restart: %v, %v", f.prompts, err)
	}
	if _, err := s.Post(ctx, alice, Post{Text: "rate bypass"}, false); err == nil {
		t.Fatal("restart bypassed rate limit")
	}
	if _, err := s.BroadcastRoster(ctx, "roster"); err != nil {
		t.Fatal(err)
	}
	s, err = NewService(&s.options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BroadcastRoster(ctx, "second roster"); err == nil {
		t.Fatal("restart bypassed roster rate limit")
	}
}

func TestCrashBetweenSendAndDeliveryRecord(t *testing.T) {
	f := newFakeTransport()
	s := newTestService(t, transportOptions(f))
	path := filepath.Join(t.TempDir(), "crash-journal")
	j, err := channel.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.options.Journal = j
	f.afterSend = func() { j.Close() }
	message, err := s.Post(t.Context(), Identity{"test", "alice"}, Post{Text: "wake", Member: "bob"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(t.Context()); err == nil {
		t.Fatal("missing delivery record was not reported")
	}
	j, err = channel.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	s.options.Journal = j
	s, err = NewService(&s.options)
	if err != nil {
		t.Fatal(err)
	}
	f.afterSend = nil
	if err := s.Deliver(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts["bob"]) != 2 {
		t.Fatal("successful send without journal confirmation was not retried")
	}
	for _, prompt := range f.prompts["bob"] {
		if !strings.Contains(prompt, message.ID) {
			t.Fatal("crash retry changed dedup ID")
		}
	}
}

func TestQueuedMessageRechecksSenderPolicy(t *testing.T) {
	f := newFakeTransport()
	roster := []Member{{Name: "alice", Role: "master"}, {Name: "bob", Role: "worker"}}
	s := newTestService(t, func(o *Options) {
		transportOptions(f)(o)
		o.Members = func() []Member { return roster }
		o.MaxHops = 1
	})
	ctx := t.Context()
	alice := Identity{"test", "alice"}
	r, err := s.Post(ctx, alice, Post{Text: "parent"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Post(ctx, alice, Post{Text: "queued", Mentions: []string{"bob"}, ReplyTo: r.ID}, false); err != nil {
		t.Fatal(err)
	}
	roster[0].Role = "worker"
	if err := s.Deliver(ctx); err != nil || len(f.prompts) != 0 {
		t.Fatal("demoted sender bypassed hop guard", err)
	}
	roster[0].Role, roster[0].Muted = "master", true
	if err := s.Deliver(ctx); err != nil || len(f.prompts) != 0 {
		t.Fatal("muted sender delivered", err)
	}
	roster = roster[1:]
	if err := s.Deliver(ctx); err != nil || len(f.prompts) != 0 {
		t.Fatal("removed sender delivered", err)
	}
}

func TestDeliveryRequiresInstructionsAndHonorsCancellation(t *testing.T) {
	s := newTestService(t, nil)
	options := s.options
	options.Transport = newFakeTransport()
	if _, err := NewService(&options); err == nil {
		t.Fatal("transport accepted without member instructions")
	}
	transportOptions(newFakeTransport())(&options)
	s, err := NewService(&options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Deliver(ctx); err == nil {
		t.Fatal("canceled delivery accepted")
	}
}
