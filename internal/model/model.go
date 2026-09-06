// Package model defines the data shared by Jira ingestion and analytics.
package model

import "time"

// Period is a half-open reporting window: Start is included and End is excluded.
type Period struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether t falls inside the reporting window.
func (p Period) Contains(t time.Time) bool {
	return !t.Before(p.Start) && t.Before(p.End)
}

// User identifies a Jira user. AccountID is stable; Name is for display.
type User struct {
	AccountID string
	Name      string
}

// Worklog is one person's actual effort on an issue.
type Worklog struct {
	Author  User
	Started time.Time
	Seconds int64
}

type Comment struct {
	Author       User
	Created      time.Time
	UpdateAuthor User
	Updated      time.Time
}

type Change struct {
	Author  User
	Created time.Time
	Fields  []string
}

// Issue is the subset of a Jira issue required for sprint activity analytics.
type Issue struct {
	ID                      string
	Key                     string
	Summary                 string
	Type                    string
	ProjectKey              string
	ProjectName             string
	Assignee                User
	Creator                 User
	Created                 time.Time
	Updated                 time.Time
	Labels                  []string
	OriginalEstimateSeconds int64
	Worklogs                []Worklog
	Comments                []Comment
	Changes                 []Change
}

// Category is the normalized kind of engineering work.
type Category string

const (
	CategoryStory    Category = "Story"
	CategoryBug      Category = "Bug"
	CategoryTechDebt Category = "Tech Debt"
	CategorySupport  Category = "Support"
	CategoryOther    Category = "Other"
)

// CategoryOrder gives reports a predictable order.
var CategoryOrder = []Category{
	CategoryStory,
	CategoryBug,
	CategoryTechDebt,
	CategorySupport,
	CategoryOther,
}
