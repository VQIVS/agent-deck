package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

// DoneCriterion defines one stable checklist identity and its completion requirement.
type DoneCriterion struct {
	ID       string `json:"id" yaml:"id"`
	Text     string `json:"text" yaml:"text"`
	Required bool   `json:"required" yaml:"required"`
}

// DefinitionOfDone is the trusted checklist against which worker reports are checked.
type DefinitionOfDone []DoneCriterion

func (d DefinitionOfDone) Validate() error {
	if len(d) == 0 || len(d) > 100 {
		return fmt.Errorf("definition of done needs 1 to 100 criteria")
	}
	ids := map[string]bool{}
	for _, item := range d {
		if !reportText(item.ID, 100) || !reportText(item.Text, 2000) || ids[item.ID] {
			return fmt.Errorf("invalid or duplicate done criterion %q", item.ID)
		}
		ids[item.ID] = true
	}
	return nil
}

// ReportItem is a worker's completion claim, never an independently verified result.
type ReportItem struct {
	ID       string `json:"id"`
	Complete bool   `json:"complete"`
	Evidence string `json:"evidence,omitempty"`
}

// ReportTest records a worker-reported command and pass, fail, or not_run outcome.
type ReportTest struct {
	CriterionID string `json:"criterion_id"`
	Command     string `json:"command"`
	Outcome     string `json:"outcome"`
	Details     string `json:"details,omitempty"`
}

// DoneReport is the untrusted worker payload; identity is checked against authenticated membership.
type DoneReport struct {
	TaskID     string       `json:"task_id,omitempty"`
	Member     string       `json:"member"`
	Session    string       `json:"session"`
	Summary    string       `json:"summary"`
	Checklist  []ReportItem `json:"checklist"`
	Tests      []ReportTest `json:"tests,omitempty"`
	OpenIssues []string     `json:"open_issues,omitempty"`
}

func DecodeDoneReport(data []byte) (DoneReport, error) {
	var report DoneReport
	if len(data) > 1<<20 {
		return report, fmt.Errorf("done report exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return report, fmt.Errorf("decode done report: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return report, fmt.Errorf("done report must contain exactly one JSON value")
	}
	return report, nil
}

func (r *DoneReport) Validate(definition DefinitionOfDone) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	if !reportText(r.Member, 200) || !reportText(r.Session, 200) || !reportText(r.Summary, 16000) ||
		len(r.Checklist) != len(definition) || len(r.Tests) > 100 || len(r.OpenIssues) > 100 {
		return fmt.Errorf("invalid done report identity, summary, or checklist size")
	}
	if err := r.validateItems(definition); err != nil {
		return err
	}
	return r.validateDetails()
}

func (r *DoneReport) validateDetails() error {
	for _, test := range r.Tests {
		if !reportText(test.Command, 4000) || len(test.Details) > 16000 ||
			(test.Outcome != "pass" && test.Outcome != "fail" && test.Outcome != "not_run") {
			return fmt.Errorf("invalid reported test %q", test.Command)
		}
	}
	for _, issue := range r.OpenIssues {
		if !reportText(issue, 4000) {
			return fmt.Errorf("invalid open issue")
		}
	}
	return nil
}

func (r *DoneReport) validateItems(definition DefinitionOfDone) error {
	ids := map[string]bool{}
	for _, item := range definition {
		ids[item.ID] = true
	}
	for _, item := range r.Checklist {
		if !ids[item.ID] || len(item.Evidence) > 16000 {
			return fmt.Errorf("unknown, duplicate, or invalid report criterion %q", item.ID)
		}
		delete(ids, item.ID)
	}
	for _, test := range r.Tests {
		if !slices.ContainsFunc(definition, func(item DoneCriterion) bool { return item.ID == test.CriterionID }) {
			return fmt.Errorf("reported test needs a known criterion_id %q", test.CriterionID)
		}
	}
	return nil
}

func (r *DoneReport) complete(definition DefinitionOfDone) bool {
	if len(r.OpenIssues) > 0 {
		return false
	}
	for _, test := range r.Tests {
		if test.Outcome != "pass" {
			return false
		}
	}
	required := map[string]bool{}
	for _, item := range definition {
		required[item.ID] = item.Required
	}
	for _, item := range r.Checklist {
		if required[item.ID] && !item.Complete {
			return false
		}
		if required[item.ID] && !slices.ContainsFunc(r.Tests, func(test ReportTest) bool {
			return test.CriterionID == item.ID && test.Outcome == "pass"
		}) {
			return false
		}
	}
	return true
}

func reportText(text string, maximum int) bool {
	return strings.TrimSpace(text) != "" && len(text) <= maximum && !strings.ContainsRune(text, '\x00')
}

// ReportMetadata is independently collected Git information, supplied only by trusted integration code.
type ReportMetadata struct {
	ObservedAt time.Time `json:"observed_at"`
	Branch     string    `json:"branch"`
	Commit     string    `json:"commit"`
	Dirty      bool      `json:"dirty"`
}

// ReportRecord distinguishes structured worker claims from an inferred idle-message projection.
type ReportRecord struct {
	Version      int              `json:"version"`
	Team         string           `json:"team"`
	Member       string           `json:"member"`
	Session      string           `json:"session"`
	Source       string           `json:"source"`
	RecordedAt   time.Time        `json:"recorded_at"`
	ReportedDone bool             `json:"reported_done"`
	Definition   DefinitionOfDone `json:"definition,omitempty"`
	Reported     *DoneReport      `json:"reported,omitempty"`
	Observed     *ReportMetadata  `json:"independently_observed,omitempty"`
	InferredText string           `json:"inferred_text,omitempty"`
	MessageID    string           `json:"message_id,omitempty"`
}

// CurrentBinding keeps retained reports from marking a replacement session done.
func (r *ReportRecord) CurrentBinding(t *Team) bool {
	if r.Team != t.Name {
		return false
	}
	member, err := reportWorker(t, r.Member)
	return err == nil && member.Session == r.Session
}

func reportWorker(t *Team, authenticatedMember string) (*Member, error) {
	for i := range t.Members {
		member := &t.Members[i]
		if member.Name == authenticatedMember && member.Role == "worker" && member.Session != "" {
			return member, nil
		}
	}
	return nil, fmt.Errorf("report identity must be a current worker with a bound session")
}

// InferDoneReport projects an idle assistant message without manufacturing checklist or test outcomes.
func InferDoneReport(t *Team, authenticatedMember, state string, messages []api.Message) (ReportRecord, error) {
	member, err := reportWorker(t, authenticatedMember)
	if err != nil {
		return ReportRecord{}, err
	}
	if state != "idle" {
		return ReportRecord{}, fmt.Errorf("inferred report requires idle worker")
	}
	latest := LatestAssistant(messages)
	if latest == nil || strings.TrimSpace(AssistantText(messages)) == "" {
		return ReportRecord{}, fmt.Errorf("no assistant text to infer a report from")
	}
	for i := range latest.Parts {
		part := &latest.Parts[i]
		if part.SessionID != "" && part.SessionID != member.Session {
			return ReportRecord{}, fmt.Errorf("assistant message belongs to a different session")
		}
	}
	return ReportRecord{
		Version: 1, Team: t.Name, Member: member.Name, Session: member.Session,
		Source: "inferred", RecordedAt: time.Now().UTC(),
		InferredText: AssistantText(messages), MessageID: latest.Info.ID,
	}, nil
}
