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
	UpdatedAt     time.Time          `json:"updatedAt"`
	Source        string             `json:"source"`
	Stale         bool               `json:"stale"`
	Warning       string             `json:"warning,omitempty"`
	SelectedSpace string             `json:"selectedSpace,omitempty"`
	Spaces        []spacePayload     `json:"spaces"`
	Period        periodPayload      `json:"period"`
	Summary       summaryPayload     `json:"summary"`
	Engineers     []engineerPayload  `json:"engineers"`
	Categories    []breakdownPayload `json:"categories"`
	Projects      []breakdownPayload `json:"projects"`
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
	for _, category := range result.Categories {
		response.Categories = append(response.Categories, makeBreakdown(category))
	}
	for _, project := range result.Projects {
		response.Projects = append(response.Projects, makeBreakdown(project))
	}
	return response
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
