package team

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/vqivs/agent-deck/internal/fsutil"
)

// ReportStore atomically persists the latest structured report per worker under a private root.
type ReportStore struct {
	Root     string
	ReadOnly bool
}

func (s ReportStore) Submit(ctx context.Context, t *Team, authenticatedMember string,
	definition DefinitionOfDone, reported *DoneReport, observed *ReportMetadata,
) (ReportRecord, error) {
	if err := ctx.Err(); err != nil {
		return ReportRecord{}, err
	}
	if s.ReadOnly {
		return ReportRecord{}, fmt.Errorf("read-only: done reports disabled")
	}
	member, err := reportWorker(t, authenticatedMember)
	if err != nil {
		return ReportRecord{}, err
	}
	if reported == nil || reported.Member != member.Name || reported.Session != member.Session {
		return ReportRecord{}, fmt.Errorf("reported identity does not match authenticated worker binding")
	}
	if !slices.Equal(definition, t.DefinitionOfDone) {
		return ReportRecord{}, fmt.Errorf("report definition does not match current trusted team policy")
	}
	definition = slices.Clone(t.DefinitionOfDone)
	if err := reported.Validate(definition); err != nil {
		return ReportRecord{}, err
	}
	if observed != nil && observed.ObservedAt.IsZero() {
		return ReportRecord{}, fmt.Errorf("independent metadata needs an observation timestamp")
	}
	record := ReportRecord{
		Version: 1, Team: t.Name, Member: member.Name, Session: member.Session,
		Source: "structured", RecordedAt: time.Now().UTC(), ReportedDone: reported.complete(definition),
		Definition: definition, Reported: reported, Observed: observed,
	}
	if err := s.save(ctx, &record); err != nil {
		return ReportRecord{}, err
	}
	return record, nil
}

func (s ReportStore) reportPath(teamName, memberName string) (string, error) {
	if s.Root == "" || Slug(teamName) == "" || Slug(memberName) == "" {
		return "", fmt.Errorf("report store needs root, team and member names")
	}
	return filepath.Join(s.Root, Slug(teamName), Slug(memberName)+".json"), nil
}

func (s ReportStore) save(ctx context.Context, record *ReportRecord) error {
	path, err := s.reportPath(record.Team, record.Member)
	if err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode done report: %w", err)
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("stored done report exceeds 1 MiB")
	}
	for _, dir := range []string{s.Root, filepath.Dir(path)} {
		if err := privateReportDir(dir); err != nil {
			return err
		}
	}
	return replaceReport(ctx, path, data)
}

func privateReportDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect report directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("report directory must be a real directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("protect report directory: %w", err)
	}
	return nil
}

func replaceReport(ctx context.Context, path string, data []byte) error {
	if err := fsutil.WriteAtomic(ctx, path, data, 0o600, ".report-*"); err != nil {
		return fmt.Errorf("replace done report: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open report directory for sync: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync report directory: %w", err)
	}
	return nil
}

func (s ReportStore) Load(ctx context.Context, t *Team, memberName string) (ReportRecord, error) {
	if err := ctx.Err(); err != nil {
		return ReportRecord{}, err
	}
	if _, err := reportWorker(t, memberName); err != nil {
		return ReportRecord{}, err
	}
	path, err := s.reportPath(t.Name, memberName)
	if err != nil {
		return ReportRecord{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return ReportRecord{}, fmt.Errorf("open done report: %w", err)
	}
	defer func() { _ = file.Close() }()
	var record ReportRecord
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, fmt.Errorf("decode stored done report: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ReportRecord{}, fmt.Errorf("stored done report has trailing data")
	}
	if err := record.validateStored(t.Name, memberName); err != nil {
		return ReportRecord{}, err
	}
	if !record.CurrentBinding(t) || !slices.Equal(record.Definition, t.DefinitionOfDone) {
		return ReportRecord{}, fmt.Errorf("stored report belongs to a stale binding or definition of done")
	}
	return record, nil
}

func (r *ReportRecord) validateStored(teamName, memberName string) error {
	if r.Version != 1 || r.Team != teamName || r.Member != memberName || r.Session == "" ||
		r.Source != "structured" || r.RecordedAt.IsZero() || r.Reported == nil ||
		r.Reported.Member != r.Member || r.Reported.Session != r.Session || r.InferredText != "" {
		return fmt.Errorf("invalid stored report identity or provenance")
	}
	if err := r.Reported.Validate(r.Definition); err != nil {
		return err
	}
	if r.ReportedDone != r.Reported.complete(r.Definition) {
		return fmt.Errorf("stored report completion disagrees with checklist")
	}
	return nil
}
