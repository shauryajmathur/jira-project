// Package report renders analytics in a console-friendly format.
package report

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"jira-project/internal/analytics"
)

func PrintConsole(output io.Writer, result analytics.Result, mode, jql string) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)

	fmt.Fprintln(writer, "ENGINEERING SPRINT ACTIVITY REPORT")
	fmt.Fprintln(writer, strings.Repeat("=", 76))
	fmt.Fprintf(writer, "Source:\t%s\n", mode)
	fmt.Fprintf(writer, "Period:\t%s to %s (%d business days)\n",
		result.Period.Start.Format("02 Jan 2006"),
		result.Period.End.AddDate(0, 0, -1).Format("02 Jan 2006"),
		result.BusinessDays,
	)
	if mode != "demo data" {
		fmt.Fprintf(writer, "JQL:\t%s\n", oneLine(jql))
	}

	section(writer, "TEAM SUMMARY")
	fmt.Fprintf(writer, "Tracked resources:\t%d\n", len(result.Engineers))
	fmt.Fprintf(writer, "Resources with logged time:\t%d\n", result.ResourcesWithTime)
	fmt.Fprintf(writer, "Issues analyzed:\t%d\n", result.IssueCount)
	fmt.Fprintf(writer, "Issues worked:\t%d\n", result.WorkedIssueCount)
	fmt.Fprintf(writer, "Sprint utilization (logged work):\t%s\n", hours(result.ActualSeconds))
	fmt.Fprintf(writer, "Planned estimate:\t%s\n", hours(result.PlannedSeconds))
	fmt.Fprintf(writer, "Plan variance:\t%s\n", signedHours(result.ActualSeconds-result.PlannedSeconds))

	section(writer, "TEAM CAPACITY - TRACKED RESOURCE ACTIVITY")
	fmt.Fprintln(writer, "Resource\tTime spent\tSprint share\tPlanned\tVariance\tIssues worked")
	for _, engineer := range result.Engineers {
		fmt.Fprintf(writer, "%s\t%s\t%.1f%%\t%s\t%s\t%d\n",
			oneLine(engineer.Name),
			hours(engineer.ActualSeconds),
			engineer.ShareOfSprint,
			hours(engineer.PlannedSeconds),
			signedHours(engineer.ActualSeconds-engineer.PlannedSeconds),
			engineer.WorkedIssueCount,
		)
	}

	section(writer, "WORK TYPE DISTRIBUTION")
	fmt.Fprintln(writer, "Category\tTime spent\tPlanned\tSprint share\tIssues")
	for _, category := range result.Categories {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%.1f%%\t%d\n",
			category.Name, hours(category.ActualSeconds), hours(category.PlannedSeconds), category.ShareOfActual, category.IssueCount)
	}

	section(writer, "PROJECT DISTRIBUTION")
	fmt.Fprintln(writer, "Project\tTime spent\tPlanned\tSprint share\tIssues")
	for _, project := range result.Projects {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%.1f%%\t%d\n",
			project.Name, hours(project.ActualSeconds), hours(project.PlannedSeconds), project.ShareOfActual, project.IssueCount)
	}

	section(writer, "WHAT EACH ENGINEER WORKED ON")
	for _, engineer := range result.Engineers {
		if engineer.ActualSeconds == 0 {
			continue
		}
		fmt.Fprintf(writer, "%s (%s actual)\n", oneLine(engineer.Name), hours(engineer.ActualSeconds))
		if len(engineer.Activities) == 0 {
			fmt.Fprintln(writer, "  No assigned or logged issues in this report.")
			continue
		}
		fmt.Fprintln(writer, "  Key\tProject\tType / category\tTime spent\tPlanned\tSummary")
		for _, activity := range engineer.Activities {
			fmt.Fprintf(writer, "  %s\t%s\t%s / %s\t%s\t%s\t%s\n",
				activity.IssueKey,
				activity.ProjectKey,
				activity.IssueType,
				activity.Category,
				hours(activity.ActualSeconds),
				hours(activity.PlannedSeconds),
				oneLine(activity.Summary),
			)
		}
	}

	section(writer, "DATA QUALITY")
	fmt.Fprintf(writer, "Issues without original estimate:\t%d\n", result.IssuesNoEstimate)
	fmt.Fprintf(writer, "Issues without worklogs in period:\t%d\n", result.IssuesNoWorklogs)
	fmt.Fprintf(writer, "Unassigned issues:\t%d\n", result.UnassignedIssues)
	fmt.Fprintf(writer, "Planned time without an assignee:\t%s\n", hours(result.UnassignedPlanned))
	fmt.Fprintf(writer, "Logged time without an author:\t%s\n", hours(result.UnattributedSeconds))
	fmt.Fprintln(writer, "Note:\tSprint utilization is total Jira worklog time in the period. Team capacity compares planned estimates by assignee with recorded time by worklog author; it is not theoretical availability.")

	return writer.Flush()
}

func section(writer io.Writer, title string) {
	fmt.Fprintf(writer, "\n%s\n%s\n", title, strings.Repeat("-", len(title)))
}

func hours(seconds int64) string {
	return fmt.Sprintf("%.1fh", float64(seconds)/3600)
}

func signedHours(seconds int64) string {
	return fmt.Sprintf("%+.1fh", float64(seconds)/3600)
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
