package ui

import (
	"strings"
	"testing"

	"github.com/qivs/agent-deck/internal/team"
)

func TestBoardApprovalShowsCapturedDetailsAndRequiresConfirmation(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	m.w, m.h = 100, 24
	m.board.Actions = []team.ReconcileAction{{
		ID: "original", Kind: "create",
		Proposal: &team.WorkerProposal{
			Task: "test-task", Role: "tester", Model: "provider/model",
			Paths: team.Leases{"internal/state/**"},
		},
	}}
	if cmd := m.boardKey(key("y")); cmd != nil || m.board.Confirm == nil {
		t.Fatal("first approval executed without confirmation")
	}
	view := m.boardView()
	for _, detail := range []string{
		"Confirm pool approval", "original", "test-task", "internal/state/**",
		"tester", "provider/model", "Reason: Add capacity for ready task",
	} {
		if !strings.Contains(view, detail) {
			t.Fatalf("confirmation missing %q: %s", detail, view)
		}
	}
	m.board.Actions = append(m.board.Actions, team.ReconcileAction{ID: "replacement", Kind: "create"})
	m.board.Index = 1
	if cmd := m.boardKey(key("y")); cmd == nil || m.board.Confirm != nil {
		t.Fatal("captured action lost when selection changed")
	}
}

func TestBoardApprovalRejectsStaleReadOnlyAndCancellation(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	m.board.Actions = []team.ReconcileAction{{ID: "original", Kind: "create"}}
	m.boardKey(key("y"))
	m.board.Actions = []team.ReconcileAction{{ID: "replacement", Kind: "create"}}
	if cmd := m.boardKey(key("y")); cmd != nil || !strings.Contains(m.note, "no longer pending") {
		t.Fatal("stale action retargeted")
	}
	m.boardKey(key("y"))
	m.opts.ReadOnly = true
	if cmd := m.boardKey(key("y")); cmd != nil {
		t.Fatal("read-only confirmation executed")
	}
	m.opts.ReadOnly = false
	m.boardKey(key("y"))
	if cmd := m.boardKey(key("n")); cmd != nil || m.board.Confirm != nil {
		t.Fatal("cancel did not discard captured action")
	}
}
