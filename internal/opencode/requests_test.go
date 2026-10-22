package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPendingRequestsAndReplies(t *testing.T) {
	posts := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != "/tmp/team" {
			t.Error("missing directory scope")
		}
		switch r.URL.Path {
		case "/permission":
			fmt.Fprint(w, `[{"id":"per_a","sessionID":"session-a","permission":"bash","patterns":["go test *"]}]`)
		case "/question":
			fmt.Fprint(
				w,
				`[{"id":"que_a","sessionID":"session-a","questions":[{"header":"Target","question":"Which target?",
"options":[{"label":"Local","description":"This machine"}]}]}]`,
			)
		case "/permission/per_a/reply":
			posts++
			if r.Method != "POST" {
				t.Error(r.Method)
			}
			var body struct{ Reply, Message string }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Reply == "reject" && body.Message != "use local tests" {
				t.Error(body)
			}
			fmt.Fprint(w, "true")
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	c := New(s.URL, false)
	ctx := context.Background()
	p, e := c.Permissions(ctx, "/tmp/team")
	if e != nil || len(p) != 1 || p[0].Patterns[0] != "go test *" {
		t.Fatalf("%+v %v", p, e)
	}
	q, e := c.Questions(ctx, "/tmp/team")
	if e != nil || len(q) != 1 || q[0].Questions[0].Question != "Which target?" {
		t.Fatalf("%+v %v", q, e)
	}
	for _, reply := range []string{"once", "always", "reject"} {
		if e = c.ReplyPermission(ctx, "/tmp/team", "per_a", reply, "use local tests"); e != nil {
			t.Fatal(e)
		}
	}
	c.ReadOnly = true
	for _, reply := range []string{"once", "always", "reject"} {
		if c.ReplyPermission(ctx, "/tmp/team", "per_a", reply, "") == nil {
			t.Fatal("read-only reply allowed")
		}
	}
	if posts != 3 {
		t.Fatal(posts)
	}
}
