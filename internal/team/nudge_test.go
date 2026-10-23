package team

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestNudgeBoundaries(t *testing.T) {
	now := time.Unix(1000, 0)
	policy := NudgePolicy{Enabled: true, MaxNudges: 2, MinInterval: time.Minute, Message: "Continue if unfinished"}
	idle := now.Add(-time.Minute)
	s := NudgeState{}
	if !policy.Due(now, idle, "IDLE", false, s) {
		t.Fatal("first nudge not due")
	}
	for _, status := range []string{"BUSY", "WAITING", "STALLED", "ERROR", "UNKNOWN", "OFFLINE"} {
		if policy.Due(now, idle, status, false, s) {
			t.Fatalf("nudge permitted in %s", status)
		}
	}
	if policy.Due(now, idle, "IDLE", true, s) || policy.Due(now, now, "IDLE", false, s) {
		t.Fatal("read-only or fresh idle bypassed guard")
	}
	s.Sent(now)
	if policy.Due(now.Add(time.Second), idle, "IDLE", false, s) {
		t.Fatal("minimum interval bypassed")
	}
	now = now.Add(time.Minute)
	if !policy.Due(now, idle, "IDLE", false, s) {
		t.Fatal("second nudge not due")
	}
	s.Sent(now)
	if policy.Due(now.Add(time.Hour), idle, "IDLE", false, s) {
		t.Fatal("maximum bypassed")
	}
	s = NudgeState{Done: true}
	if policy.Due(now, idle, "IDLE", false, s) {
		t.Fatal("done member nudged")
	}
}

func TestNudgePolicyValidationAndPersistence(t *testing.T) {
	if (NudgePolicy{}).Validate() != nil {
		t.Fatal("default must be valid and disabled")
	}
	for _, p := range []NudgePolicy{
		{Enabled: true},
		{MaxNudges: -1},
		{MinInterval: -1},
		{Enabled: true, MaxNudges: 1, MinInterval: time.Minute, Message: " "},
	} {
		if p.Validate() == nil {
			t.Fatalf("invalid policy accepted: %+v", p)
		}
	}
	p := NudgePolicy{Enabled: true, MaxNudges: 3, MinInterval: time.Minute, Message: "Continue"}
	b, err := yaml.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var restored NudgePolicy
	if err = yaml.Unmarshal(b, &restored); err != nil || restored != p {
		t.Fatalf("policy round trip: %+v, %v", restored, err)
	}
	s := NudgeState{Count: 2, Last: time.Unix(1000, 0).UTC(), Done: true}
	b, err = yaml.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var counter NudgeState
	if err = yaml.Unmarshal(b, &counter); err != nil || counter != s {
		t.Fatalf("counter round trip: %+v, %v", counter, err)
	}
}
