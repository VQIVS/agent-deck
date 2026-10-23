package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a directory-scoped OpenCode server and enforces read-only requests.
type Client struct {
	Base     string
	HTTP     *http.Client
	ReadOnly bool
}

func New(base string, readOnly bool) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 20 * time.Second
	return &Client{
		strings.TrimRight(base, "/"),
		&http.Client{Timeout: 20 * time.Second, Transport: transport},
		readOnly,
	}
}

// Session identifies a conversation and its owning directory.
type Session struct {
	Time struct {
		Updated int64 `json:"updated"`
	} `json:"time"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	ParentID  string `json:"parentID"`
}

// Project lists a repository worktree and its sandbox directories.
type Project struct {
	Worktree  string   `json:"worktree"`
	Sandboxes []string `json:"sandboxes"`
}

// Status is the server's current session activity.
type Status struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Part is a text, reasoning, or tool fragment within a message.
type Part struct {
	Role      string `json:"-"`
	Created   int64  `json:"-"`
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	Tool      string `json:"tool"`
	State     struct {
		Status   string                     `json:"status"`
		Input    json.RawMessage            `json:"input"`
		Output   string                     `json:"output"`
		Error    string                     `json:"error"`
		Metadata json.RawMessage            `json:"metadata"`
		Time     struct{ Start, End int64 } `json:"time"`
	} `json:"state"`
}

// Message groups ordered parts with speaker and timestamp metadata.
type Message struct {
	Info struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		Time struct {
			Created   int64 `json:"created"`
			Completed int64 `json:"completed"`
		} `json:"time"`
		Tokens struct {
			Input int64 `json:"input"`
			Cache struct {
				Read int64 `json:"read"`
			} `json:"cache"`
		} `json:"tokens"`
	} `json:"info"`
	Parts []Part `json:"parts"`
}

// Event carries an incremental SSE update with type-specific properties.
type Event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

func (c *Client) request(ctx context.Context, method, path, dir string, body, out any) error {
	if c.ReadOnly && method != http.MethodGet {
		return fmt.Errorf("read-only mode: %s blocked", method)
	}
	u, err := url.Parse(c.Base + path)
	if err != nil {
		return fmt.Errorf("parse request URL: %w", err)
	}
	q := u.Query()
	q.Set("directory", dir)
	u.RawQuery = q.Encode()
	var r io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return fmt.Errorf("encode %s request: %w", path, e)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return fmt.Errorf("create %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("%s %s: %s %s", method, path, res.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return nil
}

func (c *Client) Sessions(ctx context.Context, dir string) ([]Session, error) {
	var v []Session
	e := c.request(ctx, "GET", "/session", dir, nil, &v)
	return v, e
}

func (c *Client) Status(ctx context.Context, dir string) (map[string]Status, error) {
	var v map[string]Status
	e := c.request(ctx, "GET", "/session/status", dir, nil, &v)
	return v, e
}

func (c *Client) Messages(ctx context.Context, dir, id string) ([]Message, error) {
	var v []Message
	e := c.request(ctx, "GET", "/session/"+url.PathEscape(id)+"/message?limit=30", dir, nil, &v)
	return v, e
}

func (c *Client) Prompt(ctx context.Context, dir, id, text string) error {
	body := map[string]any{"parts": []map[string]string{{"type": "text", "text": text}}}
	return c.request(ctx, "POST", "/session/"+url.PathEscape(id)+"/prompt_async", dir, body, nil)
}

func (c *Client) Abort(ctx context.Context, dir, id string) error {
	return c.request(ctx, "POST", "/session/"+url.PathEscape(id)+"/abort", dir, nil, nil)
}

func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var v []Project
	e := c.request(ctx, "GET", "/project", "", nil, &v)
	return v, e
}

func (c *Client) Create(ctx context.Context, dir, title string) (Session, error) {
	var s Session
	e := c.request(ctx, "POST", "/session", dir, map[string]string{"title": title}, &s)
	return s, e
}

func (c *Client) Delete(ctx context.Context, dir, id string) error {
	return c.request(ctx, "DELETE", "/session/"+url.PathEscape(id), dir, nil, nil)
}

// Stream reconnects with bounded backoff. A connection notification precedes events
// so consumers can refresh a snapshot after reconnecting. Every stream is scoped.
func (c *Client) Stream(ctx context.Context, dir string, event func(Event), connection func(error)) {
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := c.streamOnce(ctx, dir, event, connection)
		if ctx.Err() != nil {
			return
		}
		connection(err)
		if time.Since(started) > 30*time.Second {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 15*time.Second)
	}
}

func (c *Client) streamOnce(ctx context.Context, dir string, event func(Event), connection func(error)) error {
	u, err := url.Parse(c.Base + "/event")
	if err != nil {
		return fmt.Errorf("parse stream URL: %w", err)
	}
	q := u.Query()
	q.Set("directory", dir)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), http.NoBody)
	if err != nil {
		return fmt.Errorf("create stream request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	// No whole-request timeout on a long-lived SSE response; cancel it with ctx.
	h := &http.Client{Transport: c.HTTP.Transport}
	res, err := h.Do(req)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		return fmt.Errorf("event stream: %s", res.Status)
	}
	connection(nil)
	return DecodeSSE(res.Body, event)
}

func DecodeSSE(r io.Reader, consume func(Event)) error {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 4<<20)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		var e Event
		if json.Unmarshal([]byte(strings.Join(data, "\n")), &e) == nil && e.Type != "" {
			consume(e)
		}
		data = nil
	}
	for s.Scan() {
		line := s.Text()
		if line == "" {
			flush()
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
	if err := s.Err(); err != nil {
		return fmt.Errorf("scan SSE: %w", err)
	}
	return io.EOF
}
