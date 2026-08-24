package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"jira-project/internal/model"
)

func TestFetchIssuesAndPaginatedWorklogs(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	period := model.Period{Start: start, End: start.AddDate(0, 0, 5)}

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "dev@acme.test" || password != "token" {
			t.Errorf("unexpected Basic authentication")
		}
		body := ""
		status := http.StatusOK
		switch request.URL.Path {
		case "/rest/api/3/search/jql":
			if request.Method != http.MethodPost {
				t.Errorf("search method = %s, want POST", request.Method)
			}
			var searchBody searchRequest
			if err := json.NewDecoder(request.Body).Decode(&searchBody); err != nil {
				t.Errorf("decode search request: %v", err)
			}
			if searchBody.JQL != "sprint = 42" {
				t.Errorf("JQL = %q", searchBody.JQL)
			}
			body = `{
				"isLast":true,
				"issues":[{"key":"APP-1","fields":{"summary":"Build report","issuetype":{"name":"Story"},"project":{"key":"APP"},"assignee":{"accountId":"u1","displayName":"Alex"},"labels":[],"timeoriginalestimate":14400}}]
			}`
		case "/rest/api/3/issue/APP-1/worklog":
			startAt, _ := strconv.Atoi(request.URL.Query().Get("startAt"))
			if startAt == 0 {
				body = `{"startAt":0,"total":2,"worklogs":[{"author":{"accountId":"u1","displayName":"Alex"},"started":"2026-08-03T09:00:00.000+0000","timeSpentSeconds":3600}]}`
			} else {
				body = `{"startAt":1,"total":2,"worklogs":[{"author":{"accountId":"u1","displayName":"Alex"},"started":"2026-08-04T09:00:00Z","timeSpentSeconds":7200}]}`
			}
		default:
			status = http.StatusNotFound
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})

	client := &Client{
		baseURL: "https://jira.test", email: "dev@acme.test", token: "token",
		parallelism: 2, httpClient: &http.Client{Transport: transport},
	}
	issues, err := client.FetchIssues(context.Background(), "sprint = 42", period)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || len(issues[0].Worklogs) != 2 {
		t.Fatalf("got %d issues and %d worklogs, want 1 and 2", len(issues), len(issues[0].Worklogs))
	}
	if issues[0].Worklogs[1].Seconds != 7200 {
		t.Fatalf("second worklog = %d seconds, want 7200", issues[0].Worklogs[1].Seconds)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNewClientRequiresSafeHTTPSURL(t *testing.T) {
	invalid := []string{
		"http://example.atlassian.net",
		"https://user@example.atlassian.net",
		"https://example.atlassian.net?debug=true",
		"https://example.atlassian.net#fragment",
	}
	for _, baseURL := range invalid {
		if _, err := NewClient(baseURL, "dev@acme.test", "token", 1); err == nil {
			t.Errorf("NewClient(%q) succeeded", baseURL)
		}
	}

	client, err := NewClient("https://api.atlassian.com/ex/jira/cloud-id", "dev@acme.test", "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if client.baseURL != "https://api.atlassian.com/ex/jira/cloud-id" {
		t.Fatalf("base URL = %q", client.baseURL)
	}
}

func TestAttachWorklogsReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &Client{parallelism: 1}
	if err := client.attachWorklogs(ctx, nil, model.Period{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestSearchIssuesRejectsRepeatedPaginationToken(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"isLast":false,"nextPageToken":"same","issues":[]}`)),
			Request:    request,
		}, nil
	})
	client := &Client{
		baseURL: "https://jira.test", email: "dev@acme.test", token: "token",
		parallelism: 1, httpClient: &http.Client{Transport: transport},
	}
	if _, err := client.searchIssues(context.Background(), "project = APP"); err == nil {
		t.Fatal("expected repeated pagination token to fail")
	}
}
