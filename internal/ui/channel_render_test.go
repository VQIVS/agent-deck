package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/channel"
	"github.com/vqivs/agent-deck/internal/team"
)

type failingChannel struct{}

func (failingChannel) ReadUser(context.Context) ([]channel.Record, error) { return nil, nil }
func (failingChannel) PostUser(context.Context, string, []string) error   { return errors.New("offline") }

func TestChannelFailedSendRetainsDraftAndBlocksDuplicate(t *testing.T) {
	m := requestModel("http://unused", false)
	m.EnableChannel(failingChannel{})
	m.channel.Draft = "keep this"
	cmd := m.postChannel()
	if cmd == nil || !m.channel.Sending || m.postChannel() != nil {
		t.Fatal("inflight state")
	}
	m.Update(cmd())
	if m.channel.Draft != "keep this" || !m.channel.Editing || m.channel.Sending {
		t.Fatal("failed send lost draft")
	}
	m.channelKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.channel.Draft != "keep this" || m.channel.Editing {
		t.Fatal("escape lost draft")
	}
}

func TestChannelMentionCompletionAndNewline(t *testing.T) {
	m := requestModel("http://unused", false)
	m.teams = &teamUI{Manager: &team.Manager{Team: team.Team{Members: []team.Member{
		{Name: "Master", Role: "master"}, {Name: "Worker", Role: "worker"},
	}}}}
	m.channel.Editing = true
	m.channel.Draft = "hello @wo"
	if choices := m.channelMentionChoices(); len(choices) != 1 || choices[0] != "@Worker" {
		t.Fatal(choices)
	}
	m.channelKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.channel.Draft != "hello @Worker " {
		t.Fatal("mention not completed", m.channel.Draft)
	}
	m.channelKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if !strings.HasSuffix(m.channel.Draft, "\n") {
		t.Fatal("Ctrl+J did not add newline")
	}
}

func TestChannelChatBoundedRosterAndClick(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w, m.h = 80, 24
	m.channel.Records = []channel.Record{
		{Kind: "roster", Sender: "agent-deck", Text: strings.Repeat("huge roster wall\n", 100)},
		{Kind: "message", Sender: "user", Text: "hello", Recipients: []string{"Master"}},
	}
	text := ansi.Strip(m.channelView())
	if strings.Contains(text, "huge roster wall") || !strings.Contains(text, "user [user]") {
		t.Fatal(text)
	}
	for _, line := range strings.Split(text, "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Fatal("overflow")
		}
	}
	if len(strings.Split(text, "\n")) > 24 {
		t.Fatal("height overflow")
	}
	if handled, _ := m.channelClick("channel-compose"); !handled || !m.channel.Editing {
		t.Fatal("compose click")
	}
	m.channelKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.channelKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m.channelKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.channel.Offset == 0 {
		t.Fatal("gg not oldest")
	}
	m.channelKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.channel.Offset != 0 {
		t.Fatal("G not newest")
	}
}

func TestChannelCardContextAndRosterExpansion(t *testing.T) {
	m := requestModel("http://unused", false)
	m.teams = &teamUI{Manager: &team.Manager{Team: team.Team{Members: []team.Member{{Name: "Lead", Role: "master"}}}}}
	record := channel.Record{
		Kind: "message", Sender: "Lead", Recipients: []string{"Worker"}, Private: true,
		Time: time.Date(2026, 10, 24, 9, 42, 0, 0, time.UTC), Text: "first line\nsecond line",
	}
	card := ansi.Strip(strings.Join(m.channelCard(&record, 80), "\n"))
	for _, want := range []string{"Lead [master]", "→ Worker", "09:42", "private", "first line\n  second line"} {
		if !strings.Contains(card, want) {
			t.Fatalf("missing %q in %s", want, card)
		}
	}
	record.Kind, record.Text = "roster", "full roster details"
	if strings.Contains(strings.Join(m.channelCard(&record, 80), "\n"), record.Text) {
		t.Fatal("roster not collapsed")
	}
	m.channelClick("channel-roster")
	if !strings.Contains(strings.Join(m.channelCard(&record, 80), "\n"), record.Text) {
		t.Fatal("roster not expanded")
	}
}

func TestChannelComposerKeepsNavigationAsText(t *testing.T) {
	m := requestModel("http://unused", false)
	m.channel.Open, m.workspace.View = true, "channel"
	m.channelKey(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "123456cgjkmoGr" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.channel.Draft != "123456cgjkmoGr" || !m.channel.Open || m.workspace.View != "channel" {
		t.Fatalf("composer intercepted input: %+v", m.channel)
	}
	m.channelKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.channelKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.channel.Draft != "123456cgjkmoGr" || !m.channel.Editing {
		t.Fatal("cancel/resume lost draft")
	}
}

func TestChannelReadOnlyAndBackActions(t *testing.T) {
	m := requestModel("http://unused", false)
	m.opts.ReadOnly = true
	m.channel.Open, m.workspace.View = true, "channel"
	m.channelClick("channel-compose")
	m.channelKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.channel.Editing {
		t.Fatal("read-only composer opened")
	}
	m.channel.Editing, m.channel.Draft = true, "saved"
	m.channelKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.channel.Draft != "saved" || m.channel.Editing {
		t.Fatal("stale read-only editor modified draft")
	}
	m.w, m.h = 80, 24
	m.zones = nil
	view := m.channelView()
	if !strings.Contains(view, "Read-only") {
		t.Fatal("read-only explanation missing")
	}
	for _, zone := range m.zones {
		if zone.Action == "channel-compose" {
			t.Fatal("disabled composer has mouse target")
		}
	}
	m.channelClick("channel-back")
	if m.channel.Open || m.workspace.View != "dashboard" || m.channel.Draft != "saved" {
		t.Fatal("back failed to preserve draft and return dashboard")
	}
}

func TestChannelOldestScrollCanReturnAndFooterFits(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w, m.h = 80, 16
	for i := 0; i < 30; i++ {
		m.channel.Records = append(m.channel.Records, channel.Record{Kind: "message", Sender: "user", Text: "hello"})
	}
	m.channel.Offset = 1 << 30
	m.channelView()
	offset := m.channel.Offset
	m.channelKey(tea.KeyMsg{Type: tea.KeyDown})
	m.channelView()
	if m.channel.Offset != offset-1 {
		t.Fatal("oldest scroll trapped at unbounded offset")
	}
	m.channel.Editing, m.channel.Status = true, "Send failed · draft preserved"
	m.channel.Draft = "first\nsecond\nthird"
	view := ansi.Strip(m.channelView())
	for _, want := range []string{"Compose", "third", "Send failed", "Esc back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("footer missing %q: %s", want, view)
		}
	}
	if len(strings.Split(view, "\n")) > m.h {
		t.Fatal("footer overflow")
	}
}

func TestChannelSendResultPreservesNewerDraft(t *testing.T) {
	m := requestModel("http://unused", false)
	m.EnableChannel(&fakeTeamChannel{})
	m.channel.Draft, m.channel.Editing = "submitted", true
	cmd := m.postChannel()
	m.channel.Draft = "newer draft"
	m.Update(cmd())
	if m.channel.Draft != "newer draft" || !m.channel.Editing || m.channel.Sending || m.channel.Status != "Sent" {
		t.Fatal("successful send overwrote newer draft or omitted feedback")
	}
}

func TestChannelMentionRequiresWordBoundary(t *testing.T) {
	m := requestModel("http://unused", false)
	m.teams = &teamUI{Manager: &team.Manager{Team: team.Team{Members: []team.Member{{Name: "Worker"}}}}}
	m.channel.Draft = "email@wo"
	if len(m.channelMentionChoices()) != 0 {
		t.Fatal("email offered member completions")
	}
	m.completeChannelMention()
	if m.channel.Draft != "email@wo" {
		t.Fatal("email rewritten")
	}
	m.channel.Draft = "hello\u2003@wo"
	m.completeChannelMention()
	if m.channel.Draft != "hello\u2003@Worker " {
		t.Fatal("unicode whitespace mention not completed")
	}
}

func TestChannelComposeMouseTargetAndKeyboardSend(t *testing.T) {
	m := requestModel("http://unused", false)
	m.w, m.h = 80, 24
	m.channel.Open, m.workspace.View = true, "channel"
	m.EnableChannel(&fakeTeamChannel{})
	m.channelView()
	found := false
	for _, target := range m.zones {
		if target.Action != "channel-compose" {
			continue
		}
		z, ok := hit(m.zones, target.X, target.Y)
		if !ok || z.Action != "channel-compose" {
			t.Fatal("compose hit target inaccessible")
		}
		m.channelClick(z.Action)
		found = true
		break
	}
	if !found || !m.channel.Editing {
		t.Fatal("compose button inaccessible")
	}
	m.channel.Draft = "hello team"
	cmd := m.channelKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.channel.Status != "Sending…" {
		t.Fatal("keyboard send missing inflight feedback")
	}
	m.Update(cmd())
	if m.channel.Draft != "" || m.channel.Editing || m.channel.Status != "Sent" {
		t.Fatal("send did not finish and clear submitted draft")
	}
}
