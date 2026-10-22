package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientAndDiscovery(t *testing.T) {
	var posts atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("directory")
		if r.URL.Path != "/project" && dir != "/tmp/project" && dir != "/tmp/child" {
			t.Errorf("unscoped request %s", r.URL)
		}
		switch r.URL.Path {
		case "/project":
			fmt.Fprint(w, `[{"worktree":"/tmp/project","sandboxes":[]}]`)
		case "/session":
			if r.Method == "POST" {
				posts.Add(1)
				fmt.Fprint(w, `{"id":"session-new"}`)
			} else {
				fmt.Fprint(w, `[{"id":"session-a","directory":"/tmp/project","title":"Lead"},
{"id":"session-child","directory":"/tmp/child","parentID":"session-a"}]`)
			}
		case "/session/status":
			fmt.Fprint(w, `{"session-child":{"type":"busy"}}`)
		case "/session/session-a/message":
			if r.URL.Query().Get("limit") != "30" {
				t.Error("no message limit")
			}
			fmt.Fprint(w, `[{"info":{"id":"msg_1","role":"assistant"},"parts":[]}]`)
		case "/session/session-a/prompt_async":
			posts.Add(1)
			var v struct{ Parts []struct{ Text string } }
			if e := json.NewDecoder(r.Body).Decode(&v); e != nil || len(v.Parts) != 1 || v.Parts[0].Text != "hello" {
				t.Error("bad prompt")
			}
			w.WriteHeader(204)
		case "/session/session-a/abort":
			posts.Add(1)
			fmt.Fprint(w, "true")
		default:
			http.Error(w, "missing", 404)
		}
	}))
	defer s.Close()
	ctx := context.Background()
	c := New(s.URL, false)
	if _, e := c.Messages(ctx, "/tmp/project", "session-a"); e != nil {
		t.Fatal(e)
	}
	if e := c.Prompt(ctx, "/tmp/project", "session-a", "hello"); e != nil {
		t.Fatal(e)
	}
	if e := c.Abort(ctx, "/tmp/project", "session-a"); e != nil {
		t.Fatal(e)
	}
	v, e := c.Discover(ctx)
	if e != nil || len(v.Dirs) != 2 || len(v.Sessions) != 2 || !v.Active["/tmp/child\x00session-child"] {
		t.Fatalf("%+v %v", v, e)
	}
	c.ReadOnly = true
	if e = c.Prompt(ctx, "/tmp/project", "session-a", "hello"); e == nil {
		t.Fatal("read-only prompt")
	}
	if e = c.Abort(ctx, "/tmp/project", "session-a"); e == nil {
		t.Fatal("read-only abort")
	}
	if posts.Load() != 2 {
		t.Fatal("unsafe requests")
	}
	c.ReadOnly = false
	if _, e = c.Messages(ctx, "/tmp/project", "session-missing"); e == nil {
		t.Fatal("ignored 404")
	}
}

func TestSSEAndReconnect(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != "/tmp/project" {
			t.Error("unscoped SSE")
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(
			w,
			": heartbeat\n\ndata: invalid\n\ndata: {"+"\n"+"data: \"type\":\"future.event\",\"properties\":{}}\n\n",
		)
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	c := New(s.URL, true)
	count := 0
	c.Stream(ctx, "/tmp/project", func(e Event) {
		count++
		if count == 2 {
			cancel()
		}
	}, func(error) {})
	if count != 2 || calls.Load() < 2 {
		t.Fatalf("events=%d requests=%d", count, calls.Load())
	}
	var got []Event
	_ = DecodeSSE(strings.NewReader("data: {\"type\":\"tail\"}"), func(e Event) { got = append(got, e) })
	if len(got) != 1 {
		t.Fatal("lost final frame")
	}
}
