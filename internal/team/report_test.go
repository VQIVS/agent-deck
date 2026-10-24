package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func reportFixture() (Team, DefinitionOfDone, DoneReport) {
	tm := Team{Name: "example", Members: []Member{
		{Name: "lead", Role: "master", Session: "ses_lead"},
		{Name: "worker", Role: "worker", Session: "ses_worker"},
	}}
	definition := DefinitionOfDone{{ID: "tests", Text: "Run package tests", Required: true}}
	tm.DefinitionOfDone = definition
	report := DoneReport{
		Member: "worker", Session: "ses_worker", Summary: "Implemented reports",
		Checklist: []ReportItem{{ID: "tests", Complete: true, Evidence: "go test ./internal/team/..."}},
		Tests:     []ReportTest{{CriterionID: "tests", Command: "go test ./internal/team/...", Outcome: "pass"}},
	}
	return tm, definition, report
}

func TestReportIdentityAndValidation(t *testing.T) {
	tm, definition, original := reportFixture()
	for _, test := range []struct {
		name string
		edit func(*DoneReport)
	}{
		{name: "forged member", edit: func(r *DoneReport) { r.Member = "lead" }},
		{name: "stale session", edit: func(r *DoneReport) { r.Session = "ses_old" }},
		{name: "missing criterion", edit: func(r *DoneReport) { r.Checklist = nil }},
		{name: "unknown criterion", edit: func(r *DoneReport) {
			r.Checklist = []ReportItem{{ID: "unknown", Complete: true}}
		}},
		{name: "invalid outcome", edit: func(r *DoneReport) {
			r.Tests = []ReportTest{{Command: "go test", Outcome: "probably"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := original
			test.edit(&report)
			store := ReportStore{Root: filepath.Join(t.TempDir(), "reports")}
			if _, err := store.Submit(t.Context(), &tm, "worker", definition, &report, nil); err == nil {
				t.Fatal("invalid report accepted")
			}
			if _, err := os.Stat(store.Root); !os.IsNotExist(err) {
				t.Fatalf("invalid report caused filesystem side effects: %v", err)
			}
		})
	}
	store := ReportStore{Root: t.TempDir()}
	if _, err := store.Submit(t.Context(), &tm, "lead", definition, &original, nil); err == nil {
		t.Fatal("master accepted as worker identity")
	}
	tm.Members[1].Role = "master"
	if _, err := store.Submit(t.Context(), &tm, "worker", definition, &original, nil); err == nil {
		t.Fatal("stale role accepted")
	}
}

func TestReportPrivatePersistenceAndReadOnly(t *testing.T) {
	tm, definition, report := reportFixture()
	store := ReportStore{Root: filepath.Join(t.TempDir(), "reports")}
	observed := &ReportMetadata{ObservedAt: time.Now().UTC(), Branch: "main", Commit: "abc", Dirty: true}
	record, err := store.Submit(t.Context(), &tm, "worker", definition, &report, observed)
	if err != nil {
		t.Fatal(err)
	}
	if !record.ReportedDone || record.Source != "structured" || !record.Observed.Dirty {
		t.Fatalf("claims and observed metadata lost: %+v", record)
	}
	for _, path := range []string{
		store.Root, filepath.Join(store.Root, "example"),
		filepath.Join(store.Root, "example", "worker.json"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode %o, want %o", path, info.Mode().Perm(), want)
		}
	}
	store.ReadOnly = true
	loaded, err := store.Load(t.Context(), &tm, "worker")
	if err != nil || loaded.Reported.Summary != report.Summary || loaded.Observed.Commit != "abc" {
		t.Fatalf("load report: %+v, %v", loaded, err)
	}
	report.Summary = "overwrite"
	if _, err := store.Submit(t.Context(), &tm, "worker", definition, &report, nil); err == nil {
		t.Fatal("read-only submit accepted")
	}
	loaded, err = store.Load(t.Context(), &tm, "worker")
	if err != nil || loaded.Reported.Summary == "overwrite" {
		t.Fatal("read-only submit changed report", err)
	}
	store.ReadOnly = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Submit(ctx, &tm, "worker", definition, &report, nil); err == nil {
		t.Fatal("canceled submit accepted")
	}
	loaded, err = store.Load(t.Context(), &tm, "worker")
	if err != nil || loaded.Reported.Summary == "overwrite" {
		t.Fatal("canceled submit changed report", err)
	}
}

func TestReportStrictDecodeAndCompletion(t *testing.T) {
	for _, data := range []string{
		`{"independently_observed":{"commit":"fake"}}`, `{} {}`, strings.Repeat(" ", (1<<20)+1),
	} {
		if _, err := DecodeDoneReport([]byte(data)); err == nil {
			t.Fatal("invalid payload accepted")
		}
	}
	_, definition, report := reportFixture()
	report.Checklist[0].Complete = false
	if report.complete(definition) {
		t.Fatal("incomplete required item marked done")
	}
	report.Checklist[0].Complete = true
	report.Tests[0].Outcome = "fail"
	if report.complete(definition) {
		t.Fatal("failed reported test marked done")
	}
	report.Tests[0].Outcome = "pass"
	report.OpenIssues = []string{"still broken"}
	if report.complete(definition) {
		t.Fatal("open issues marked done")
	}
}

func TestInferredReportNeverClaimsDone(t *testing.T) {
	tm, _, _ := reportFixture()
	var message api.Message
	message.Info.ID, message.Info.Role = "msg_report", "assistant"
	message.Parts = []api.Part{{Type: "text", Text: "All tests pass, everything done", SessionID: "ses_worker"}}
	record, err := InferDoneReport(&tm, "worker", "idle", []api.Message{message})
	if err != nil {
		t.Fatal(err)
	}
	if record.Source != "inferred" || record.ReportedDone || record.Reported != nil ||
		record.Observed != nil || len(record.Definition) != 0 || record.MessageID != "msg_report" {
		t.Fatalf("inferred report manufactured claims: %+v", record)
	}
	if _, err := InferDoneReport(&tm, "worker", "busy", []api.Message{message}); err == nil {
		t.Fatal("busy report inferred")
	}
	message.Parts[0].SessionID = "ses_other"
	if _, err := InferDoneReport(&tm, "worker", "idle", []api.Message{message}); err == nil {
		t.Fatal("foreign message inferred")
	}
}

func TestReportReplacementAndRetainedIdentity(t *testing.T) {
	tm, definition, report := reportFixture()
	store := ReportStore{Root: filepath.Join(t.TempDir(), "reports")}
	if _, err := store.Submit(t.Context(), &tm, "worker", definition, &report, nil); err != nil {
		t.Fatal(err)
	}
	report.Summary = "updated"
	if _, err := store.Submit(t.Context(), &tm, "worker", definition, &report, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context(), &tm, "worker")
	if err != nil || loaded.Reported.Summary != "updated" || !loaded.CurrentBinding(&tm) {
		t.Fatal("replacement report lost", err)
	}
	tm.Members[1].Session = "ses_replacement"
	if loaded.CurrentBinding(&tm) {
		t.Fatal("retained report applies to replacement session")
	}
	if _, err := store.Load(t.Context(), &tm, "worker"); err == nil {
		t.Fatal("stale report loaded for replacement session")
	}
	files, err := filepath.Glob(filepath.Join(store.Root, "example", ".report-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary files leaked", files, err)
	}
}

func TestReportTrustedDefinition(t *testing.T) {
	tm, definition, report := reportFixture()
	store := ReportStore{Root: filepath.Join(t.TempDir(), "reports")}
	forged := DefinitionOfDone{{ID: "tests", Text: "Run package tests", Required: false}}
	if _, err := store.Submit(t.Context(), &tm, "worker", forged, &report, nil); err == nil {
		t.Fatal("caller weakened trusted definition")
	}
	if _, err := store.Submit(t.Context(), &tm, "worker", definition, &report, nil); err != nil {
		t.Fatal(err)
	}
	tm.DefinitionOfDone = DefinitionOfDone{{ID: "tests", Text: "Run race tests", Required: true}}
	if _, err := store.Load(t.Context(), &tm, "worker"); err == nil {
		t.Fatal("stored report supplied its own obsolete definition")
	}
}

func TestRequiredReportCriterionNeedsPassingTest(t *testing.T) {
	_, definition, original := reportFixture()
	for _, outcome := range []string{"not_run", "fail"} {
		report := original
		report.Tests = []ReportTest{{CriterionID: "tests", Command: "go test", Outcome: outcome}}
		if report.complete(definition) {
			t.Fatalf("required criterion marked done with %s", outcome)
		}
	}
	original.Tests = nil
	if original.complete(definition) {
		t.Fatal("required criterion marked done without passing evidence")
	}
	original.Tests = []ReportTest{{CriterionID: "another", Command: "go test", Outcome: "pass"}}
	if original.complete(definition) {
		t.Fatal("passing unrelated test satisfied required criterion")
	}
}

func TestReportRejectsSymlinkDirectory(t *testing.T) {
	tm, definition, report := reportFixture()
	store := ReportStore{Root: t.TempDir()}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(store.Root, "example")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(t.Context(), &tm, "worker", definition, &report, nil); err == nil {
		t.Fatal("symlink report directory accepted")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatal("report escaped into symlink target", err)
	}
}
