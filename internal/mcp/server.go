package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Server provides stateless MCP streamable HTTP with bearer authentication on every request.
type Server struct {
	Tokens  *Tokens
	Service *Service
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.IsLoopback() {
		return fmt.Errorf("MCP listener must bind an explicit loopback address")
	}
	server := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second}
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stop()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve team MCP: %w", err)
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !localRequest(r) || r.Header.Get("Origin") != "" {
		http.Error(w, "loopback clients only", http.StatusForbidden)
		return
	}
	identity, ok := s.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "invalid member credential", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
		return
	}
	var call request
	if err := decode(http.MaxBytesReader(w, r.Body, 64*1024), &call); err != nil {
		writeError(w, nil, -32700, "invalid JSON request")
		return
	}
	if call.JSONRPC != "2.0" || call.Method == "" {
		writeError(w, call.ID, -32600, "invalid JSON-RPC request")
		return
	}
	if len(call.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	s.dispatch(w, r, identity, &call)
}

func localRequest(r *http.Request) bool {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() {
		return false
	}
	host := r.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func (s *Server) authenticate(r *http.Request) (Identity, bool) {
	if s.Tokens == nil || s.Service == nil {
		return Identity{}, false
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return Identity{}, false
	}
	identity, ok := s.Tokens.Authenticate(strings.TrimPrefix(header, "Bearer "))
	if !ok {
		return Identity{}, false
	}
	_, err := s.Service.member(identity)
	return identity, err == nil
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, identity Identity, call *request) {
	switch call.Method {
	case "initialize":
		s.initialize(w, call)
	case "ping":
		writeResult(w, call.ID, struct{}{})
	case "tools/list":
		writeResult(w, call.ID, map[string]any{"tools": tools()})
	case "tools/call":
		result, err := s.call(r.Context(), identity, call.Params)
		if err != nil {
			writeResult(w, call.ID, toolResult(err.Error(), true))
			return
		}
		b, err := json.Marshal(result)
		if err != nil {
			writeError(w, call.ID, -32603, "encode tool result")
			return
		}
		writeResult(w, call.ID, toolResult(string(b), false))
	default:
		writeError(w, call.ID, -32601, "method not found")
	}
}

func (s *Server) initialize(w http.ResponseWriter, call *request) {
	var params struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ClientInfo      map[string]any `json:"clientInfo"`
	}
	if err := decode(bytes.NewReader(call.Params), &params); err != nil {
		writeError(w, call.ID, -32602, "invalid initialize parameters")
		return
	}
	version := params.ProtocolVersion
	if version != "2025-03-26" && version != "2025-06-18" && version != "2025-11-25" {
		version = "2025-03-26"
	}
	writeResult(w, call.ID, map[string]any{
		"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]string{"name": "agent-deck", "version": "0.1.0"},
	})
}

func decode(reader io.Reader, value any) error {
	d := json.NewDecoder(reader)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request must contain exactly one JSON value")
	}
	return nil
}

func toolResult(text string, failed bool) map[string]any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": failed}
}

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, map[string]any{
		"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message},
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
