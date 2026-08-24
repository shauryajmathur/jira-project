package report

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jira-project/internal/analytics"
	"jira-project/internal/model"
)

func TestWritePDF(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	result := analytics.Result{
		Period:            model.Period{Start: start, End: start.AddDate(0, 0, 5)},
		BusinessDays:      5,
		IssueCount:        1,
		WorkedIssueCount:  1,
		ResourcesWithTime: 1,
		ActualSeconds:     6 * 3600,
		PlannedSeconds:    8 * 3600,
		Engineers: []analytics.Engineer{{
			Name: "Alex Chen", ActualSeconds: 6 * 3600, PlannedSeconds: 8 * 3600,
			ShareOfSprint: 100, WorkedIssueCount: 1,
			Activities: []analytics.Activity{{
				IssueKey: "APP-1", ProjectKey: "APP", IssueType: "Story",
				Category: model.CategoryStory, ActualSeconds: 6 * 3600,
				PlannedSeconds: 8 * 3600, Summary: "Build the sprint activity report",
			}},
		}},
		Categories: []analytics.Breakdown{{Name: "Story", ActualSeconds: 6 * 3600, PlannedSeconds: 8 * 3600, ShareOfActual: 100, IssueCount: 1}},
		Projects:   []analytics.Breakdown{{Name: "APP", ActualSeconds: 6 * 3600, PlannedSeconds: 8 * 3600, ShareOfActual: 100, IssueCount: 1}},
	}

	path := filepath.Join(t.TempDir(), "report.pdf")
	savedPath, err := WritePDF(path, result, "project = APP")
	if err != nil {
		t.Fatal(err)
	}
	if savedPath != path {
		t.Fatalf("saved path = %q, want %q", savedPath, path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) < 1000 || !bytes.HasPrefix(contents, []byte("%PDF-")) {
		t.Fatalf("generated file is not a valid-looking PDF (%d bytes)", len(contents))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("report permissions = %v, want 0600", info.Mode().Perm())
	}
}

func TestWritePDFPreservesUnicodeText(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	result := analytics.Result{
		Period: model.Period{Start: start, End: start.AddDate(0, 0, 1)}, BusinessDays: 1,
		Engineers: []analytics.Engineer{{
			Name:       "José Łukasz",
			Activities: []analytics.Activity{{IssueKey: "APP-1", Summary: "Résumé café", ProjectKey: "APP"}},
		}},
	}

	path := filepath.Join(t.TempDir(), "unicode-report.pdf")
	if _, err := WritePDF(path, result, "project = APP"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) < 20_000 {
		t.Fatal("embedded Unicode font is missing")
	}
}
