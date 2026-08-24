// Package jira reads issues and worklogs from the Jira Cloud REST API v3.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"jira-project/internal/model"
)

const pageSize = 100

type Client struct {
	baseURL     string
	email       string
	token       string
	parallelism int
	httpClient  *http.Client
}

// NewClient creates a Jira client using an Atlassian email and API token.
func NewClient(baseURL, email, token string, parallelism int) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid Jira API base URL %q", baseURL)
	}
	if email == "" || token == "" {
		return nil, fmt.Errorf("email and API token for Jira are required")
	}
	if parallelism < 1 {
		parallelism = 1
	}
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		email:       email,
		token:       token,
		parallelism: parallelism,
		httpClient:  &http.Client{},
	}, nil
}

// FetchIssues searches Jira and then fetches every issue's paginated worklogs.
func (c *Client) FetchIssues(ctx context.Context, jql string, period model.Period) ([]model.Issue, error) {
	issues, err := c.searchIssues(ctx, jql)
	if err != nil {
		return nil, err
	}
	if err := c.attachWorklogs(ctx, issues, period); err != nil {
		return nil, err
	}
	return issues, nil
}

type searchRequest struct {
	JQL           string   `json:"jql"`
	MaxResults    int      `json:"maxResults"`
	Fields        []string `json:"fields"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type jiraUser struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

type searchIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary              string    `json:"summary"`
		IssueType            namedItem `json:"issuetype"`
		Project              keyedItem `json:"project"`
		Assignee             *jiraUser `json:"assignee"`
		Labels               []string  `json:"labels"`
		TimeOriginalEstimate int64     `json:"timeoriginalestimate"`
	} `json:"fields"`
}

type namedItem struct {
	Name string `json:"name"`
}

type keyedItem struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type searchResponse struct {
	Issues        []searchIssue `json:"issues"`
	NextPageToken string        `json:"nextPageToken"`
	IsLast        bool          `json:"isLast"`
}

func (c *Client) searchIssues(ctx context.Context, jql string) ([]model.Issue, error) {
	request := searchRequest{
		JQL:        jql,
		MaxResults: pageSize,
		Fields:     []string{"summary", "issuetype", "project", "assignee", "labels", "timeoriginalestimate"},
	}
	var issues []model.Issue
	seenTokens := make(map[string]struct{})
	for {
		var response searchResponse
		if err := c.doJSON(ctx, http.MethodPost, "/rest/api/3/search/jql", nil, request, &response); err != nil {
			return nil, fmt.Errorf("search issues: %w", err)
		}
		for _, item := range response.Issues {
			issue := model.Issue{
				Key:                     item.Key,
				Summary:                 item.Fields.Summary,
				Type:                    item.Fields.IssueType.Name,
				ProjectKey:              item.Fields.Project.Key,
				ProjectName:             item.Fields.Project.Name,
				Labels:                  item.Fields.Labels,
				OriginalEstimateSeconds: item.Fields.TimeOriginalEstimate,
			}
			if item.Fields.Assignee != nil {
				issue.Assignee = user(*item.Fields.Assignee)
			}
			issues = append(issues, issue)
		}
		if response.IsLast || response.NextPageToken == "" {
			break
		}
		if _, seen := seenTokens[response.NextPageToken]; seen {
			return nil, fmt.Errorf("search issues: Jira repeated a pagination token")
		}
		seenTokens[response.NextPageToken] = struct{}{}
		request.NextPageToken = response.NextPageToken
	}
	return issues, nil
}

func (c *Client) attachWorklogs(ctx context.Context, issues []model.Issue, period model.Period) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	errors := make(chan error, c.parallelism)
	var workers sync.WaitGroup

	for worker := 0; worker < c.parallelism; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				worklogs, err := c.issueWorklogs(ctx, issues[index].Key, period)
				if err != nil {
					select {
					case errors <- fmt.Errorf("issue %s: %w", issues[index].Key, err):
					default:
					}
					cancel()
					return
				}
				issues[index].Worklogs = worklogs
			}
		}()
	}

	go func() {
		defer close(jobs)
		for index := range issues {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()

	workers.Wait()
	select {
	case err := <-errors:
		return fmt.Errorf("fetch worklogs: %w", err)
	default:
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fetch worklogs: %w", err)
	}
	return nil
}

type worklogResponse struct {
	StartAt int           `json:"startAt"`
	Total   int           `json:"total"`
	Items   []jiraWorklog `json:"worklogs"`
}

type jiraWorklog struct {
	Author           jiraUser `json:"author"`
	Started          string   `json:"started"`
	TimeSpentSeconds int64    `json:"timeSpentSeconds"`
}

func (c *Client) issueWorklogs(ctx context.Context, issueKey string, period model.Period) ([]model.Worklog, error) {
	startAt := 0
	var worklogs []model.Worklog
	for {
		query := url.Values{}
		query.Set("startAt", strconv.Itoa(startAt))
		query.Set("maxResults", strconv.Itoa(pageSize))
		query.Set("startedAfter", strconv.FormatInt(period.Start.Add(-time.Millisecond).UnixMilli(), 10))
		query.Set("startedBefore", strconv.FormatInt(period.End.UnixMilli(), 10))

		path := "/rest/api/3/issue/" + url.PathEscape(issueKey) + "/worklog"
		var response worklogResponse
		if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Items {
			started, err := parseJiraTime(item.Started)
			if err != nil {
				return nil, fmt.Errorf("parse worklog start %q: %w", item.Started, err)
			}
			worklogs = append(worklogs, model.Worklog{
				Author:  user(item.Author),
				Started: started,
				Seconds: item.TimeSpentSeconds,
			})
		}
		startAt += len(response.Items)
		if startAt >= response.Total || len(response.Items) == 0 {
			break
		}
	}
	return worklogs, nil
}

func user(item jiraUser) model.User {
	return model.User{AccountID: item.AccountID, Name: item.DisplayName}
}

func parseJiraTime(value string) (time.Time, error) {
	formats := []string{time.RFC3339Nano, "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05-0700"}
	for _, format := range formats {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported Jira timestamp")
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	for attempt := 0; attempt < 3; attempt++ {
		endpoint := c.baseURL + path
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.SetBasicAuth(c.email, c.token)
		req.Header.Set("Accept", "application/json")
		if input != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		response, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		closeErr := response.Body.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if response.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			wait := retryDelay(response.Header.Get("Retry-After"))
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			message := strings.TrimSpace(string(responseBody))
			if len(message) > 500 {
				message = message[:500] + "..."
			}
			return fmt.Errorf("request to Jira returned %s: %s", response.Status, message)
		}
		if output == nil || len(responseBody) == 0 {
			return nil
		}
		if err := json.Unmarshal(responseBody, output); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}
	return fmt.Errorf("rate limit from Jira persisted after retries")
}

func retryDelay(raw string) time.Duration {
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 1 {
		return time.Second
	}
	if seconds > 30 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}
