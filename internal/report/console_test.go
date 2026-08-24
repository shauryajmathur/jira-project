package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"jira-project/internal/analytics"
	"jira-project/internal/model"
)

func TestPrintConsoleUsesRecordedWorkDefinitions(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	result := analytics.Result{
		Period: model.Period{Start: start, End: start.AddDate(0, 0, 1)}, BusinessDays: 1,
		UnassignedIssues: 1, UnassignedPlanned: 2 * 3600,
	}
	var output bytes.Buffer
	if err := PrintConsole(&output, result, "demo data", ""); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Sprint utilization (logged work)", "TEAM CAPACITY - TRACKED RESOURCE ACTIVITY",
		"Unassigned issues", "Planned time without an assignee",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("report does not contain %q", expected)
		}
	}
	for _, removed := range []string{"Observed capacity", "LOW-UTILIZATION", "OVER CAPACITY"} {
		if strings.Contains(output.String(), removed) {
			t.Errorf("report still contains old metric %q", removed)
		}
	}
}
