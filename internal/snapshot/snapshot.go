package snapshot

import (
	"context"
	"fmt"
	"strings"

	"jira-project/internal/analytics"
	"jira-project/internal/config"
	"jira-project/internal/jira"
	"jira-project/internal/model"
	"jira-project/internal/sample"
)

type Data struct {
	Result analytics.Result
	Source string
	JQL    string
	Issues []model.Issue
}

func Load(ctx context.Context, cfg config.Config) (Data, error) {
	if cfg.Demo {
		return calculate(sample.Issues(cfg.Period), cfg, "demo data"), nil
	}

	client, err := jira.NewClient(cfg.JiraBaseURL, cfg.JiraEmail, cfg.JiraToken, cfg.Parallelism)
	if err != nil {
		return Data{}, err
	}
	issues, err := client.FetchIssues(ctx, cfg.JQL, cfg.Period)
	if err != nil {
		return Data{}, fmt.Errorf("fetch Jira data: %w", err)
	}
	return calculate(issues, cfg, "Jira Cloud"), nil
}

func calculate(issues []model.Issue, cfg config.Config, source string) Data {
	result := analytics.Calculate(issues, analytics.Options{Period: cfg.Period})
	return Data{Result: result, Source: source, JQL: cfg.JQL, Issues: issues}
}

// ForProject recalculates a loaded snapshot for one Jira project without
// fetching the same issues and worklogs again.
func ForProject(data Data, cfg config.Config, projectKey string) (Data, bool) {
	if projectKey == "" {
		return data, true
	}

	issues := make([]model.Issue, 0)
	for _, issue := range data.Issues {
		key := strings.TrimSpace(issue.ProjectKey)
		if key == "" {
			key = "Unknown project"
		}
		if key == projectKey {
			issues = append(issues, issue)
		}
	}
	if len(issues) == 0 {
		return Data{}, false
	}

	filtered := calculate(issues, cfg, data.Source)
	filtered.JQL = data.JQL
	return filtered, true
}
