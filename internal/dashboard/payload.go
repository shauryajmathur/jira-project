package dashboard

import (
	"sort"
	"strings"
	"time"

	"jira-project/internal/analytics"
	"jira-project/internal/model"
	"jira-project/internal/snapshot"
)

type payload struct {
	UpdatedAt      time.Time               `json:"updatedAt"`
	Source         string                  `json:"source"`
	Stale          bool                    `json:"stale"`
	Warning        string                  `json:"warning,omitempty"`
	SelectedSpace  string                  `json:"selectedSpace,omitempty"`
	Spaces         []spacePayload          `json:"spaces"`
	Period         periodPayload           `json:"period"`
	Summary        summaryPayload          `json:"summary"`
	Engineers      []engineerPayload       `json:"engineers"`
	ActivityPeople []activityPersonPayload `json:"activityPeople"`
	ActivityTypes  []activityTypePayload   `json:"activityTypes"`
	Categories     []breakdownPayload      `json:"categories"`
	Projects       []breakdownPayload      `json:"projects"`
}

type spacePayload struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	IssueCount int    `json:"issueCount"`
}

type periodPayload struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	BusinessDays int    `json:"businessDays"`
}

type summaryPayload struct {
	TrackedResources       int     `json:"trackedResources"`
	ResourcesWithTime      int     `json:"resourcesWithTime"`
	IssueCount             int     `json:"issueCount"`
	WorkedIssueCount       int     `json:"workedIssueCount"`
	SprintUtilizationHours float64 `json:"sprintUtilizationHours"`
	PlannedHours           float64 `json:"plannedHours"`
	VarianceHours          float64 `json:"varianceHours"`
	UnattributedHours      float64 `json:"unattributedHours"`
	UnassignedPlannedHours float64 `json:"unassignedPlannedHours"`
}

type engineerPayload struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	TimeSpentHours       float64           `json:"timeSpentHours"`
	PlannedHours         float64           `json:"plannedHours"`
	VarianceHours        float64           `json:"varianceHours"`
	ShareOfSprintPercent float64           `json:"shareOfSprintPercent"`
	WorkedIssueCount     int               `json:"workedIssueCount"`
	Activities           []activityPayload `json:"activities"`
}

type activityPayload struct {
	EngineerID   string  `json:"engineerId"`
	Engineer     string  `json:"engineer"`
	IssueKey     string  `json:"issueKey"`
	Summary      string  `json:"summary"`
	IssueType    string  `json:"issueType"`
	Category     string  `json:"category"`
	ProjectKey   string  `json:"projectKey"`
	ActualHours  float64 `json:"actualHours"`
	PlannedHours float64 `json:"plannedHours"`
}

type activityPersonPayload struct {
	ID            string                     `json:"id"`
	Name          string                     `json:"name"`
	ActivityCount int                        `json:"activityCount"`
	Types         []activityTypeCountPayload `json:"types"`
}

type activityTypeCountPayload struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type activityTypePayload struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Count       int    `json:"count"`
}

type breakdownPayload struct {
	Name          string  `json:"name"`
	ActualHours   float64 `json:"actualHours"`
	PlannedHours  float64 `json:"plannedHours"`
	ShareOfActual float64 `json:"shareOfActual"`
	IssueCount    int     `json:"issueCount"`
}

func makePayload(data snapshot.Data, allIssues []model.Issue, selectedSpace string, updatedAt time.Time, stale bool, warning string) payload {
	result := data.Result
	response := payload{
		UpdatedAt:     updatedAt,
		Source:        data.Source,
		Stale:         stale,
		Warning:       warning,
		SelectedSpace: selectedSpace,
		Spaces:        makeSpaces(allIssues),
		Period: periodPayload{
			Start:        result.Period.Start.Format("2006-01-02"),
			End:          result.Period.End.AddDate(0, 0, -1).Format("2006-01-02"),
			BusinessDays: result.BusinessDays,
		},
		Summary: summaryPayload{
			TrackedResources:       len(result.Engineers),
			ResourcesWithTime:      result.ResourcesWithTime,
			IssueCount:             result.IssueCount,
			WorkedIssueCount:       result.WorkedIssueCount,
			SprintUtilizationHours: hours(result.ActualSeconds),
			PlannedHours:           hours(result.PlannedSeconds),
			VarianceHours:          hours(result.ActualSeconds - result.PlannedSeconds),
			UnattributedHours:      hours(result.UnattributedSeconds),
			UnassignedPlannedHours: hours(result.UnassignedPlanned),
		},
	}

	for _, engineer := range result.Engineers {
		response.Engineers = append(response.Engineers, makeEngineer(engineer))
	}
	response.ActivityPeople, response.ActivityTypes = makeActivitySummary(data.Issues, result.Engineers, result.Period)
	for _, category := range result.Categories {
		response.Categories = append(response.Categories, makeBreakdown(category))
	}
	for _, project := range result.Projects {
		response.Projects = append(response.Projects, makeBreakdown(project))
	}
	return response
}

var activityDefinitions = []activityTypePayload{
	{ID: "issue-created", Label: "Created an issue", Description: "Created a Jira issue or ticket."},
	{ID: "work-logged", Label: "Logged work", Description: "Submitted one worklog entry."},
	{ID: "comment-added", Label: "Added a comment", Description: "Posted one comment on an issue."},
	{ID: "comment-edited", Label: "Edited a comment", Description: "Saved a later edit to an existing comment."},
	{ID: "status-changed", Label: "Changed status", Description: "Moved an issue to another workflow status."},
	{ID: "assignee-changed", Label: "Changed assignee", Description: "Assigned or reassigned an issue."},
	{ID: "sprint-changed", Label: "Changed sprint", Description: "Added, removed, or moved an issue between sprints."},
	{ID: "estimate-changed", Label: "Changed estimate", Description: "Updated an original or remaining estimate."},
	{ID: "priority-changed", Label: "Changed priority", Description: "Updated an issue's priority."},
	{ID: "attachment-changed", Label: "Changed attachment", Description: "Added or removed an attachment."},
	{ID: "link-changed", Label: "Changed issue link", Description: "Added or removed a relationship between issues."},
	{ID: "details-updated", Label: "Updated issue details", Description: "Changed another tracked issue field, such as its summary, description, labels, or components."},
}

type activityPersonBuilder struct {
	ID    string
	Name  string
	Count int
	Types map[string]int
}

func makeActivitySummary(issues []model.Issue, engineers []analytics.Engineer, period model.Period) ([]activityPersonPayload, []activityTypePayload) {
	people := make(map[string]*activityPersonBuilder)
	for _, engineer := range engineers {
		people[engineer.ID] = &activityPersonBuilder{ID: engineer.ID, Name: engineer.Name, Types: make(map[string]int)}
	}
	totals := make(map[string]int)
	add := func(actor model.User, occurred time.Time, kind string) {
		if kind == "" || !period.Contains(occurred) || actor.AccountID == "" && strings.TrimSpace(actor.Name) == "" {
			return
		}
		name := strings.TrimSpace(actor.Name)
		if name == "" {
			name = "Unknown user"
		}
		id := actor.AccountID
		if id == "" {
			id = "name:" + strings.ToLower(name)
		}
		person := people[id]
		if person == nil {
			person = &activityPersonBuilder{ID: id, Name: name, Types: make(map[string]int)}
			people[id] = person
		}
		person.Count++
		person.Types[kind]++
		totals[kind]++
	}

	for _, issue := range issues {
		add(issue.Creator, issue.Created, "issue-created")
		for _, comment := range issue.Comments {
			add(comment.Author, comment.Created, "comment-added")
			if comment.Updated.After(comment.Created) {
				add(comment.UpdateAuthor, comment.Updated, "comment-edited")
			}
		}
		for _, change := range issue.Changes {
			add(change.Author, change.Created, classifyChange(change.Fields))
		}
	}

	result := make([]activityPersonPayload, 0, len(people))
	for _, person := range people {
		value := activityPersonPayload{ID: person.ID, Name: person.Name, ActivityCount: person.Count, Types: []activityTypeCountPayload{}}
		for _, definition := range activityDefinitions {
			if count := person.Types[definition.ID]; count > 0 {
				value.Types = append(value.Types, activityTypeCountPayload{ID: definition.ID, Label: definition.Label, Count: count})
			}
		}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ActivityCount == result[j].ActivityCount {
			return result[i].Name < result[j].Name
		}
		return result[i].ActivityCount > result[j].ActivityCount
	})

	definitions := make([]activityTypePayload, len(activityDefinitions))
	copy(definitions, activityDefinitions)
	for index := range definitions {
		definitions[index].Count = totals[definitions[index].ID]
	}
	return result, definitions
}

func classifyChange(fields []string) string {
	changed := make(map[string]bool, len(fields))
	for _, field := range fields {
		changed[strings.ToLower(strings.TrimSpace(field))] = true
	}
	switch {
	case changed["comment"]:
		return ""
	case changed["worklogid"]:
		return "work-logged"
	case changed["status"]:
		return "status-changed"
	case changed["assignee"]:
		return "assignee-changed"
	case changed["sprint"]:
		return "sprint-changed"
	case changed["timeoriginalestimate"] || changed["timeestimate"] || changed["story points"] || changed["story point estimate"]:
		return "estimate-changed"
	case changed["priority"]:
		return "priority-changed"
	case changed["attachment"]:
		return "attachment-changed"
	case changed["issuelinks"] || changed["link"]:
		return "link-changed"
	default:
		return "details-updated"
	}
}

func makeSpaces(issues []model.Issue) []spacePayload {
	byKey := make(map[string]*spacePayload)
	for _, issue := range issues {
		key := strings.TrimSpace(issue.ProjectKey)
		if key == "" {
			key = "Unknown project"
		}
		space := byKey[key]
		if space == nil {
			name := strings.TrimSpace(issue.ProjectName)
			if name == "" {
				name = key
			}
			space = &spacePayload{Key: key, Name: name}
			byKey[key] = space
		}
		space.IssueCount++
	}
	spaces := make([]spacePayload, 0, len(byKey))
	for _, space := range byKey {
		spaces = append(spaces, *space)
	}
	sort.Slice(spaces, func(i, j int) bool { return spaces[i].Key < spaces[j].Key })
	return spaces
}

func makeEngineer(engineer analytics.Engineer) engineerPayload {
	value := engineerPayload{
		ID:                   engineer.ID,
		Name:                 engineer.Name,
		TimeSpentHours:       hours(engineer.ActualSeconds),
		PlannedHours:         hours(engineer.PlannedSeconds),
		VarianceHours:        hours(engineer.ActualSeconds - engineer.PlannedSeconds),
		ShareOfSprintPercent: engineer.ShareOfSprint,
		WorkedIssueCount:     engineer.WorkedIssueCount,
		Activities:           []activityPayload{},
	}
	for _, activity := range engineer.Activities {
		value.Activities = append(value.Activities, activityPayload{
			EngineerID:   engineer.ID,
			Engineer:     engineer.Name,
			IssueKey:     activity.IssueKey,
			Summary:      activity.Summary,
			IssueType:    activity.IssueType,
			Category:     string(activity.Category),
			ProjectKey:   activity.ProjectKey,
			ActualHours:  hours(activity.ActualSeconds),
			PlannedHours: hours(activity.PlannedSeconds),
		})
	}
	return value
}

func makeBreakdown(value analytics.Breakdown) breakdownPayload {
	return breakdownPayload{
		Name:          value.Name,
		ActualHours:   hours(value.ActualSeconds),
		PlannedHours:  hours(value.PlannedSeconds),
		ShareOfActual: value.ShareOfActual,
		IssueCount:    value.IssueCount,
	}
}

func hours(seconds int64) float64 {
	return float64(seconds) / 3600
}
