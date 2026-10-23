package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/qivs/agent-deck/internal/team"
)

func healthFields(member *team.Member) []formField {
	stall := "5m"
	if member.StallAfter != nil {
		stall = member.StallAfter.String()
	}
	return []formField{
		{Label: "Stall threshold (0 disables)", Value: stall},
		{Label: "Auto-nudge enabled", Value: strconv.FormatBool(member.Nudge.Enabled), Choices: []string{"false", "true"}},
		{Label: "Auto-nudge maximum", Value: strconv.Itoa(member.Nudge.MaxNudges)},
		{Label: "Auto-nudge interval", Value: member.Nudge.MinInterval.String()},
		{Label: "Auto-nudge message", Value: member.Nudge.Message},
		{
			Label: "Task done (stops nudges)", Value: strconv.FormatBool(member.NudgeStatus.Done),
			Choices: []string{"false", "true"},
		},
	}
}

func applyHealthValues(member *team.Member, values map[string]string) error {
	if values["Stall threshold (0 disables)"] == "" {
		return nil
	}
	stall, err := time.ParseDuration(values["Stall threshold (0 disables)"])
	if err != nil || stall < 0 {
		return fmt.Errorf("stall threshold needs a nonnegative duration")
	}
	interval, err := time.ParseDuration(values["Auto-nudge interval"])
	if err != nil {
		return fmt.Errorf("parse nudge interval: %w", err)
	}
	maximum, err := strconv.Atoi(values["Auto-nudge maximum"])
	if err != nil {
		return fmt.Errorf("parse nudge maximum: %w", err)
	}
	member.StallAfter = &stall
	member.Nudge = team.NudgePolicy{
		Enabled: values["Auto-nudge enabled"] == "true", MaxNudges: maximum,
		MinInterval: interval, Message: values["Auto-nudge message"],
	}
	member.NudgeStatus.Done = values["Task done (stops nudges)"] == "true"
	if err := member.Nudge.Validate(); err != nil {
		return fmt.Errorf("validate nudge policy: %w", err)
	}
	return nil
}
