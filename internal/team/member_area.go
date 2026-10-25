package team

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// schedulingLabel validates explicit scheduling metadata, never inferred identity.
func schedulingLabel(label string) bool {
	return label != "" && len(label) <= 100 && strings.TrimSpace(label) == label &&
		!strings.ContainsAny(label, "\x00\r\n\t")
}

func (worker *SchedulerWorker) validatePaths() error {
	if err := worker.Leases.Validate(); err != nil {
		return fmt.Errorf("worker %q leases: %w", worker.Name, err)
	}
	if err := worker.Areas.Validate(); err != nil {
		return fmt.Errorf("worker %q areas: %w", worker.Name, err)
	}
	return nil
}

func (m *Member) validateScheduling() error {
	if m.SchedulingRole != "" && !schedulingLabel(m.SchedulingRole) {
		return fmt.Errorf("invalid scheduling_role: use a trimmed label of at most 100 characters")
	}
	if len(m.Skills) > 100 || len(m.Areas) > 100 {
		return fmt.Errorf("scheduling skills and areas are limited to 100 entries")
	}
	seen := map[string]bool{}
	for _, skill := range m.Skills {
		if !schedulingLabel(skill) || seen[skill] {
			return fmt.Errorf("invalid or duplicate scheduling skill %q", skill)
		}
		seen[skill] = true
	}
	if err := m.Areas.Validate(); err != nil {
		return fmt.Errorf("invalid scheduling areas: %w", err)
	}
	return nil
}

func workerCompatibility(task *Task, worker *SchedulerWorker) string {
	role := worker.Role
	if role == "" {
		role = "worker"
	}
	if task.Role != "" && task.Role != role {
		return fmt.Sprintf("requires scheduling role %q; worker has %q", task.Role, role)
	}
	for _, skill := range task.Skills {
		if !slices.Contains(worker.Skills, skill) {
			return fmt.Sprintf("missing required skill %q", skill)
		}
	}
	if len(worker.Areas) == 0 {
		return ""
	}
	if len(task.Paths) == 0 {
		return "task needs explicit paths for area-restricted worker"
	}
	for _, taskPath := range task.Paths {
		if !slices.ContainsFunc(worker.Areas, func(area string) bool { return areaContains(area, taskPath) }) {
			return fmt.Sprintf("task path %q is outside configured member areas", taskPath)
		}
	}
	return ""
}

func (input *SchedulingInput) compatibilityRejections(task *Task) string {
	var reasons []string
	for i := range input.Workers {
		worker := &input.Workers[i]
		if reason := workerCompatibility(task, worker); reason != "" {
			reasons = append(reasons, worker.Name+": "+reason)
		}
	}
	return strings.Join(reasons, "; ")
}

// areaContains deliberately proves containment rather than accepting overlap.
// OpenCode stars cross directory boundaries. Complex wildcard-to-wildcard
// containment is accepted only for identical patterns or a containing final star.
func areaContains(area, taskPath string) bool {
	if area == taskPath {
		return true
	}
	if !strings.ContainsAny(taskPath, "*?") {
		pattern := regexp.QuoteMeta(area)
		pattern = strings.ReplaceAll(pattern, `\*`, `.*`)
		pattern = strings.ReplaceAll(pattern, `\?`, `.`)
		return regexp.MustCompile("^" + pattern + "$").MatchString(taskPath)
	}
	first := strings.IndexAny(area, "*?")
	if first < 0 || strings.Trim(area[first:], "*") != "" {
		return false
	}
	return strings.HasPrefix(leasePrefix(taskPath), area[:first])
}
