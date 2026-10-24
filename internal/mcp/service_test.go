package mcp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/channel"
)

func newTestService(t *testing.T, configure func(*Options)) *Service {
	t.Helper()
	j, err := channel.Open(filepath.Join(t.TempDir(), "channel"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	options := Options{Team: "test", Journal: j, Members: func() []Member {
		return []Member{{Name: "alice", Role: "master"}, {Name: "bob", Role: "worker"}, {Name: "eve"}}
	}}
	if configure != nil {
		configure(&options)
	}
	members := options.Members
	options.Members = func() []Member {
		roster := append([]Member(nil), members()...)
		for i := range roster {
			if roster[i].ID == "" {
				roster[i].ID = roster[i].Name + "-membership-1"
			}
		}
		return roster
	}
	s, err := NewService(&options)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTokenRotationAndIsolation(t *testing.T) {
	tokens := NewTokens()
	identity := Identity{Team: "test", Member: "alice"}
	if _, err := tokens.Issue(identity, false); err == nil {
		t.Fatal("shared directory accepted")
	}
	first, err := tokens.Issue(identity, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tokens.Issue(identity, true)
	if err != nil || second == first {
		t.Fatalf("rotation: %v", err)
	}
	if _, ok := tokens.Authenticate(first); ok {
		t.Fatal("old credential remains valid")
	}
	if got, ok := tokens.Authenticate(second); !ok || got != identity {
		t.Fatalf("identity: %+v, %v", got, ok)
	}
	tokens.Revoke(identity)
	if _, ok := tokens.Authenticate(second); ok {
		t.Fatal("revoked credential remains valid")
	}
}

func TestDMVisibilityAndReplyPrivacy(t *testing.T) {
	s := newTestService(t, nil)
	ctx := context.Background()
	alice, bob, eve := Identity{"test", "alice"}, Identity{"test", "bob"}, Identity{"test", "eve"}
	message, err := s.Post(ctx, alice, Post{Text: "secret", Member: "bob"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if records, err := s.Read(ctx, eve, ""); err != nil || len(records) != 0 {
		t.Fatalf("DM leaked: %v, %v", records, err)
	}
	for _, post := range []Post{
		{Text: "leak", ReplyTo: message.ID},
		{Text: "leak", Member: "eve", ReplyTo: message.ID},
	} {
		if _, err := s.Post(ctx, bob, post, post.Member != ""); err == nil {
			t.Fatal("private thread escaped")
		}
	}
	if _, err := s.Post(ctx, bob, Post{Text: "reply", Member: "alice", ReplyTo: message.ID}, true); err != nil {
		t.Fatal(err)
	}
	first, _ := s.Read(ctx, bob, "")
	again, _ := s.Read(ctx, bob, "")
	if len(first) != 2 || len(again) != 2 {
		t.Fatal("repeated read changed journal")
	}
	if _, err := s.Post(ctx, eve, Post{Text: "forge", ReplyTo: message.ID}, false); err == nil {
		t.Fatal("unseen parent accepted")
	}
}

func TestPostingGuardsAndCurrentRole(t *testing.T) {
	now := time.Unix(1000, 0)
	readOnly := false
	roster := []Member{{Name: "alice", Role: "worker"}, {Name: "bob", Role: "worker"}}
	s := newTestService(t, func(o *Options) {
		o.Now = func() time.Time { return now }
		o.ReadOnly = func() bool { return readOnly }
		o.Members = func() []Member { return roster }
		o.Rate, o.MaxBytes = 3, 10
	})
	ctx := context.Background()
	alice := Identity{"test", "alice"}
	for _, post := range []Post{{Text: " "}, {Text: "too long message"}, {Text: "hi", Mentions: []string{"unknown"}}} {
		if _, err := s.Post(ctx, alice, post, false); err == nil {
			t.Fatalf("accepted invalid post: %+v", post)
		}
	}
	var parent string
	for range 3 {
		r, err := s.Post(ctx, alice, Post{Text: "hi", ReplyTo: parent}, false)
		if err != nil {
			t.Fatal(err)
		}
		parent = r.ID
	}
	if _, err := s.Post(ctx, alice, Post{Text: "limit"}, false); err == nil {
		t.Fatal("rate limit not enforced")
	}
	now = now.Add(time.Minute)
	if _, err := s.Post(ctx, alice, Post{Text: "hop", ReplyTo: parent}, false); err == nil {
		t.Fatal("hop limit not enforced")
	}
	roster[0].Role = "master"
	if _, err := s.Post(ctx, alice, Post{Text: "hop", ReplyTo: parent}, false); err != nil {
		t.Fatal(err)
	}
	roster[0].Muted = true
	if _, err := s.Post(ctx, alice, Post{Text: "muted"}, false); err == nil {
		t.Fatal("mute not enforced")
	}
	roster[0].Muted = false
	readOnly = true
	if _, err := s.Post(ctx, alice, Post{Text: "readonly"}, false); err == nil {
		t.Fatal("read-only not enforced")
	}
	readOnly = false
	roster = roster[1:]
	if _, err := s.Read(ctx, alice, ""); err == nil {
		t.Fatal("removed member retained access")
	}
	if _, err := s.Read(ctx, Identity{"other", "bob"}, ""); err == nil {
		t.Fatal("cross-team access accepted")
	}
}
