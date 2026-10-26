package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"
)

// Record is an immutable message or delivery event in a team's private journal.
type Record struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Thread     string            `json:"thread"`
	Sender     string            `json:"sender"`
	Recipients []string          `json:"recipients,omitempty"`
	Text       string            `json:"text,omitempty"`
	Private    bool              `json:"private,omitempty"`
	Hops       int               `json:"hops"`
	Message    string            `json:"message,omitempty"`
	Time       time.Time         `json:"time"`
	SenderID   string            `json:"sender_id,omitempty"`
	Audience   map[string]string `json:"audience,omitempty"`
}

// Journal serializes durable writes and exclusively owns one private JSONL file.
type Journal struct {
	mu        sync.Mutex
	file      *os.File
	records   []Record
	fatal     error
	positions map[string]int
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
	j := &Journal{file: f, positions: make(map[string]int)}
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
		j.positions[r.ID] = len(j.records)
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
	if j.fatal != nil {
		return fmt.Errorf("channel journal requires owner restart: %w", j.fatal)
	}
	if j.file == nil {
		if j.fatal != nil {
			return fmt.Errorf("channel journal requires owner restart: %w", j.fatal)
		}
		return fmt.Errorf("channel journal is closed")
	}
	if record == nil {
		return fmt.Errorf("channel record is required")
	}
	r := *record
	if r.ID == "" || r.Sender == "" || r.Kind == "" {
		return fmt.Errorf("channel record requires id, sender and kind")
	}
	if _, exists := j.positions[r.ID]; exists {
		return fmt.Errorf("duplicate channel record id")
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode channel record: %w", err)
	}
	if _, err := j.file.Write(append(b, '\n')); err != nil {
		j.stopWrites(err)
		return fmt.Errorf("append channel record: %w", err)
	}
	if err := j.file.Sync(); err != nil {
		j.stopWrites(err)
		return fmt.Errorf("sync channel record: %w", err)
	}
	r.Recipients = slices.Clone(r.Recipients)
	r.Audience = cloneAudience(r.Audience)
	j.positions[r.ID] = len(j.records)
	j.records = append(j.records, r)
	return nil
}

func (j *Journal) stopWrites(err error) {
	// An uncertain write must be recovered on reopen before accepting another record.
	// Retain the file lock until the owner explicitly closes this journal.
	j.fatal = err
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
		result[i].Audience = cloneAudience(result[i].Audience)
	}
	return result, nil
}

func cloneAudience(audience map[string]string) map[string]string {
	return maps.Clone(audience)
}

func visibleMessage(r *Record, member, identity string) bool {
	if r.Kind != "message" && r.Kind != "roster" {
		return false
	}
	return !r.Private || (r.Sender == member && r.SenderID == identity) ||
		(r.Audience[member] == identity && slices.Contains(r.Recipients, member))
}

// ReadUserMessages is a privileged bounded forward read, including private messages.
func (j *Journal) ReadUserMessages(ctx context.Context, since string, limit int) ([]Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	start := 0
	if since != "" {
		position, exists := j.positions[since]
		if !exists {
			return nil, fmt.Errorf("unknown user channel cursor")
		}
		start = position + 1
	}
	result := make([]Record, 0, limit)
	for i := start; i < len(j.records) && len(result) < limit; i++ {
		r := &j.records[i]
		if r.Kind != "message" && r.Kind != "roster" {
			continue
		}
		copyRecord := *r
		copyRecord.Recipients = slices.Clone(r.Recipients)
		copyRecord.Audience = cloneAudience(r.Audience)
		result = append(result, copyRecord)
	}
	return result, nil
}

// ReadMessages bounds member-facing results and applies immutable membership identities.
func (j *Journal) ReadMessages(ctx context.Context, member, identity, since string, limit int) ([]Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	result := make([]Record, 0, limit)
	found := since == ""
	start := 0
	if since != "" {
		position, exists := j.positions[since]
		if !exists {
			return nil, fmt.Errorf("unknown channel cursor")
		}
		start = position
	}
	for i := start; i < len(j.records); i++ {
		r := &j.records[i]
		if !visibleMessage(r, member, identity) {
			continue
		}
		if !found {
			found = r.ID == since
			continue
		}
		copyRecord := *r
		copyRecord.Recipients = slices.Clone(r.Recipients)
		copyRecord.Audience = cloneAudience(r.Audience)
		result = append(result, copyRecord)
		if len(result) == limit {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown channel cursor")
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
