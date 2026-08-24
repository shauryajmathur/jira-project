# Jira engineering analytics

This Go program reads issues and worklogs from Jira Cloud, calculates sprint activity metrics, and serves the result as a live dashboard, terminal report, and printable PDF.

## Run it

1. Copy `.env.example` to `.env` and keep it private:

```bash
cp .env.example .env
chmod 600 .env
```

2. Add your Jira API base URL, Atlassian account email, and API token.
3. Set the JQL and reporting dates. The default JQL selects all open sprints.
4. Start the dashboard:

```bash
go run . dashboard
```

Open `http://127.0.0.1:8080`. Choose the reporting dates and press **Apply dates** to recalculate the dashboard. The page refreshes from Jira every five minutes. Use **Refresh Jira** for an immediate update. **Download PDF** uses the current dashboard period and space.

To print the report in the terminal and write the PDF without starting the dashboard, run:

```bash
go run .
```

To generate the terminal report and PDF for a specific inclusive date range:

```bash
go run . report --start 2026-08-03 --end 2026-08-14
```

The PDF is written to:

```text
output/pdf/engineering-utilization-report.pdf
```

The terminal also prints the absolute file path. On macOS, open it from the terminal with:

```bash
open output/pdf/engineering-utilization-report.pdf
```

For a normal API token, use a base URL such as `https://your-company.atlassian.net`. A scoped API token uses `https://api.atlassian.com/ex/jira/<cloud-id>` instead.

Missing credentials are treated as a configuration error. To intentionally run the local sample report, set `DEMO=true` and leave the three Jira credential fields empty.

## What the report contains

- Work performed by each engineer
- Actual time split across Stories, Bugs, Tech Debt, Support, and Other
- Planned estimates compared with logged time
- Recorded and planned hours for every tracked resource, including assignees with no logged time
- Sprint utilization as total recorded Jira time in the reporting period
- Actual and planned time split across Jira projects
- Data-quality checks for missing estimates, missing worklogs, and unattributed time
- Summary, work-type, project, and resource-hours charts

## Calculations

```text
planned effort = sum of Jira original estimates
actual effort = sum of Jira worklogs inside the reporting period
sprint utilization = total actual effort recorded in the reporting period
resource activity = planned effort by assignee + actual effort by worklog author
plan variance = actual effort - planned effort
```

Planned time belongs to the issue assignee. Recorded time belongs to the worklog author. The program discovers resources from those two Jira fields; it does not maintain a separate team list. The Team capacity section shows every resource discovered from assignments or worklogs and clearly identifies tracked people with no logged time. It is an activity view, not a claim about contractual availability or productivity.

## Project layout

```text
main.go                    application flow
internal/config            .env loading and validation
internal/jira              Jira REST API client and pagination
internal/model             Jira-independent data types
internal/analytics         calculations and work classification
internal/snapshot          shared Jira-to-analytics loading flow
internal/dashboard         live web dashboard and JSON endpoint
internal/report            terminal and PDF output
internal/sample            local demonstration data
```

## Verify it

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Set `DASHBOARD_ADDR` to another localhost or loopback address if the default `127.0.0.1:8080` address does not suit your machine. The server rejects public bind addresses and non-local Host headers because the dashboard contains company Jira data.

Sprint utilization depends on accurate Jira worklogs. Jira permissions and worklog visibility determine what the API can return, and enhanced JQL search can briefly lag recent Jira changes. Resources who do not appear as an assignee or worklog author in the selected issues remain outside the report, so a completely inactive person cannot be discovered without a separate roster.
