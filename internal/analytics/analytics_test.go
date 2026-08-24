package analytics

import (
	"testing"
	"time"

	"jira-project/internal/model"
)

func TestCalculate(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) // Monday
	period := model.Period{Start: start, End: start.AddDate(0, 0, 5)}
	alex := model.User{AccountID: "1", Name: "Alex"}
	priya := model.User{AccountID: "2", Name: "Priya"}

	issues := []model.Issue{
		{
			Key: "APP-1", Type: "Story", ProjectKey: "APP", Assignee: alex,
			OriginalEstimateSeconds: 10 * 3600,
			Worklogs:                []model.Worklog{{Author: alex, Started: start.Add(10 * time.Hour), Seconds: 8 * 3600}},
		},
		{
			Key: "APP-2", Type: "Task", Labels: []string{"tech-debt"}, ProjectKey: "APP", Assignee: priya,
			OriginalEstimateSeconds: 4 * 3600,
			Worklogs: []model.Worklog{
				{Author: alex, Started: start.Add(24 * time.Hour), Seconds: 2 * 3600},
				{Author: priya, Started: period.End, Seconds: 99 * 3600}, // End is excluded.
			},
		},
	}

	result := Calculate(issues, Options{Period: period})

	if result.BusinessDays != 5 {
		t.Fatalf("BusinessDays = %d, want 5", result.BusinessDays)
	}
	if result.ActualSeconds != 10*3600 {
		t.Fatalf("ActualSeconds = %d, want %d", result.ActualSeconds, 10*3600)
	}
	if result.PlannedSeconds != 14*3600 {
		t.Fatalf("PlannedSeconds = %d, want %d", result.PlannedSeconds, 14*3600)
	}
	if result.WorkedIssueCount != 2 {
		t.Fatalf("WorkedIssueCount = %d, want 2", result.WorkedIssueCount)
	}
	if result.ResourcesWithTime != 1 {
		t.Fatalf("ResourcesWithTime = %d, want 1", result.ResourcesWithTime)
	}
	if got := result.Categories[2].ActualSeconds; got != 2*3600 {
		t.Fatalf("tech debt actual = %d, want %d", got, 2*3600)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		issue model.Issue
		want  model.Category
	}{
		{model.Issue{Type: "Bug"}, model.CategoryBug},
		{model.Issue{Type: "Task", Labels: []string{"technical-debt"}}, model.CategoryTechDebt},
		{model.Issue{Type: "Incident"}, model.CategorySupport},
		{model.Issue{Type: "User Story"}, model.CategoryStory},
		{model.Issue{Type: "Debug Task"}, model.CategoryOther},
		{model.Issue{Type: "Supportability"}, model.CategoryOther},
		{model.Issue{Type: "Bug", Labels: []string{"tech_debt"}}, model.CategoryTechDebt},
		{model.Issue{Type: "Spike"}, model.CategoryOther},
	}
	for _, test := range tests {
		if got := Classify(test.issue); got != test.want {
			t.Errorf("Classify(%q) = %q, want %q", test.issue.Type, got, test.want)
		}
	}
}

func TestCalculateKeepsAccountIDsDistinct(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	period := model.Period{Start: start, End: start.AddDate(0, 0, 1)}
	issues := []model.Issue{
		{Key: "APP-1", Assignee: model.User{AccountID: "one", Name: "Alex"}, OriginalEstimateSeconds: 3600},
		{Key: "APP-2", Assignee: model.User{AccountID: "two", Name: "Alex"}, OriginalEstimateSeconds: 7200},
	}

	result := Calculate(issues, Options{Period: period})
	if len(result.Engineers) != 2 {
		t.Fatalf("engineers = %d, want 2", len(result.Engineers))
	}
	if result.Engineers[0].ID == result.Engineers[1].ID {
		t.Fatalf("engineer IDs were merged: %+v", result.Engineers)
	}
}

func TestCalculateDoesNotCreateAnonymousEngineer(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	period := model.Period{Start: start, End: start.AddDate(0, 0, 1)}
	issues := []model.Issue{{
		Key: "APP-1", Type: "Task", ProjectKey: "APP", OriginalEstimateSeconds: 4 * 3600,
		Worklogs: []model.Worklog{{Started: start.Add(time.Hour), Seconds: 2 * 3600}},
	}}

	result := Calculate(issues, Options{Period: period})
	if len(result.Engineers) != 0 {
		t.Fatalf("anonymous work produced %d engineers", len(result.Engineers))
	}
	if result.UnattributedSeconds != 2*3600 {
		t.Fatalf("unattributed seconds = %d", result.UnattributedSeconds)
	}
	if result.UnassignedIssues != 1 || result.UnassignedPlanned != 4*3600 {
		t.Fatalf("unassigned = %d issues, %d seconds", result.UnassignedIssues, result.UnassignedPlanned)
	}
}

func TestCalculateExcludesResourceWithoutEstimateOrLoggedTime(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	period := model.Period{Start: start, End: start.AddDate(0, 0, 1)}
	issues := []model.Issue{{
		Key: "APP-1", Assignee: model.User{AccountID: "one", Name: "Alex"},
	}}

	result := Calculate(issues, Options{Period: period})
	if len(result.Engineers) != 0 || result.ResourcesWithTime != 0 {
		t.Fatalf("empty assignment produced %d resources and %d with time", len(result.Engineers), result.ResourcesWithTime)
	}
}
