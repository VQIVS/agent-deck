package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"
)

// Record is an immutable message or delivery event in a team's private journal.
type Record struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Thread     string    `json:"thread"`
	Sender     string    `json:"sender"`
	Recipients []string  `json:"recipients,omitempty"`
	Text       string    `json:"text,omitempty"`
	Private    bool      `json:"private,omitempty"`
	Hops       int       `json:"hops"`
	Message    string    `json:"message,omitempty"`
	Time       time.Time `json:"time"`
}

// Journal serializes durable writes and exclusively owns one private JSONL file.
type Journal struct {
	mu      sync.Mutex
	file    *os.File
	records []Record
}

func Open(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create channel directory: %w", err)
	}
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open channel journal: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	j := &Journal{file: f}
	if err := j.load(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return j, nil
}

func (j *Journal) load() error {
	if err := syscall.Flock(int(j.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("lock channel journal: %w", err)
	}
	if err := j.file.Chmod(0o600); err != nil {
		return fmt.Errorf("protect channel journal: %w", err)
	}
	b, err := io.ReadAll(j.file)
	if err != nil {
		return fmt.Errorf("read channel journal: %w", err)
	}
	end := bytes.LastIndexByte(b, '\n') + 1
	for _, line := range bytes.Split(b[:end], []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("decode channel journal: %w", err)
		}
		j.records = append(j.records, r)
	}
	// Only discard an incomplete crash tail; complete records are never rewritten.
	if err := j.file.Truncate(int64(end)); err != nil {
		return fmt.Errorf("recover channel journal tail: %w", err)
	}
	if _, err := j.file.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seek channel journal: %w", err)
	}
	return nil
}

func (j *Journal) Append(ctx context.Context, record *Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.file == nil {
		return fmt.Errorf("channel journal is closed")
	}
	if record == nil {
		return fmt.Errorf("channel record is required")
	}
	r := *record
	if r.ID == "" || r.Sender == "" || r.Kind == "" {
		return fmt.Errorf("channel record requires id, sender and kind")
	}
	for i := range j.records {
		if j.records[i].ID == r.ID {
			return fmt.Errorf("duplicate channel record id")
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode channel record: %w", err)
	}
	if _, err := j.file.Write(append(b, '\n')); err != nil {
		j.stopWrites()
		return fmt.Errorf("append channel record: %w", err)
	}
	if err := j.file.Sync(); err != nil {
		j.stopWrites()
		return fmt.Errorf("sync channel record: %w", err)
	}
	r.Recipients = slices.Clone(r.Recipients)
	j.records = append(j.records, r)
	return nil
}

func (j *Journal) stopWrites() {
	// An uncertain write must be recovered on reopen before accepting another record.
	_ = j.file.Close()
	j.file = nil
}

func (j *Journal) Read(ctx context.Context, member, since string) ([]Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := []Record{}
	found := since == ""
	for i := range j.records {
		r := &j.records[i]
		visible := !r.Private || r.Sender == member || slices.Contains(r.Recipients, member)
		if !visible {
			continue
		}
		if found {
			copyRecord := *r
			copyRecord.Recipients = slices.Clone(r.Recipients)
			result = append(result, copyRecord)
		} else if r.ID == since {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown channel cursor")
	}
	return result, nil
}

// Snapshot is a privileged infrastructure view, never a member-facing read.
func (j *Journal) Snapshot(ctx context.Context) ([]Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := slices.Clone(j.records)
	for i := range result {
		result[i].Recipients = slices.Clone(result[i].Recipients)
	}
	return result, nil
}

func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	if err != nil {
		return fmt.Errorf("close channel journal: %w", err)
	}
	return nil
}
