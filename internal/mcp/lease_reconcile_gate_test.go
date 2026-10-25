package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

type leaseGateTransport struct {
	fail  bool
	sends int
}

func (g *leaseGateTransport) ReconcileTaskLeases(context.Context) error {
	if g.fail {
		return fmt.Errorf("native policy unavailable")
	}
	return nil
}
func (g *leaseGateTransport) Busy(context.Context, string) (bool, error) { return false, nil }
func (g *leaseGateTransport) Send(context.Context, string, string) error { g.sends++; return nil }

func TestLeaseFailureQueuesNoticeButGatesWakeUntilRepair(t *testing.T) {
	transport := &leaseGateTransport{fail: true}
	service := newTestService(t, func(options *Options) {
		options.Transport = transport
		options.Instructions = func(context.Context, string) (string, error) { return "", nil }
	})
	notices := []team.TaskNotice{{To: "bob", Task: "T-1", Text: "review requested"}}
	if err := service.Notify(t.Context(), notices); err == nil {
		t.Fatal("native policy failure hidden")
	}
	if err := service.Deliver(t.Context()); err == nil || transport.sends != 0 {
		t.Fatal("worker woke before native reset", err)
	}
	transport.fail = false
	if err := service.Deliver(t.Context()); err != nil || transport.sends != 1 {
		t.Fatal("repair did not deliver saved notice", err)
	}
}
