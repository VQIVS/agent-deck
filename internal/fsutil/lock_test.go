package fsutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLockContentionCancellationAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, err := Lock(t.Context(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("lock file is not private", info, err)
	}
	if _, err := Lock(t.Context(), path, false); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatal("contended lock succeeded", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := Lock(ctx, path, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("contended wait ignored cancellation", err)
	}
	acquired := make(chan error, 1)
	go func() {
		unlock, err := Lock(t.Context(), path, true)
		if err == nil {
			unlock()
		}
		acquired <- err
	}()
	release()
	release()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("release did not unblock waiter")
	}
}

func TestLockRejectsSymlinkAndCancelledCreation(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if release, err := Lock(t.Context(), link, false); err == nil {
		release()
		t.Fatal("symlink lock accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(dir, "canceled")
	if _, err := Lock(ctx, path, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled lock created file", err)
	}
}
