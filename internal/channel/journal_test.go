package channel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestJournalRecoveryAndPrivacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team.channel.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	records := []Record{
		{ID: "1", Kind: "message", Sender: "alice", Text: "public"},
		{ID: "2", Kind: "message", Sender: "alice", Recipients: []string{"bob"}, Private: true},
		{ID: "3", Kind: "ack", Sender: "bob", Recipients: []string{"alice"}, Private: true, Message: "2"},
	}
	for i := range records {
		if err := j.Append(ctx, &records[i]); err != nil {
			t.Fatal(err)
		}
	}
	if other, err := Open(path); err == nil {
		other.Close()
		t.Fatal("second writer acquired journal")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"id":"crash`)
	f.Close()
	j, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for member, count := range map[string]int{"alice": 3, "bob": 3, "eve": 1, "user": 1} {
		records, err := j.Read(ctx, member, "")
		if err != nil || len(records) != count {
			t.Fatalf("%s: got %v, %v", member, records, err)
		}
	}
	if _, err := j.Read(ctx, "eve", "2"); err == nil {
		t.Fatal("private cursor exposed")
	}
	if records, err := j.Read(ctx, "bob", "2"); err != nil || len(records) != 1 {
		t.Fatalf("cursor result: %v, %v", records, err)
	}
	if err := j.Append(ctx, &Record{ID: "4", Kind: "message", Sender: "bob"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
}

func TestFatalWriteRetainsOwnershipUntilClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	j.mu.Lock()
	j.stopWrites(errors.New("uncertain fsync"))
	j.mu.Unlock()
	if err := j.Append(t.Context(), &Record{ID: "x", Kind: "message", Sender: "a"}); err == nil {
		t.Fatal("fatal journal continued writing")
	}
	if other, err := Open(path); err == nil {
		other.Close()
		t.Fatal("fatal journal released ownership before owner restart")
	}
	j.Close()
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
}

func TestJournalConcurrentWritesAndCopies(t *testing.T) {
	j, err := Open(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			err := j.Append(context.Background(), &Record{
				ID: fmt.Sprint(i), Kind: "message", Sender: "alice", Private: true, Recipients: []string{"bob"},
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	records, err := j.Read(context.Background(), "bob", "")
	if err != nil || len(records) != 20 {
		t.Fatalf("records: %v, %v", records, err)
	}
	records[0].Recipients[0] = "eve"
	if records, _ := j.Read(context.Background(), "eve", ""); len(records) != 0 {
		t.Fatal("read mutated stored recipients")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := j.Append(ctx, &Record{ID: "canceled", Kind: "message", Sender: "alice"}); err == nil {
		t.Fatal("canceled append succeeded")
	}
}

func TestJournalRejectsCorruptionAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal")
	os.WriteFile(path, []byte("invalid\n"), 0o600)
	if j, err := Open(path); err == nil {
		j.Close()
		t.Fatal("complete corrupt record accepted")
	}
	link := filepath.Join(dir, "link")
	os.Symlink(path, link)
	if j, err := Open(link); err == nil {
		j.Close()
		t.Fatal("symlink accepted")
	}
}
