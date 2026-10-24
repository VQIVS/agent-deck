package mcp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vqivs/agent-deck/internal/channel"
)

type fakeTransport struct {
	busy      map[string]bool
	prompts   map[string][]string
	fail      bool
	afterSend func()
}

func (f *fakeTransport) Busy(_ context.Context, member string) (bool, error) {
	return f.busy[member], nil
}

func (f *fakeTransport) Send(_ context.Context, member, prompt string) error {
	f.prompts[member] = append(f.prompts[member], prompt)
	if f.afterSend != nil {
		f.afterSend()
	}
	if f.fail {
		return errors.New("ambiguous submission failure")
	}
	return nil
}

func transportOptions(f *fakeTransport) func(*Options) {
	return func(o *Options) {
		o.Transport = f
		o.Instructions = func(_ context.Context, member string) (string, error) {
			return "rules/system/model roster tools for " + member, nil
		}
	}
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{busy: make(map[string]bool), prompts: make(map[string][]string)}
}

func TestDeliveryBusyThenIdleAndReadAck(t *testing.T) {
	f := newFakeTransport()
	f.busy["bob"] = true
	s := newTestService(t, transportOptions(f))
	ctx := t.Context()
	alice, bob := Identity{"test", "alice"}, Identity{"test", "bob"}
	if _, err := s.Post(ctx, alice, Post{Text: "ordinary"}, false); err != nil {
		t.Fatal(err)
	}
	message, err := s.Post(ctx, alice, Post{Text: "wake", Mentions: []string{"bob"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(ctx); err != nil || len(f.prompts) != 0 {
		t.Fatalf("busy interrupted: %v, %v", f.prompts, err)
	}
	f.busy["bob"] = false
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if err := s.Deliver(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(f.prompts["bob"]) != 1 || len(f.prompts) != 1 {
		t.Fatalf("duplicate/unmentioned wake: %v", f.prompts)
	}
	prompt := f.prompts["bob"][0]
	if !strings.Contains(prompt, message.ID) || !strings.Contains(prompt, "rules/system/model roster tools for bob") {
		t.Fatal("missing dedup id or instructions", prompt)
	}
	queued, err := s.Post(ctx, alice, Post{Text: "read first", Member: "bob"}, true)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		records, err := s.Read(ctx, bob, message.ID)
		if err != nil || len(records) != 1 || records[0].ID != queued.ID {
			t.Fatalf("read returned events/duplicates: %v, %v", records, err)
		}
	}
	if err := s.Deliver(ctx); err != nil || len(f.prompts["bob"]) != 1 {
		t.Fatalf("acknowledged message woke recipient: %v, %v", f.prompts, err)
	}
	records, _ := s.options.Journal.Snapshot(ctx)
	acks := 0
	for i := range records {
		if records[i].Kind == "ack" && records[i].Message == queued.ID {
			acks++
		}
	}
	if acks != 1 {
		t.Fatalf("ack events: %d", acks)
	}
}

func TestDeliveryCrashReplayRetainsIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	j, err := channel.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeTransport()
	f.fail = true
	options := Options{Team: "test", Journal: j, Members: func() []Member {
		return []Member{{ID: "alice-1", Name: "alice", Role: "master"}, {ID: "bob-1", Name: "bob", Role: "worker"}}
	}}
	transportOptions(f)(&options)
	s, err := NewService(&options)
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.Post(t.Context(), Identity{"test", "alice"}, Post{Text: "secret", Member: "bob"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(t.Context()); err == nil {
		t.Fatal("submission failure lost")
	}
	j.Close()
	j, err = channel.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	options.Journal = j
	s, err = NewService(&options)
	if err != nil {
		t.Fatal(err)
	}
	f.fail = false
	if err := s.Deliver(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts["bob"]) != 2 {
		t.Fatal("ambiguous submission not replayed")
	}
	for _, prompt := range f.prompts["bob"] {
		if !strings.Contains(prompt, message.ID) {
			t.Fatal("retry changed message ID")
		}
	}
	j.Close()
	j, err = channel.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	options.Journal = j
	s, _ = NewService(&options)
	if err := s.Deliver(t.Context()); err != nil || len(f.prompts["bob"]) != 2 {
		t.Fatalf("delivered state lost across restart: %v", err)
	}
}

func TestRosterQueueAndDeliveryPolicy(t *testing.T) {
	f := newFakeTransport()
	f.busy["bob"] = true
	readOnly, muted := false, false
	s := newTestService(t, func(o *Options) {
		transportOptions(f)(o)
		o.ReadOnly = func() bool { return readOnly }
		o.Members = func() []Member {
			return []Member{{Name: "alice", Role: "master"}, {Name: "bob", Role: "worker", Muted: muted}}
		}
	})
	r, err := s.BroadcastRoster(t.Context(), "current roster")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(t.Context()); err != nil || len(f.prompts["alice"]) != 0 || len(f.prompts["bob"]) != 0 {
		t.Fatalf("roster woke members: %v, %v", f.prompts, err)
	}
	records, err := s.Read(t.Context(), Identity{"test", "alice"}, "")
	if err != nil || len(records) == 0 || records[len(records)-1].ID != r.ID {
		t.Fatalf("roster not readable: %v %v", records, err)
	}
	muted, readOnly = false, true
	if _, err := s.BroadcastRoster(t.Context(), "blocked"); err == nil {
		t.Fatal("read-only roster allowed")
	}
}
