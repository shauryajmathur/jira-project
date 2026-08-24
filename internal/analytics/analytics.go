// Package analytics turns Jira issues and worklogs into report-ready metrics.
package analytics

import (
	"sort"
	"strings"

	"jira-project/internal/model"
)

type Options struct {
	Period model.Period
}

type Result struct {
	Period              model.Period
	BusinessDays        int
	IssueCount          int
	Engineers           []Engineer
	Categories          []Breakdown
	Projects            []Breakdown
	ActualSeconds       int64
	PlannedSeconds      int64
	WorkedIssueCount    int
	ResourcesWithTime   int
	IssuesNoEstimate    int
	IssuesNoWorklogs    int
	UnassignedIssues    int
	UnassignedPlanned   int64
	UnattributedSeconds int64
}

type Engineer struct {
	ID               string
	Name             string
	ActualSeconds    int64
	PlannedSeconds   int64
	ShareOfSprint    float64
	WorkedIssueCount int
	Activities       []Activity
}

type Activity struct {
	IssueKey       string
	Summary        string
	IssueType      string
	Category       model.Category
	ProjectKey     string
	ActualSeconds  int64
	PlannedSeconds int64
}

type Breakdown struct {
	Name            string
	ActualSeconds   int64
	PlannedSeconds  int64
	ShareOfActual   float64
	IssueCount      int
	uniqueIssueKeys map[string]struct{}
}

type engineerBuilder struct {
	Engineer
	activities map[string]*Activity
}

// Calculate performs all aggregation without depending on Jira or console code.
func Calculate(issues []model.Issue, options Options) Result {
	businessDays := countBusinessDays(options.Period)
	result := Result{Period: options.Period, BusinessDays: businessDays, IssueCount: len(issues)}

	engineers := make(map[string]*engineerBuilder)
	categoryTotals := make(map[string]*Breakdown)
	projectTotals := make(map[string]*Breakdown)

	getEngineer := func(user model.User) *engineerBuilder {
		name := strings.TrimSpace(user.Name)
		if name == "" {
			name = "Unknown user"
		}
		key := user.AccountID
		if key == "" {
			key = "name:" + strings.ToLower(name)
		}
		if existing := engineers[key]; existing != nil {
			if existing.Name == "Unknown user" && name != "Unknown user" {
				existing.Name = name
			}
			return existing
		}
		created := &engineerBuilder{Engineer: Engineer{ID: key, Name: name}, activities: make(map[string]*Activity)}
		engineers[key] = created
		return created
	}

	for _, issue := range issues {
		category := Classify(issue)
		categoryTotal := breakdown(categoryTotals, string(category))
		projectName := issue.ProjectKey
		if projectName == "" {
			projectName = "Unknown project"
		}
		projectTotal := breakdown(projectTotals, projectName)
		addIssue(categoryTotal, issue.Key)
		addIssue(projectTotal, issue.Key)

		result.PlannedSeconds += issue.OriginalEstimateSeconds
		categoryTotal.PlannedSeconds += issue.OriginalEstimateSeconds
		projectTotal.PlannedSeconds += issue.OriginalEstimateSeconds
		if issue.OriginalEstimateSeconds == 0 {
			result.IssuesNoEstimate++
		}

		if issue.Assignee.Name != "" || issue.Assignee.AccountID != "" {
			assignee := getEngineer(issue.Assignee)
			assignee.PlannedSeconds += issue.OriginalEstimateSeconds
			activity := activityFor(assignee, issue, category)
			activity.PlannedSeconds = issue.OriginalEstimateSeconds
		} else {
			result.UnassignedIssues++
			result.UnassignedPlanned += issue.OriginalEstimateSeconds
		}

		hasWorklog := false
		for _, worklog := range issue.Worklogs {
			if worklog.Seconds <= 0 || !options.Period.Contains(worklog.Started) {
				continue
			}
			hasWorklog = true
			result.ActualSeconds += worklog.Seconds
			categoryTotal.ActualSeconds += worklog.Seconds
			projectTotal.ActualSeconds += worklog.Seconds

			if worklog.Author.Name == "" && worklog.Author.AccountID == "" {
				result.UnattributedSeconds += worklog.Seconds
				continue
			}
			engineer := getEngineer(worklog.Author)
			engineer.ActualSeconds += worklog.Seconds
			activityFor(engineer, issue, category).ActualSeconds += worklog.Seconds
		}
		if !hasWorklog {
			result.IssuesNoWorklogs++
		} else {
			result.WorkedIssueCount++
		}
	}

	for _, engineer := range engineers {
		if engineer.ActualSeconds == 0 && engineer.PlannedSeconds == 0 {
			continue
		}
		engineer.ShareOfSprint = percent(engineer.ActualSeconds, result.ActualSeconds)
		if engineer.ActualSeconds > 0 {
			result.ResourcesWithTime++
		}
		for _, activity := range engineer.activities {
			if activity.ActualSeconds == 0 && activity.PlannedSeconds == 0 {
				continue
			}
			engineer.Activities = append(engineer.Activities, *activity)
			if activity.ActualSeconds > 0 {
				engineer.WorkedIssueCount++
			}
		}
		sort.Slice(engineer.Activities, func(i, j int) bool {
			if engineer.Activities[i].ActualSeconds == engineer.Activities[j].ActualSeconds {
				return engineer.Activities[i].IssueKey < engineer.Activities[j].IssueKey
			}
			return engineer.Activities[i].ActualSeconds > engineer.Activities[j].ActualSeconds
		})
		result.Engineers = append(result.Engineers, engineer.Engineer)
	}
	sort.Slice(result.Engineers, func(i, j int) bool {
		if result.Engineers[i].ActualSeconds == result.Engineers[j].ActualSeconds {
			if result.Engineers[i].Name == result.Engineers[j].Name {
				return result.Engineers[i].ID < result.Engineers[j].ID
			}
			return result.Engineers[i].Name < result.Engineers[j].Name
		}
		return result.Engineers[i].ActualSeconds > result.Engineers[j].ActualSeconds
	})

	result.Categories = orderedCategories(categoryTotals, result.ActualSeconds)
	result.Projects = orderedBreakdowns(projectTotals, result.ActualSeconds)
	return result
}

// Classify maps Jira issue types and labels into the PRD's work categories.
func Classify(issue model.Issue) model.Category {
	switch {
	case hasAlias(issue, "tech debt", "technical debt"):
		return model.CategoryTechDebt
	case hasAlias(issue, "bug", "defect"):
		return model.CategoryBug
	case hasAlias(issue, "support", "customer support", "service request", "incident", "customer request"):
		return model.CategorySupport
	case hasAlias(issue, "story", "user story", "task", "feature", "improvement", "epic"):
		return model.CategoryStory
	default:
		return model.CategoryOther
	}
}

func hasAlias(issue model.Issue, aliases ...string) bool {
	values := append([]string{issue.Type}, issue.Labels...)
	for _, value := range values {
		value = normalizeAlias(value)
		for _, alias := range aliases {
			if value == alias {
				return true
			}
		}
	}
	return false
}

func normalizeAlias(value string) string {
	value = strings.ToLower(value)
	value = strings.NewReplacer("-", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func activityFor(engineer *engineerBuilder, issue model.Issue, category model.Category) *Activity {
	if existing := engineer.activities[issue.Key]; existing != nil {
		return existing
	}
	created := &Activity{
		IssueKey: issue.Key, Summary: issue.Summary, IssueType: issue.Type,
		Category: category, ProjectKey: issue.ProjectKey,
	}
	engineer.activities[issue.Key] = created
	return created
}

func breakdown(values map[string]*Breakdown, name string) *Breakdown {
	if existing := values[name]; existing != nil {
		return existing
	}
	created := &Breakdown{Name: name, uniqueIssueKeys: make(map[string]struct{})}
	values[name] = created
	return created
}

func addIssue(value *Breakdown, key string) {
	if _, exists := value.uniqueIssueKeys[key]; !exists {
		value.uniqueIssueKeys[key] = struct{}{}
		value.IssueCount++
	}
}

func orderedCategories(values map[string]*Breakdown, total int64) []Breakdown {
	result := make([]Breakdown, 0, len(model.CategoryOrder))
	for _, category := range model.CategoryOrder {
		value := breakdown(values, string(category))
		value.ShareOfActual = percent(value.ActualSeconds, total)
		result = append(result, *value)
	}
	return result
}

func orderedBreakdowns(values map[string]*Breakdown, total int64) []Breakdown {
	result := make([]Breakdown, 0, len(values))
	for _, value := range values {
		value.ShareOfActual = percent(value.ActualSeconds, total)
		result = append(result, *value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ActualSeconds == result[j].ActualSeconds {
			return result[i].Name < result[j].Name
		}
		return result[i].ActualSeconds > result[j].ActualSeconds
	})
	return result
}

func countBusinessDays(period model.Period) int {
	days := 0
	for day := period.Start; day.Before(period.End); day = day.AddDate(0, 0, 1) {
		if day.Weekday() != 0 && day.Weekday() != 6 {
			days++
		}
	}
	return days
}

func percent(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}
