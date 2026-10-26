package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestWorkerRequestRootsIsolateFailuresAndPreserveOnceOnlyRouting(t *testing.T) {
	for _, failedRoot := range []string{"", "first", "second"} {
		t.Run("failed-"+failedRoot, func(t *testing.T) {
			fail := failedRoot
			var replyDirectory string
			directories := map[string]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				dir := r.URL.Query().Get("directory")
				root := ""
				switch dir {
				case "/work/runtime/first.controls/worker":
					root = "first"
				case "/work/runtime/second.controls/worker":
					root = "second"
				default:
					t.Errorf("request used code checkout or another member directory: %q", dir)
				}
				if strings.HasSuffix(r.URL.Path, "/reply") {
					replyDirectory = dir
					w.WriteHeader(http.StatusNoContent)
					return
				}
				directories[root]++
				if root == fail {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				fmt.Fprintf(w, `[{"id":"request-%s","sessionID":"worker-%s","permission":"external_directory",`+
					`"patterns":["/work/project"]},{"id":"foreign","sessionID":"other","permission":"edit"}]`, root, root)
			}))
			defer server.Close()
			manager := &team.Manager{Team: team.Team{Name: "test", Server: server.URL, Members: []team.Member{
				{Name: "lead", Role: "master", Session: "lead", Dir: "/work/project"},
				{
					Name: "first", Role: "worker", Session: "worker-first", Dir: "/work/project",
					ControlDir: "/work/runtime/first.controls/worker",
				},
				{
					Name: "second", Role: "worker", Session: "worker-second", Dir: "/work/project",
					ControlDir: "/work/runtime/second.controls/worker",
				},
			}}}
			transport := newFakeTransport()
			service := newTestService(t, func(o *Options) {
				transportOptions(transport)(o)
				o.Members = func() []Member {
					return []Member{
						{Name: "lead", Role: "master"},
						{Name: "first", Role: "worker"},
						{Name: "second", Role: "worker"},
					}
				}
			})
			runtime := &Runtime{Service: service}
			for range 2 {
				err := runtime.routeWorkerRequests(t.Context(), manager)
				if (err != nil) != (fail != "") {
					t.Fatal("directory failure diagnostic lost", err)
				}
				if err := service.Deliver(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			tools := &TaskTools{Provider: func(context.Context) (*team.Manager, team.BoardStore, error) {
				return manager, team.BoardStore{}, nil
			}}
			healthy := "first"
			if fail == "first" {
				healthy = "second"
			}
			_, err := tools.answerRequest(t.Context(), Identity{Team: "test", Member: "lead"},
				json.RawMessage(fmt.Sprintf(`{"id":"request-%s","reply":"once"}`, healthy)))
			if err != nil || replyDirectory != "/work/runtime/"+healthy+".controls/worker" {
				t.Fatal("healthy request could not be answered across failed root", replyDirectory, err)
			}
			fail = ""
			if err := runtime.routeWorkerRequests(t.Context(), manager); err != nil {
				t.Fatal(err)
			}
			if err := service.Deliver(t.Context()); err != nil {
				t.Fatal(err)
			}
			fail = "first"
			if err := runtime.routeWorkerRequests(t.Context(), manager); err == nil {
				t.Fatal("new root outage was hidden")
			}
			fail = ""
			if err := runtime.routeWorkerRequests(t.Context(), manager); err != nil {
				t.Fatal(err)
			}
			if err := service.Deliver(t.Context()); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(transport.prompts["lead"], "\n")
			for _, root := range []string{"first", "second"} {
				if strings.Count(joined, "Permission request request-"+root+" ") != 1 || directories[root] == 0 {
					t.Fatal("request lost or delivered repeatedly", root, transport.prompts)
				}
			}
			if strings.Contains(joined, "foreign") {
				t.Fatal("foreign session request forwarded")
			}
		})
	}
}
