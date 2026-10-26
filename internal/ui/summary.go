package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vqivs/agent-deck/internal/channel"
	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

// SummaryAssistant is a bounded transcript snapshot, not a complete session archive.
type SummaryAssistant struct {
	Member, Directory, Session string
	Messages                   []api.Message
	Limit                      int
	Status                     SummaryStatus
}

// SummaryStatus records actual fetch results independently from record counts.
// A successful zero-record fetch is loaded; an unrequested source is unknown.
type SummaryStatus struct {
	Loaded, Loading bool
	LastSuccess     time.Time
	Error           string
}

// SummarySource combines persisted evidence and explicitly bounded session excerpts.
// Channel records must already be visibility-filtered by the caller.
type SummarySource struct {
	Board                      *team.TaskBoard
	Channel                    []channel.Record
	Assistants                 []SummaryAssistant
	Team                       string
	Members                    []string
	BoardStatus, ChannelStatus SummaryStatus
}

// SummaryEntry is an attributed evidence item, never an AI-generated completion claim.
type SummaryEntry struct {
	ID, Member, Kind, TaskID, Text string
	At                             time.Time
	Inferred                       bool
	Truncated                      bool
	Report                         *team.DoneReport
}

func ProjectSummary(source *SummarySource) []SummaryEntry {
	if source == nil {
		return nil
	}
	entries := map[string]SummaryEntry{}
	add := func(entry SummaryEntry) { entries[entry.ID] = entry }
	if source.Board != nil {
		summaryBoard(source.Board, add)
	}
	for i := range source.Channel {
		r := &source.Channel[i]
		if r.ID != "" && r.Text != "" {
			add(SummaryEntry{ID: "channel:" + r.ID, Member: r.Sender, Kind: "channel", Text: r.Text, At: r.Time})
		}
	}
	for i := range source.Assistants {
		summaryAssistant(&source.Assistants[i], add)
	}
	result := make([]SummaryEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].At.IsZero() != result[j].At.IsZero() {
			return !result[i].At.IsZero()
		}
		if !result[i].At.Equal(result[j].At) {
			return result[i].At.Before(result[j].At)
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func summaryBoard(board *team.TaskBoard, add func(SummaryEntry)) {
	for i := range board.History {
		summaryChange(&board.History[i], add)
	}
	for i := range board.Tasks {
		task := &board.Tasks[i]
		for j := range task.History {
			change := task.History[j]
			if change.Task == "" {
				change.Task = task.ID
			}
			summaryChange(&change, add)
		}
		for j := range task.Comments {
			c := &task.Comments[j]
			add(SummaryEntry{
				ID:     fmt.Sprintf("comment:%s:%d:%s:%s", task.ID, c.At.UnixNano(), c.Actor, c.Text),
				Member: c.Actor, TaskID: task.ID, Kind: "comment", Text: c.Text, At: c.At,
			})
		}
		if record := task.Completion; record != nil && record.Source == "structured" && record.Reported != nil {
			add(SummaryEntry{
				ID: fmt.Sprintf("report:%s:%d", task.ID, record.RecordedAt.UnixNano()), Member: record.Member, TaskID: task.ID,
				Kind: "report", Text: summaryReport(record.Reported), At: record.RecordedAt, Report: record.Reported,
			})
		}
		if task.Report != nil {
			add(SummaryEntry{
				ID: "legacy-report:" + task.ID, Member: kanbanAssignee(task), TaskID: task.ID,
				Kind: "report", Text: task.Report.Text + " · tests claimed: " + task.Report.Tests,
				At: task.UpdatedAt, Inferred: true,
			})
		}
	}
}

func summaryReport(report *team.DoneReport) string {
	lines := make([]string, 0, 1+len(report.Checklist)+len(report.Tests)+len(report.OpenIssues))
	lines = append(lines, report.Summary)
	for _, item := range report.Checklist {
		lines = append(lines, fmt.Sprintf("Criterion %s: complete=%t; %s", item.ID, item.Complete, item.Evidence))
	}
	for _, test := range report.Tests {
		lines = append(lines, fmt.Sprintf("Test claimed [%s] %s: %s · %s",
			test.CriterionID, test.Command, test.Outcome, test.Details))
	}
	for _, issue := range report.OpenIssues {
		lines = append(lines, "Open issue: "+issue)
	}
	return strings.Join(lines, "\n")
}

func summaryChange(change *team.TaskChange, add func(SummaryEntry)) {
	id := fmt.Sprintf("history:%s:%d:%s:%s:%s:%s",
		change.Task, change.At.UnixNano(), change.Actor, change.Field, change.From, change.To)
	add(SummaryEntry{
		ID: id, Member: change.Actor, TaskID: change.Task, Kind: "history", At: change.At,
		Text: change.Field + ": " + change.From + " → " + change.To,
	})
}

func summaryAssistant(source *SummaryAssistant, add func(SummaryEntry)) {
	limit := source.Limit
	if limit <= 0 {
		limit = 30
	}
	for i := max(0, len(source.Messages)-limit); i < len(source.Messages); i++ {
		message := &source.Messages[i]
		if message.Info.Role != "assistant" || message.Info.ID == "" {
			continue
		}
		var text []string
		for j := range message.Parts {
			if message.Parts[j].Type == "text" {
				text = append(text, message.Parts[j].Text)
			}
		}
		if strings.TrimSpace(strings.Join(text, "")) == "" {
			continue
		}
		excerpt := []rune(strings.Join(text, "\n"))
		truncated := len(excerpt) > 4000
		if truncated {
			marker := []rune("… [excerpt truncated]")
			excerpt = append(excerpt[:4000-len(marker)], marker...)
		}
		at := time.Time{}
		if message.Info.Time.Created > 0 {
			at = time.UnixMilli(message.Info.Time.Created)
		}
		add(SummaryEntry{
			ID:     fmt.Sprintf("assistant:%q:%q:%q", source.Directory, source.Session, message.Info.ID),
			Member: source.Member, Kind: "assistant", Text: string(excerpt),
			At: at, Inferred: true, Truncated: truncated,
		})
	}
}
