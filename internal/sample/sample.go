// Package sample supplies local data for explicit demo runs.
package sample

import (
	"time"

	"jira-project/internal/model"
)

func Issues(period model.Period) []model.Issue {
	alex := model.User{AccountID: "demo-alex", Name: "Alex Chen"}
	priya := model.User{AccountID: "demo-priya", Name: "Priya Nair"}
	marcus := model.User{AccountID: "demo-marcus", Name: "Marcus Lee"}
	noah := model.User{AccountID: "demo-noah", Name: "Noah Patel"}

	day := func(offset, hour int) time.Time {
		return period.Start.AddDate(0, 0, offset).Add(time.Duration(hour) * time.Hour)
	}
	hours := func(value int) int64 { return int64(value) * 60 * 60 }

	return []model.Issue{
		{
			Key: "WEB-142", Summary: "Build account settings page", Type: "Story", ProjectKey: "WEB",
			Assignee: alex, OriginalEstimateSeconds: hours(20),
			Worklogs: []model.Worklog{{Author: alex, Started: day(1, 10), Seconds: hours(9)}, {Author: alex, Started: day(2, 10), Seconds: hours(8)}},
		},
		{
			Key: "API-88", Summary: "Fix duplicate invoice creation", Type: "Bug", ProjectKey: "API",
			Assignee: priya, OriginalEstimateSeconds: hours(8),
			Worklogs: []model.Worklog{{Author: priya, Started: day(2, 9), Seconds: hours(11)}, {Author: alex, Started: day(2, 14), Seconds: hours(2)}},
		},
		{
			Key: "API-91", Summary: "Replace deprecated authentication middleware", Type: "Task", ProjectKey: "API",
			Labels: []string{"tech-debt"}, Assignee: priya, OriginalEstimateSeconds: hours(24),
			Worklogs: []model.Worklog{{Author: priya, Started: day(4, 9), Seconds: hours(18)}, {Author: marcus, Started: day(5, 11), Seconds: hours(4)}},
		},
		{
			Key: "OPS-31", Summary: "Investigate customer export failure", Type: "Support", ProjectKey: "OPS",
			Assignee: marcus, OriginalEstimateSeconds: hours(6),
			Worklogs: []model.Worklog{{Author: marcus, Started: day(3, 13), Seconds: hours(7)}},
		},
		{
			Key: "WEB-151", Summary: "Add dashboard loading skeleton", Type: "Story", ProjectKey: "WEB",
			Assignee: marcus, OriginalEstimateSeconds: hours(12),
			Worklogs: []model.Worklog{{Author: marcus, Started: day(7, 10), Seconds: hours(10)}},
		},
		{
			Key: "DATA-17", Summary: "Prototype retention query", Type: "Spike", ProjectKey: "DATA",
			Assignee: noah, OriginalEstimateSeconds: hours(16),
		},
	}
}
