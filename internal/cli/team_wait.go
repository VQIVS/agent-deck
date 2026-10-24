package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

type sendReceipt struct {
	Server, Session, Directory, AssistantID string
}

func receiptPath(manager *team.Manager, index int) (string, error) {
	root, err := server.StateRoot()
	if err != nil {
		return "", fmt.Errorf("resolve receipt directory: %w", err)
	}
	return filepath.Join(root, "sent", team.Slug(manager.Team.Name),
		team.Slug(manager.Team.Members[index].Name)+".json"), nil
}

func teamSend(ctx context.Context, manager *team.Manager, index int, opts *teamFlags, out io.Writer) error {
	if opts.ReadOnly {
		return fmt.Errorf("read-only: messaging disabled")
	}
	text, err := readPrompt(opts)
	if err != nil {
		return err
	}
	member := &manager.Team.Members[index]
	messages, err := api.New(manager.Team.Server, true).Messages(ctx, member.Dir, member.Session)
	if err != nil {
		return fmt.Errorf("read pre-send baseline: %w", err)
	}
	receipt := sendReceipt{Server: manager.Team.Server, Session: member.Session, Directory: member.Dir}
	if latest := team.LatestAssistant(messages); latest != nil {
		receipt.AssistantID = latest.Info.ID
	}
	path, err := receiptPath(manager, index)
	if err != nil {
		return err
	}
	if err := saveReceipt(path, receipt); err != nil {
		return err
	}
	if err := manager.Send(ctx, index, text); err != nil {
		return fmt.Errorf("send to member (send receipt retained for uncertain delivery): %w", err)
	}
	if opts.JSON {
		return writeJSON(out, map[string]string{"member": member.Name, "session": member.Session, "status": "sent"})
	}
	return writeText(out, "Message sent to "+member.Name)
}

func saveReceipt(path string, receipt sendReceipt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create send receipt directory: %w", err)
	}
	b, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("encode send receipt: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".receipt-*")
	if err != nil {
		return fmt.Errorf("create send receipt: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(b)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write send receipt: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("save send receipt: %w", err)
	}
	return nil
}

func loadReceipt(manager *team.Manager, index int) (*sendReceipt, error) {
	path, err := receiptPath(manager, index)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read send receipt: %w", err)
	}
	var receipt sendReceipt
	if err := json.Unmarshal(b, &receipt); err != nil {
		return nil, fmt.Errorf("decode send receipt: %w", err)
	}
	member := &manager.Team.Members[index]
	if receipt.Server != manager.Team.Server || receipt.Session != member.Session || receipt.Directory != member.Dir {
		return nil, nil
	}
	return &receipt, nil
}

func teamWait(ctx context.Context, manager *team.Manager, index int, opts *teamFlags, out io.Writer) error {
	if opts.Timeout <= 0 || opts.Poll <= 0 {
		return fmt.Errorf("--timeout and --poll must be positive")
	}
	receipt, err := loadReceipt(manager, index)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	started := time.Now()
	seenBusy := false
	for {
		status := manager.Observe(ctx, index)
		seenBusy = seenBusy || status.State == "busy"
		done := waitDone(ctx, manager, index, receipt, status.State, seenBusy,
			time.Since(started) >= min(2*opts.Poll, 10*time.Second))
		if status.State == "waiting" || done {
			return waitResult(out, opts, &status)
		}
		if status.State == "offline" && ctx.Err() == nil {
			return fmt.Errorf("member %q is offline: %s", status.Name, status.Error)
		}
		if err := waitPoll(ctx, opts); err != nil {
			return err
		}
	}
}

func waitDone(ctx context.Context, manager *team.Manager, index int, receipt *sendReceipt,
	state string, seenBusy, graceElapsed bool,
) bool {
	if state != "idle" {
		return false
	}
	if seenBusy || receipt == nil && graceElapsed {
		return true
	}
	return receipt != nil && completedSince(ctx, manager, index, receipt)
}

func waitPoll(ctx context.Context, opts *teamFlags) error {
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &exitError{3, "timed out waiting for " + opts.Member}
		}
		return fmt.Errorf("wait for member: %w", ctx.Err())
	case <-time.After(opts.Poll):
		return nil
	}
}

func completedSince(ctx context.Context, manager *team.Manager, index int, receipt *sendReceipt) bool {
	member := &manager.Team.Members[index]
	messages, err := api.New(manager.Team.Server, true).Messages(ctx, member.Dir, member.Session)
	if err != nil {
		return false
	}
	latest := team.LatestAssistant(messages)
	return latest != nil && latest.Info.ID != receipt.AssistantID && latest.Info.Time.Completed > 0
}

func waitResult(out io.Writer, opts *teamFlags, status *team.MemberStatus) error {
	var err error
	if opts.JSON {
		err = writeJSON(out, status)
	} else {
		err = writeText(out, status.Name+": "+status.State)
	}
	if err != nil {
		return err
	}
	if status.State == "waiting" {
		return &exitError{2, status.Name + " is waiting for a permission or question"}
	}
	return nil
}
