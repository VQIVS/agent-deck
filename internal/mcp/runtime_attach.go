package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/vqivs/agent-deck/internal/channel"
	"github.com/vqivs/agent-deck/internal/team"
)

const maxTaskNotices = 20

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
	Action   string            `json:"action"`
	Text     string            `json:"text,omitempty"`
	Mentions []string          `json:"mentions,omitempty"`
	Member   string            `json:"member,omitempty"`
	Since    string            `json:"since,omitempty"`
	Limit    int               `json:"limit,omitempty"`
	Notices  []team.TaskNotice `json:"notices,omitempty"`
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
	path := discoveryPath(&r.options)
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
	path := discoveryPath(&r.options)
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
	copyOptions := *options
	if err := runtimeStateDirectory(&copyOptions); err != nil {
		return nil, err
	}
	options = &copyOptions
	owner, startErr := StartRuntime(ctx, options)
	if startErr == nil {
		return &RuntimeConnection{owner: owner}, nil
	}
	state, err := readDiscovery(discoveryPath(options))
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
	if err := c.call(ctx, &userRequest{Action: "ping"}, nil); err != nil {
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
	err := c.call(ctx, &userRequest{Action: "read"}, &records)
	return records, err
}

func (c *RuntimeConnection) ReadUserSince(ctx context.Context, since string, limit int) ([]channel.Record, error) {
	if c.owner != nil {
		return c.owner.ReadUserSince(ctx, since, limit)
	}
	var records []channel.Record
	err := c.call(ctx, &userRequest{Action: "read_since", Since: since, Limit: limit}, &records)
	return records, err
}

func (c *RuntimeConnection) PostUser(ctx context.Context, text string, mentions []string) error {
	if c.owner != nil {
		return c.owner.PostUser(ctx, text, mentions)
	}
	return c.call(ctx, &userRequest{Action: "post", Text: text, Mentions: mentions}, nil)
}

// Notify forwards saved board notices through the privileged owner path.
func (c *RuntimeConnection) Notify(ctx context.Context, notices []team.TaskNotice) error {
	if c.readOnly {
		return fmt.Errorf("read-only: task notifications disabled")
	}
	if c.owner != nil {
		return c.owner.notifyUser(ctx, notices)
	}
	return c.call(ctx, &userRequest{Action: "notify", Notices: notices}, nil)
}

func (r *Runtime) notifyUser(ctx context.Context, notices []team.TaskNotice) error {
	manager, err := r.current(ctx)
	if err != nil {
		return err
	}
	if manager.ReadOnly || r.readOnly() {
		return fmt.Errorf("read-only: task notifications disabled")
	}
	if len(notices) == 0 || len(notices) > maxTaskNotices {
		return fmt.Errorf("task notices require 1 to %d entries", maxTaskNotices)
	}
	valid := make([]team.TaskNotice, 0, len(notices))
	var failures []error
	for _, notice := range notices {
		if err := validateOwnerNotice(manager, &notice); err != nil {
			failures = append(failures, err)
			continue
		}
		if len(notice.Text) > r.Service.options.MaxBytes {
			text := notice.Text[:r.Service.options.MaxBytes]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
			notice.Text = text
			failures = append(failures, fmt.Errorf("notify %q: text truncated to message limit", notice.To))
		}
		valid = append(valid, notice)
	}
	if len(valid) > 0 {
		if err := r.Service.Notify(ctx, valid); err != nil {
			failures = append(failures, fmt.Errorf("queue task notifications: %w", err))
		}
	}
	return errors.Join(failures...)
}

func validateOwnerNotice(manager *team.Manager, notice *team.TaskNotice) error {
	index, err := manager.MemberIndex(notice.To)
	if err != nil || manager.Team.Members[index].ID == "" || manager.Team.Members[index].Muted {
		return fmt.Errorf("notify %q: recipient absent or muted", notice.To)
	}
	if strings.TrimSpace(notice.Text) == "" || !utf8.ValidString(notice.Text) ||
		strings.TrimSpace(notice.Task) == "" || len(notice.Task) > 200 {
		return fmt.Errorf("notify %q: invalid content", notice.To)
	}
	return nil
}

func (c *RuntimeConnection) InstallCredential(ctx context.Context, member string) error {
	if c.owner != nil {
		return c.owner.InstallCredential(ctx, member)
	}
	return c.call(ctx, &userRequest{Action: "install", Member: member}, nil)
}

// SetupWorkflow is an explicit privileged user action, never a member MCP tool.
func (c *RuntimeConnection) SetupWorkflow(ctx context.Context, member string) error {
	if c.owner != nil {
		return c.owner.SetupWorkflow(ctx, member)
	}
	return c.call(ctx, &userRequest{Action: "setup_workflow", Member: member}, nil)
}

func (c *RuntimeConnection) RevokeCredential(ctx context.Context, member string) error {
	if c.owner != nil {
		return c.owner.RevokeCredential(ctx, member)
	}
	return c.call(ctx, &userRequest{Action: "revoke", Member: member}, nil)
}

func (c *RuntimeConnection) call(ctx context.Context, request *userRequest, result any) error {
	if c.readOnly && request.Action != "read" && request.Action != "read_since" && request.Action != "ping" {
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
	result, err := r.userAction(request.Context(), &input)
	if err != nil {
		http.Error(w, "runtime action failed", http.StatusConflict)
		return
	}
	writeJSON(w, result)
}

func (r *Runtime) userAction(ctx context.Context, input *userRequest) (any, error) {
	switch input.Action {
	case "ping":
		r.mu.RLock()
		name := r.manager.Team.Name
		r.mu.RUnlock()
		return map[string]string{"team": name}, nil
	case "read":
		return r.ReadUser(ctx)
	case "read_since":
		return r.ReadUserSince(ctx, input.Since, input.Limit)
	case "install":
		return nil, r.InstallCredential(ctx, input.Member)
	case "setup_workflow":
		return nil, r.SetupWorkflow(ctx, input.Member)
	case "reset_workflow":
		return nil, r.ResetWorkflow(ctx, input.Member)
	case "revoke":
		return nil, r.RevokeCredential(ctx, input.Member)
	case "post":
		return nil, r.PostUser(ctx, input.Text, input.Mentions)
	case "notify":
		return nil, r.notifyUser(ctx, input.Notices)
	default:
		return nil, fmt.Errorf("unknown runtime user action")
	}
}
