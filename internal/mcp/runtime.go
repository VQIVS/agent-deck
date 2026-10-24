package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/vqivs/agent-deck/internal/channel"
	"github.com/vqivs/agent-deck/internal/team"
)

// ManagerProvider returns an independent, immutable snapshot on every call.
type ManagerProvider func(context.Context) (*team.Manager, error)

// RuntimeOptions configures one team server; credentials are never installed implicitly.
type RuntimeOptions struct {
	Scheduler    *SchedulerTools
	Provider     ManagerProvider
	JournalPath  string
	StateDir     string
	Address      string
	Interval     time.Duration
	Timeout      time.Duration
	OnError      func(error)
	Muted        func(string) bool
	SubmitReport func(context.Context, *team.Manager, string, *team.DoneReport) (any, error)
}

// Runtime owns one loopback server, private journal and guarded delivery loop.
type Runtime struct {
	Service *Service
	Tokens  *Tokens
	URL     string

	options      RuntimeOptions
	mu           sync.RWMutex
	stepMu       sync.Mutex
	credentialMu sync.Mutex
	manager      *team.Manager
	roster       string
	failed       bool
	closed       bool
	journal      *channel.Journal
	listener     net.Listener
	cancel       context.CancelFunc
	done         chan struct{}
	credentials  map[string]runtimeCredential
	userToken    string
}

func StartRuntime(ctx context.Context, options *RuntimeOptions) (*Runtime, error) {
	if options == nil || options.Provider == nil || options.JournalPath == "" {
		return nil, fmt.Errorf("runtime requires provider and journal path")
	}
	o := *options
	if err := configureScheduler(&o); err != nil {
		return nil, err
	}
	runtimeDefaults(&o)
	manager, err := o.Provider(ctx)
	if err != nil {
		return nil, fmt.Errorf("load runtime team: %w", err)
	}
	if manager == nil || manager.Team.Name == "" {
		return nil, fmt.Errorf("runtime provider requires a named team")
	}
	if err := runtimeMemberships(manager); err != nil {
		return nil, err
	}
	if err := runtimeStateDirectory(&o); err != nil {
		return nil, err
	}
	listener, err := listenRuntime(ctx, o.Address)
	if err != nil {
		return nil, err
	}
	journal, err := channel.Open(o.JournalPath)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("open runtime journal: %w", err)
	}
	r := &Runtime{
		options: o, manager: manager, journal: journal, listener: listener,
		Tokens: NewTokens(), URL: "http://" + listener.Addr().String() + "/mcp",
		done: make(chan struct{}), credentials: make(map[string]runtimeCredential),
	}
	r.Service, err = NewService(&Options{
		Team: manager.Team.Name, Journal: journal, Members: r.members, ReadOnly: r.readOnly,
		Transport: r, Instructions: r.instructions,
	})
	if err == nil {
		err = r.seedRoster(ctx)
	}
	if err != nil {
		_ = journal.Close()
		_ = listener.Close()
		return nil, err
	}
	if err := r.publishDiscovery(); err != nil {
		_ = journal.Close()
		_ = listener.Close()
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	go r.run(runCtx)
	return r, nil
}

func runtimeDefaults(options *RuntimeOptions) {
	if options.Address == "" {
		options.Address = "127.0.0.1:0"
	}
	if options.Interval <= 0 {
		options.Interval = 5 * time.Second
	}
	if options.Timeout <= 0 {
		options.Timeout = 15 * time.Second
	}
}

func (r *Runtime) seedRoster(ctx context.Context) error {
	records, err := r.journal.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("seed runtime roster: %w", err)
	}
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Kind == "roster" {
			r.roster = records[i].Text
			break
		}
	}
	return nil
}

func listenRuntime(ctx context.Context, address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return nil, fmt.Errorf("runtime address must use an explicit loopback IP and port")
	}
	var config net.ListenConfig
	listener, err := config.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for team MCP: %w", err)
	}
	return listener, nil
}

func (r *Runtime) run(ctx context.Context) {
	defer close(r.done)
	serverDone := make(chan error, 1)
	go func() {
		server := &Server{Tokens: r.Tokens, Service: r.Service, Scheduler: r.options.Scheduler}
		server.UserHandler = r.serveUser
		if r.options.SubmitReport != nil {
			server.SubmitReport = r.submitReport
		}
		serverDone <- server.Serve(ctx, r.listener)
	}()
	ticker := time.NewTicker(r.options.Interval)
	defer ticker.Stop()
	for {
		stepCtx, cancel := context.WithTimeout(ctx, r.options.Timeout)
		err := r.Step(stepCtx)
		cancel()
		r.report(err)
		select {
		case <-ctx.Done():
			<-serverDone
			r.cleanup()
			return
		case err := <-serverDone:
			r.report(err)
			r.cancel()
			r.cleanup()
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) report(err error) {
	if err != nil && r.options.OnError != nil {
		r.options.OnError(err)
	}
}

func (r *Runtime) Close() error {
	r.cancel()
	<-r.done
	return nil
}

func (r *Runtime) Alive() bool {
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

func (r *Runtime) cleanup() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.credentialMu.Lock()
	defer r.credentialMu.Unlock()
	for name := range r.credentials {
		credential := r.credentials[name]
		r.Tokens.Revoke(credential.identity)
		r.report(removeRuntimeConfig(&credential))
		delete(r.credentials, name)
	}
	r.removeDiscovery()
	r.report(r.journal.Close())
}

// Step refreshes policy/roster, revokes changed bindings, broadcasts and delivers.
func (r *Runtime) Step(ctx context.Context) error {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed {
		return fmt.Errorf("runtime is closed")
	}
	manager, err := r.current(ctx)
	if err != nil {
		r.mu.Lock()
		r.failed = true
		r.mu.Unlock()
		return err
	}
	text, err := manager.Team.Roster("current roster")
	if err != nil {
		return fmt.Errorf("render runtime roster: %w", err)
	}
	r.mu.Lock()
	r.manager, r.failed = manager, false
	changed := r.roster != text
	r.mu.Unlock()
	var failures []error
	if err := r.revokeChanged(manager); err != nil {
		failures = append(failures, err)
	}
	if changed && !manager.ReadOnly {
		if _, err := r.Service.BroadcastRoster(ctx, text); err != nil {
			failures = append(failures, err)
		} else {
			r.mu.Lock()
			r.roster = text
			r.mu.Unlock()
		}
	}
	if err := r.Service.Deliver(ctx); err != nil {
		failures = append(failures, fmt.Errorf("deliver runtime channel: %w", err))
	}
	return errors.Join(failures...)
}
