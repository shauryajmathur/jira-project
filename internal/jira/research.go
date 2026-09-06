package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// SearchPage returns one Jira search page without discarding fields that are
// not used by the dashboard analytics.
func (c *Client) SearchPage(ctx context.Context, jql string, fields []string, maxResults int, nextPageToken string) (json.RawMessage, error) {
	request := searchRequest{
		JQL:           jql,
		MaxResults:    maxResults,
		Fields:        fields,
		NextPageToken: nextPageToken,
	}
	return c.rawJSON(ctx, http.MethodPost, "/rest/api/3/search/jql", nil, request)
}

// Issue returns the requested issue fields and expansions as Jira supplied them.
func (c *Client) Issue(ctx context.Context, key string, fields, expand []string) (json.RawMessage, error) {
	query := url.Values{}
	if len(fields) > 0 {
		query.Set("fields", strings.Join(fields, ","))
	}
	if len(expand) > 0 {
		query.Set("expand", strings.Join(expand, ","))
	}
	return c.rawJSON(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key), query, nil)
}

// IssueCommentsPage returns one page of comments for an issue.
func (c *Client) IssueCommentsPage(ctx context.Context, key string, startAt, maxResults int) (json.RawMessage, error) {
	query := pageQuery(startAt, maxResults)
	query.Set("orderBy", "created")
	return c.rawJSON(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/comment", query, nil)
}

// IssueWorklogsPage returns one page of worklogs for an issue.
func (c *Client) IssueWorklogsPage(ctx context.Context, key string, startAt, maxResults int, startedAfter, startedBefore int64) (json.RawMessage, error) {
	query := pageQuery(startAt, maxResults)
	if startedAfter > 0 {
		query.Set("startedAfter", strconv.FormatInt(startedAfter, 10))
	}
	if startedBefore > 0 {
		query.Set("startedBefore", strconv.FormatInt(startedBefore, 10))
	}
	return c.rawJSON(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/worklog", query, nil)
}

// IssueChangelogPage returns one page of changelog entries for an issue.
func (c *Client) IssueChangelogPage(ctx context.Context, key string, startAt, maxResults int) (json.RawMessage, error) {
	return c.rawJSON(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/changelog", pageQuery(startAt, maxResults), nil)
}

// ProjectsPage returns one page of Jira projects visible to the configured account.
func (c *Client) ProjectsPage(ctx context.Context, startAt, maxResults int, queryText string) (json.RawMessage, error) {
	query := pageQuery(startAt, maxResults)
	query.Set("orderBy", "key")
	if queryText != "" {
		query.Set("query", queryText)
	}
	return c.rawJSON(ctx, http.MethodGet, "/rest/api/3/project/search", query, nil)
}

// FieldsPage returns one page of Jira fields so an agent can discover custom fields.
func (c *Client) FieldsPage(ctx context.Context, startAt, maxResults int, queryText string) (json.RawMessage, error) {
	query := pageQuery(startAt, maxResults)
	if queryText != "" {
		query.Set("query", queryText)
	}
	return c.rawJSON(ctx, http.MethodGet, "/rest/api/3/field/search", query, nil)
}

func pageQuery(startAt, maxResults int) url.Values {
	query := url.Values{}
	query.Set("startAt", strconv.Itoa(startAt))
	query.Set("maxResults", strconv.Itoa(maxResults))
	return query
}

func (c *Client) rawJSON(ctx context.Context, method, path string, query url.Values, input any) (json.RawMessage, error) {
	var output json.RawMessage
	if err := c.doJSON(ctx, method, path, query, input, &output); err != nil {
		return nil, err
	}
	return output, nil
}
