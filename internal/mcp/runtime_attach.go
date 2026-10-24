package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/qivs/agent-deck/internal/channel"
)

// RuntimeConnection is a local owner or authenticated attachment. Closing an attachment leaves its owner running.
type RuntimeConnection struct {
	owner    *Runtime
	state    runtimeDiscovery
	client   *http.Client
	readOnly bool
}

type runtimeDiscovery struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Team     string `json:"team"`
}

type userRequest struct {
	Action   string   `json:"action"`
	Text     string   `json:"text,omitempty"`
	Mentions []string `json:"mentions,omitempty"`
	Member   string   `json:"member,omitempty"`
}

func (r *Runtime) publishDiscovery() error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("generate runtime owner credential: %w", err)
	}
	r.userToken = hex.EncodeToString(b)
	state := runtimeDiscovery{Endpoint: r.URL, Token: r.userToken, Team: r.manager.Team.Name}
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode runtime discovery: %w", err)
	}
	path := r.options.JournalPath + ".runtime.json"
	return writeDiscovery(path, encoded)
}

func writeDiscovery(path string, encoded []byte) error {
	config := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &config); err != nil {
		return fmt.Errorf("decode runtime discovery: %w", err)
	}
	_, original, err := loadRuntimeConfig(path)
	if err != nil {
		return err
	}
	return writeRuntimeConfig(path, config, original)
}

func (r *Runtime) removeDiscovery() {
	path := r.options.JournalPath + ".runtime.json"
	state, err := readDiscovery(path)
	if err == nil && state.Token == r.userToken {
		r.report(os.Remove(path))
	}
}

func readDiscovery(path string) (runtimeDiscovery, error) {
	var state runtimeDiscovery
	info, err := os.Lstat(path)
	if err != nil {
		return state, fmt.Errorf("inspect runtime discovery: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return state, fmt.Errorf("runtime discovery must be a private regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Getuid()) || info.Size() > 4096 {
		return state, fmt.Errorf("runtime discovery must belong to this user and fit 4096 bytes")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return state, fmt.Errorf("read runtime discovery: %w", err)
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return state, fmt.Errorf("decode runtime discovery: %w", err)
	}
	err = validateDiscovery(&state)
	return state, err
}

func validateDiscovery(state *runtimeDiscovery) error {
	u, err := url.Parse(state.Endpoint)
	if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/mcp" || len(state.Token) != 64 {
		return fmt.Errorf("invalid runtime discovery endpoint or credential")
	}
	return nil
}

func OpenRuntime(ctx context.Context, options *RuntimeOptions) (*RuntimeConnection, error) {
	if options == nil || options.Provider == nil {
		return nil, fmt.Errorf("runtime options and provider are required")
	}
	owner, startErr := StartRuntime(ctx, options)
	if startErr == nil {
		return &RuntimeConnection{owner: owner}, nil
	}
	state, err := readDiscovery(options.JournalPath + ".runtime.json")
	if err != nil {
		return nil, fmt.Errorf("start or discover runtime: %w", startErr)
	}
	manager, err := options.Provider(ctx)
	if err != nil || manager == nil || manager.Team.Name != state.Team {
		return nil, fmt.Errorf("runtime discovery team does not match requested team")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	c := &RuntimeConnection{state: state, readOnly: manager.ReadOnly, client: &http.Client{
		Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if err := c.call(ctx, userRequest{Action: "ping"}, nil); err != nil {
		return nil, fmt.Errorf("authenticate existing runtime owner: %w", err)
	}
	return c, nil
}

func (c *RuntimeConnection) Endpoint() string {
	if c.owner != nil {
		return c.owner.Endpoint()
	}
	return c.state.Endpoint
}

func (c *RuntimeConnection) Close() error {
	if c.owner != nil {
		return c.owner.Close()
	}
	c.client.CloseIdleConnections()
	return nil
}

func (c *RuntimeConnection) ReadUser(ctx context.Context) ([]channel.Record, error) {
	if c.owner != nil {
		return c.owner.ReadUser(ctx)
	}
	var records []channel.Record
	err := c.call(ctx, userRequest{Action: "read"}, &records)
	return records, err
}

func (c *RuntimeConnection) PostUser(ctx context.Context, text string, mentions []string) error {
	if c.owner != nil {
		return c.owner.PostUser(ctx, text, mentions)
	}
	return c.call(ctx, userRequest{Action: "post", Text: text, Mentions: mentions}, nil)
}

func (c *RuntimeConnection) InstallCredential(ctx context.Context, member string) error {
	if c.owner != nil {
		return c.owner.InstallCredential(ctx, member)
	}
	return c.call(ctx, userRequest{Action: "install", Member: member}, nil)
}

func (c *RuntimeConnection) RevokeCredential(ctx context.Context, member string) error {
	if c.owner != nil {
		return c.owner.RevokeCredential(ctx, member)
	}
	return c.call(ctx, userRequest{Action: "revoke", Member: member}, nil)
}

func (c *RuntimeConnection) call(ctx context.Context, request userRequest, result any) error {
	if c.readOnly && request.Action != "read" && request.Action != "ping" {
		return fmt.Errorf("read-only: runtime attachment mutation disabled")
	}
	b, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode runtime attachment request: %w", err)
	}
	endpoint := strings.TrimSuffix(c.state.Endpoint, "/mcp") + "/user"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build runtime attachment request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.state.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("call runtime owner: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("runtime owner rejected %s (HTTP %d)", request.Action, response.StatusCode)
	}
	if result != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(result); err != nil {
			return fmt.Errorf("decode runtime owner response: %w", err)
		}
	}
	return nil
}

func (r *Runtime) serveUser(w http.ResponseWriter, request *http.Request) {
	header := request.Header.Get("Authorization")
	if len(header) < 7 || !strings.EqualFold(header[:7], "Bearer ") ||
		subtle.ConstantTimeCompare([]byte(header[7:]), []byte(r.userToken)) != 1 {
		http.Error(w, "invalid owner credential", http.StatusUnauthorized)
		return
	}
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input userRequest
	if err := decode(http.MaxBytesReader(w, request.Body, 64<<10), &input); err != nil {
		http.Error(w, "invalid user request", http.StatusBadRequest)
		return
	}
	result, err := r.userAction(request.Context(), input)
	if err != nil {
		http.Error(w, "runtime action failed", http.StatusConflict)
		return
	}
	writeJSON(w, result)
}

func (r *Runtime) userAction(ctx context.Context, input userRequest) (any, error) {
	switch input.Action {
	case "ping":
		r.mu.RLock()
		name := r.manager.Team.Name
		r.mu.RUnlock()
		return map[string]string{"team": name}, nil
	case "read":
		return r.ReadUser(ctx)
	case "post":
		return nil, r.PostUser(ctx, input.Text, input.Mentions)
	case "install":
		return nil, r.InstallCredential(ctx, input.Member)
	case "revoke":
		return nil, r.RevokeCredential(ctx, input.Member)
	default:
		return nil, fmt.Errorf("unknown runtime user action")
	}
}
