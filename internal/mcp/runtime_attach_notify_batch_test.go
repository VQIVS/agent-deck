package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestOwnerNotifyBatchKeepsGoodRecipientsAndTruncatesUTF8(t *testing.T) {
	f := newRuntimeFixture(t)
	f.manager.Team.Members = append(f.manager.Team.Members, team.Member{
		ID: "muted-id", Name: "MutedMaster", Role: "master", Muted: true, Mode: "shared", Dir: f.manager.Team.Repo,
	})
	r := startTestRuntime(t, f)
	before, _ := r.ReadUser(t.Context())
	err := r.notifyUser(t.Context(), []team.TaskNotice{
		{To: "MutedMaster", Task: "task", Text: "skip muted"},
		{To: "bob", Task: "task", Text: strings.Repeat("界", r.Service.options.MaxBytes)},
	})
	if err == nil || !strings.Contains(err.Error(), "MutedMaster") || !strings.Contains(err.Error(), "truncated") {
		t.Fatal("partial failures not reported", err)
	}
	after, _ := r.ReadUser(t.Context())
	if len(after) != len(before)+1 {
		t.Fatal("valid notice dropped with muted peer")
	}
	last := after[len(after)-1]
	if !utf8.ValidString(last.Text) || len(last.Text) > r.Service.options.MaxBytes || last.Recipients[0] != "bob" {
		t.Fatal("oversized notice not safely queued", len(last.Text))
	}
}
