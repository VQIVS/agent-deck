package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qivs/agent-deck/internal/channel"
)

func TestImplicitReplyCannotResetHops(t *testing.T) {
	f := newFakeTransport()
	s := newTestService(t, transportOptions(f))
	alice := Identity{"test", "alice"}
	bob := Identity{"test", "bob"}
	r, err := s.Post(t.Context(), alice, Post{Text: "start", Member: "bob"}, true)
	if err != nil {
		t.Fatal(err)
	}
	r.Hops = 3
	// Seed a high-hop inbound message and its observed delivery.
	r.ID += "high"
	if err := s.options.Journal.Append(t.Context(), &r); err != nil {
		t.Fatal(err)
	}
	if err := s.event(t.Context(), "delivered", "bob", &r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Post(t.Context(), bob, Post{Text: "reset", Member: "eve"}, true); err == nil {
		t.Fatal("omitting reply_to reset delivered thread hop count")
	}
}

func TestWakeHopsClearAfterPostAndAllowMasterEscalation(t *testing.T) {
	f := newFakeTransport()
	s := newTestService(t, transportOptions(f))
	ctx := t.Context()
	alice, bob := Identity{"test", "alice"}, Identity{"test", "bob"}
	inbound, err := s.Post(ctx, alice, Post{Text: "wake", Member: "bob"}, true)
	if err != nil {
		t.Fatal(err)
	}
	inbound.ID += "high"
	inbound.Hops = 3
	if err := s.options.Journal.Append(ctx, &inbound); err != nil {
		t.Fatal(err)
	}
	if err := s.event(ctx, "delivered", "bob", &inbound); err != nil {
		t.Fatal(err)
	}
	escalation, err := s.Post(ctx, bob, Post{Text: "need help", Member: "alice"}, true)
	if err != nil || escalation.Hops != 4 {
		t.Fatalf("master escalation blocked: %+v, %v", escalation, err)
	}
	if !s.senderAllowed(&escalation) {
		t.Fatal("accepted master escalation cannot be delivered")
	}
	independent, err := s.Post(ctx, bob, Post{Text: "new work", Member: "eve"}, true)
	if err != nil || independent.Hops != 1 {
		t.Fatalf("old wake poisoned later post: %+v, %v", independent, err)
	}
	if err := s.event(ctx, "delivered", "bob", &inbound); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Post(ctx, bob, Post{Text: "new wake", Member: "eve"}, true); err == nil {
		t.Fatal("new wake did not restore hop guard")
	}
}

type selectiveTransport struct {
	sent []string
}

func (f *selectiveTransport) Busy(_ context.Context, name string) (bool, error) {
	if name == "bob" {
		return true, errors.New("offline")
	}
	return false, nil
}

func (f *selectiveTransport) Send(_ context.Context, name, _ string) error {
	f.sent = append(f.sent, name)
	return nil
}

func TestDeliveryErrorsDoNotStarveOtherMembers(t *testing.T) {
	f := &selectiveTransport{}
	s := newTestService(t, func(o *Options) {
		o.Transport = f
		o.Instructions = func(context.Context, string) (string, error) { return "rules", nil }
	})
	if _, err := s.Post(t.Context(), Identity{"test", "alice"}, Post{
		Text: "notify", Mentions: []string{"bob", "eve"},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(t.Context()); err == nil || len(f.sent) != 1 || f.sent[0] != "eve" {
		t.Fatalf("recipient error starved delivery: %v, %v", f.sent, err)
	}
}

func TestCancellationAfterSendPersistsDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := newFakeTransport()
	f.afterSend = cancel
	s := newTestService(t, transportOptions(f))
	if _, err := s.Post(ctx, Identity{"test", "alice"}, Post{Text: "send", Member: "bob"}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(ctx); err != nil {
		t.Fatal(err)
	}
	f.afterSend = nil
	if err := s.Deliver(t.Context()); err != nil || len(f.prompts["bob"]) != 1 {
		t.Fatalf("cancellation lost successful delivery: %v, %v", f.prompts, err)
	}
}

func TestSameNameRejoinCannotReadOldDM(t *testing.T) {
	bobID := "bob-1"
	s := newTestService(t, func(o *Options) {
		o.Members = func() []Member {
			return []Member{{ID: "alice-1", Name: "alice"}, {ID: bobID, Name: "bob"}}
		}
	})
	if _, err := s.Post(t.Context(), Identity{"test", "alice"}, Post{Text: "private", Member: "bob"}, true); err != nil {
		t.Fatal(err)
	}
	bobID = "bob-2"
	records, err := s.Read(t.Context(), Identity{"test", "bob"}, "")
	if err != nil || len(records) != 0 {
		t.Fatalf("rejoined member saw old DM: %v, %v", records, err)
	}
}

func TestReadBoundAndReservedNames(t *testing.T) {
	s := newTestService(t, nil)
	for i := range 105 {
		r := channel.Record{ID: strings.Repeat("x", i+1), Kind: "message", Sender: "alice", Text: "public"}
		if err := s.options.Journal.Append(t.Context(), &r); err != nil {
			t.Fatal(err)
		}
	}
	records, err := s.Read(t.Context(), Identity{"test", "bob"}, "")
	if err != nil || len(records) != 100 {
		t.Fatalf("unbounded read: %d, %v", len(records), err)
	}
	next, err := s.Read(t.Context(), Identity{"test", "bob"}, records[99].ID)
	if err != nil || len(next) != 5 {
		t.Fatalf("bounded cursor lost data: %d, %v", len(next), err)
	}
	o := s.options
	o.Members = func() []Member { return []Member{{ID: "reserved", Name: "agent-deck"}} }
	if _, err := NewService(&o); err == nil {
		t.Fatal("reserved sender accepted")
	}
}

func TestServiceDoesNotHoldMutexDuringTransport(t *testing.T) {
	f := newFakeTransport()
	s := newTestService(t, transportOptions(f))
	f.afterSend = func() {
		if _, err := s.Read(t.Context(), Identity{"test", "bob"}, ""); err != nil {
			t.Error(err)
		}
	}
	if _, err := s.Post(t.Context(), Identity{"test", "alice"}, Post{Text: "send", Member: "bob"}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestClientMetadataAndCaseInsensitiveHeaders(t *testing.T) {
	s := &Server{Tokens: NewTokens(), Service: newTestService(t, nil)}
	token, _ := s.Tokens.Issue(Identity{"test", "alice"}, true)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
		`"params":{"name":"team_members","arguments":{},"_meta":{"progressToken":1}}}`
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "bEaReR "+token)
	r.Header.Set("Content-Type", "Application/JSON; charset=utf-8")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"isError":false`) {
		t.Fatalf("real-client compatibility: %d %s", w.Code, w.Body)
	}
}
