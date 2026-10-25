package team

import (
	"slices"
	"testing"
)

func TestMemberSchedulingMetadataStoreRoundtripAndValidation(t *testing.T) {
	tm := example()
	member := &tm.Members[1]
	member.SchedulingRole, member.Skills, member.Areas = "research", []string{"sources"}, Leases{"docs/research/**"}
	store := Store{Root: t.TempDir()}
	if _, err := store.Save(t.Context(), &tm, nil); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.Load(t.Context(), tm.Name)
	if err != nil {
		t.Fatal(err)
	}
	got := &loaded.Members[1]
	if got.Role != "worker" || got.Capability != member.Capability || got.SchedulingRole != "research" ||
		!slices.Equal(got.Skills, member.Skills) || !slices.Equal(got.Areas, member.Areas) {
		t.Fatal("scheduling metadata lost or management/permission metadata changed", got)
	}
	for _, invalid := range []string{"role", "empty-skill", "duplicate-skill", "area"} {
		t.Run(invalid, func(t *testing.T) {
			bad := tm
			bad.Members = slices.Clone(tm.Members)
			m := &bad.Members[1]
			switch invalid {
			case "role":
				m.SchedulingRole = " research "
			case "empty-skill":
				m.Skills = []string{""}
			case "duplicate-skill":
				m.Skills = []string{"go", "go"}
			case "area":
				m.Areas = Leases{"../outside/**"}
			}
			if err := bad.Validate(); err == nil {
				t.Fatal("invalid scheduling metadata accepted")
			}
		})
	}
}

func TestMemberAreaContainmentRequiresCoverage(t *testing.T) {
	cases := []struct {
		area, task string
		want       bool
	}{
		{"internal/team/**", "internal/team/scheduler.go", true},
		{"internal/team/**", "internal/team/scheduler*.go", true},
		{"internal/team/**", "internal/**", false},
		{"internal/team/scheduler*.go", "internal/team/**", false},
		{"internal/team/scheduler*.go", "internal/team/scheduler_test.go", true},
		{"internal/team/scheduler*.go", "internal/team/scheduler*.go", true},
		{"docs/research/**", "docs/design/spec.md", false},
		{"src/a.go", "src/a.go*", false},
		{"src/a?", "src/a*", false},
		{"src/a?", "src/ab", true},
	}
	for _, tc := range cases {
		if got := areaContains(tc.area, tc.task); got != tc.want {
			t.Errorf("%q contains %q: got %v, want %v", tc.area, tc.task, got, tc.want)
		}
	}
}

func TestSchedulingCompatibilityRejectsExplicitlyAssignedMismatch(t *testing.T) {
	for _, mismatch := range []string{"role", "skill", "area", "broad-path", "pathless"} {
		t.Run(mismatch, func(t *testing.T) {
			task := schedulerTask("work", "internal/team/scheduler.go")
			task.Role, task.Skills, task.Assignee = "core", []string{"go"}, "chosen"
			worker := SchedulerWorker{
				Name: "chosen", State: "idle", Role: "core",
				Skills: []string{"go"}, Areas: Leases{"internal/team/**"},
			}
			switch mismatch {
			case "role":
				worker.Role = "reporting"
			case "skill":
				worker.Skills = nil
			case "area":
				worker.Areas = Leases{"docs/**"}
			case "broad-path":
				task.Paths = Leases{"internal/**"}
			case "pathless":
				task.Paths = nil
				if workerCompatibility(&task, &worker) == "" {
					t.Fatal("pathless task accepted for area-restricted worker")
				}
				return
			}
			input := schedulerInput(task)
			input.Workers = []SchedulerWorker{worker}
			plan, err := PlanSchedule(input)
			if err != nil || len(plan.Assignments) != 0 || len(plan.Proposals) != 0 || plan.Blocked[task.ID] == "" {
				t.Fatal(plan, err)
			}
		})
	}
}
