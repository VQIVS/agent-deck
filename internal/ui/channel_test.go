package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/channel"
)

type fakeTeamChannel struct{ text string }

func (f *fakeTeamChannel) ReadUser(context.Context) ([]channel.Record, error) {
	return []channel.Record{{Kind: "message", Sender: "user", Text: f.text}}, nil
}

func (f *fakeTeamChannel) PostUser(_ context.Context, text string, _ []string) error {
	f.text = text
	return nil
}

func TestChannelUserPostAndReadOnly(t *testing.T) {
	m := requestModel("http://unused", false)
	runtime := &fakeTeamChannel{}
	m.EnableChannel(runtime)
	m.channel.Draft = "hello"
	cmd := m.postChannel()
	if cmd == nil {
		t.Fatal("no post")
	}
	m.Update(cmd())
	if !strings.Contains(m.channelView(), "hello") {
		t.Fatal("missing user message")
	}
	m.opts.ReadOnly = true
	m.channel.Draft = "blocked"
	if m.postChannel() != nil {
		t.Fatal("readonly post")
	}
}
