package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPToolsAndAuthentication(t *testing.T) {
	tokens := NewTokens()
	token, err := tokens.Issue(Identity{"test", "alice"}, true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Tokens: tokens, Service: newTestService(t, nil)}
	request := func(body, credential string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{"initialize", "ping", "tools/list"} {
		params := `{}`
		if method == "initialize" {
			params = `{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test"}}`
		}
		w := request(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`, token)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"result"`) {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
	}
	post := `{"jsonrpc":"2.0","id":2,"method":"tools/call",` +
		`"params":{"name":"channel_post","arguments":{"text":"hello","mentions":["bob"]}}}`
	if w := request(post, "forged"); w.Code != http.StatusUnauthorized {
		t.Fatalf("forged token: %d", w.Code)
	}
	if w := request(post, token); !strings.Contains(w.Body.String(), `"isError":false`) {
		t.Fatal(w.Body)
	}
	forged := strings.Replace(post, `"text":"hello"`, `"text":"hello","sender":"bob"`, 1)
	if w := request(forged, token); !strings.Contains(w.Body.String(), `"isError":true`) {
		t.Fatal("caller identity accepted", w.Body)
	}
	read := `{"jsonrpc":"2.0","id":3,"method":"tools/call",` +
		`"params":{"name":"channel_read","arguments":{}}}`
	var response map[string]any
	w := request(read, token)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response["result"] == nil {
		t.Fatalf("read: %s, %v", w.Body, err)
	}
	if w := request(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, token); w.Code != 202 {
		t.Fatalf("notification: %d", w.Code)
	}
	tokens.Revoke(Identity{"test", "alice"})
	if w := request(read, token); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", w.Code)
	}
}

func TestTransportBoundaries(t *testing.T) {
	s := &Server{Tokens: NewTokens(), Service: newTestService(t, nil)}
	token, _ := s.Tokens.Issue(Identity{"test", "alice"}, true)
	for _, tc := range []struct {
		name, host, peer, origin, method, content, body string
		status                                          int
	}{
		{"remote", "127.0.0.1", "192.0.2.1:1234", "", "POST", "application/json", `{}`, 403},
		{"rebind", "evil.example", "127.0.0.1:1234", "", "POST", "application/json", `{}`, 403},
		{"origin", "localhost", "127.0.0.1:1234", "http://evil.example", "POST", "application/json", `{}`, 403},
		{"get", "localhost", "127.0.0.1:1234", "", "GET", "application/json", `{}`, 405},
		{"content", "localhost", "127.0.0.1:1234", "", "POST", "text/plain", `{}`, 415},
		{"malformed", "localhost", "127.0.0.1:1234", "", "POST", "application/json", `{`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), tc.method, "http://"+tc.host+"/mcp", strings.NewReader(tc.body))
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.content)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status: %d, %s", w.Code, w.Body)
			}
		})
	}
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := s.Serve(context.Background(), listener); err == nil {
		t.Fatal("wildcard listener accepted")
	}
}
