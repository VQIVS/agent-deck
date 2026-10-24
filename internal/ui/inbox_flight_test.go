package ui

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestInboxFlightsDeduplicateAndRetry(t *testing.T) {
	var flights InboxFlights
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if flights.Begin("/repo", "session", "request") {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("duplicate concurrent replies accepted")
	}
	flights.Finish("/repo", "session", "request", nil)
	flights.Observe("/repo", "session", []string{"request"})
	if flights.Begin("/repo", "session", "request") {
		t.Fatal("successful reply released before request removal")
	}
	flights.Observe("/repo", "session", nil)
	if !flights.Begin("/repo", "session", "request") {
		t.Fatal("removed request not cleaned up")
	}
	flights.Finish("/repo", "session", "request", errors.New("failed"))
	if !flights.Begin("/repo", "session", "request") || !flights.Begin("/other", "session", "request") ||
		!flights.Begin("/repo", "other", "request") || flights.Begin("", "session", "request") {
		t.Fatal("retry, scoping or empty identity guard failed")
	}
}
