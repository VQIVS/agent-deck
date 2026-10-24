package mcp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestAttachmentNotifyOwnerOnlyAndValidated(t *testing.T) {
	f := newRuntimeFixture(t)
	owner := startTestRuntime(t, f)
	connection, err := OpenRuntime(t.Context(), &RuntimeOptions{
		Provider: f.provider, JournalPath: owner.options.JournalPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	before, _ := owner.ReadUser(t.Context())
	notices := []team.TaskNotice{{To: "bob", Task: "task-1", Text: "Saved task is ready"}}
	if err := connection.Notify(t.Context(), notices); err != nil {
		t.Fatal(err)
	}
	after, _ := owner.ReadUser(t.Context())
	if len(after) != len(before)+1 || after[len(after)-1].Sender != "agent-deck" || !after[len(after)-1].Private {
		t.Fatal("notice was not privately queued", after)
	}
	for _, invalid := range [][]team.TaskNotice{
		nil, make([]team.TaskNotice, maxTaskNotices+1),
		{{To: "missing", Task: "task", Text: "no"}},
		{{To: "bob", Task: "task", Text: strings.Repeat("x", owner.Service.options.MaxBytes+1)}},
	} {
		if err := connection.Notify(t.Context(), invalid); err == nil {
			t.Fatal("invalid notices accepted")
		}
	}
	memberToken, _ := owner.Tokens.Issue(Identity{Team: f.manager.Team.Name, Member: "bob"}, true)
	for _, token := range []string{"forged", memberToken} {
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost,
			strings.TrimSuffix(owner.Endpoint(), "/mcp")+"/user", strings.NewReader(
				`{"action":"notify","notices":[{"To":"bob","Task":"task","Text":"forged"}]}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatal("member token authorized privileged notices")
		}
	}
	connection.readOnly = true
	if err := connection.Notify(t.Context(), notices); err == nil {
		t.Fatal("read-only attachment posted notices")
	}
	connection.readOnly = false
	f.mu.Lock()
	f.manager.ReadOnly = true
	f.mu.Unlock()
	if err := connection.Notify(t.Context(), notices); err == nil {
		t.Fatal("fresh owner read-only policy bypassed")
	}
}
