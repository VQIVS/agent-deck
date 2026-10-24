package ui

import (
	"fmt"
	"strings"

	"github.com/vqivs/agent-deck/internal/team"
)

// ReportCard renders provenance explicitly; currentBinding must come from Record.CurrentBinding.
func ReportCard(record *team.ReportRecord, currentBinding bool, width int) string {
	if record == nil || width <= 0 {
		return ""
	}
	var lines []string
	switch {
	case record.Source == "inferred":
		lines = []string{
			"INFERRED · " + record.Member + " · latest idle assistant text",
			"Completion and test results are not verified or structured.", record.InferredText,
		}
	case record.Source == "structured" && record.Reported != nil:
		lines = structuredReportLines(record)
	default:
		return fit("Report unavailable: unknown provenance", width)
	}
	if !currentBinding {
		lines = append([]string{"HISTORICAL · report does not match current binding"}, lines...)
	}
	for i, line := range lines {
		parts := strings.Split(line, "\n")
		for j := range parts {
			parts[j] = fit(parts[j], width)
		}
		lines[i] = strings.Join(parts, "\n")
	}
	return strings.Join(lines, "\n")
}

func structuredReportLines(record *team.ReportRecord) []string {
	status := "REPORT"
	if record.ReportedDone {
		status = "DONE"
	}
	lines := []string{status + " · worker-reported · " + record.Member, record.Reported.Summary}
	labels := map[string]string{}
	for _, criterion := range record.Definition {
		labels[criterion.ID] = criterion.Text
	}
	for _, item := range record.Reported.Checklist {
		mark := "[ ]"
		if item.Complete {
			mark = "[x]"
		}
		label := labels[item.ID]
		if label == "" {
			label = item.ID
		}
		lines = append(lines, mark+" "+label+" (worker claim)")
		if item.Evidence != "" {
			lines = append(lines, "  Evidence reported: "+item.Evidence)
		}
	}
	for _, test := range record.Reported.Tests {
		lines = append(lines, fmt.Sprintf("Test reported %s: %s", test.Outcome, test.Command))
	}
	for _, issue := range record.Reported.OpenIssues {
		lines = append(lines, "Open issue: "+issue)
	}
	if metadata := record.Observed; metadata != nil {
		lines = append(lines, fmt.Sprintf("Independently observed Git: branch=%s commit=%s dirty=%t",
			metadata.Branch, metadata.Commit, metadata.Dirty))
	} else {
		lines = append(lines, "Independent Git metadata: unavailable")
	}
	return lines
}
