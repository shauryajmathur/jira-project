# Jira Engineering Analytics Dashboard

Jira Engineering Analytics Dashboard is a local Go application for reviewing engineering activity from Jira Cloud. It reads issues, estimates, and worklogs through the Jira REST API, then presents the same reporting data in a browser dashboard, a terminal summary, and printable PDFs.

The application is read-only with respect to Jira. It does not create, edit, or delete Jira data.

## What it provides

- A dashboard with reporting-period and project filters
- Sprint totals for recorded time, planned estimates, contributors, and worked issues
- Per-person planned and recorded hours, variance, sprint share, and issue-level drill-downs
- Work-type and project breakdowns
- Activity filters for contributors and activity buckets
- Data-quality counts for missing estimates, missing worklogs, unassigned issues, and unattributed worklogs
- A standard printable PDF based on the current dashboard scope
- Custom PDF generation through an authenticated Codex or Claude Code CLI
- A terminal version of the standard report for scripts and local inspection

## Prerequisites

- Go 1.26.6 or newer
- A Jira Cloud account with permission to read the issues and worklogs in the configured JQL scope
- An Atlassian account email and API token
- Optional: an installed and authenticated Codex or Claude Code CLI for custom PDFs

The normal Jira Cloud base URL is `https://your-company.atlassian.net`. Scoped Atlassian API tokens use a URL such as `https://api.atlassian.com/ex/jira/<cloud-id>`.

## Configuration

The application reads `.env` from the project directory. Create the file yourself and keep it private:

```bash
touch .env
chmod 600 .env
```

Add the required Jira settings and any optional overrides:

```dotenv
# Required
JIRA_BASE_URL=https://your-company.atlassian.net
JIRA_EMAIL=you@example.com
JIRA_API_TOKEN=replace-with-your-api-token

# Optional
JIRA_JQL=sprint in openSprints() ORDER BY key
ANALYTICS_TIMEZONE=UTC
ANALYTICS_START=2026-08-24
ANALYTICS_END=2026-09-06
JIRA_PARALLELISM=6
REQUEST_TIMEOUT_SECONDS=120
DASHBOARD_ADDR=127.0.0.1:8080
```

`JIRA_BASE_URL`, `JIRA_EMAIL`, and `JIRA_API_TOKEN` must all be set. 

The remaining settings are optional:

| Variable | Default | Purpose |
| --- | --- | --- |
| `JIRA_JQL` | `sprint in openSprints() ORDER BY key` | Selects the Jira issues included in the report. |
| `ANALYTICS_TIMEZONE` | `UTC` | Defines the timezone used to interpret reporting dates. |
| `ANALYTICS_START` | 13 days before today | Sets the first included reporting date in `YYYY-MM-DD` format. |
| `ANALYTICS_END` | Today | Sets the last included reporting date in `YYYY-MM-DD` format. |
| `JIRA_PARALLELISM` | `6` | Controls concurrent Jira detail requests. Accepted range: 1 to 20. |
| `REQUEST_TIMEOUT_SECONDS` | `120` | Limits Jira refresh and report requests. Must be positive. |
| `DASHBOARD_ADDR` | `127.0.0.1:8080` | Sets the local dashboard address. Only localhost or a loopback IP is accepted. |

Together, the default start and end dates form the latest 14-day inclusive reporting window. A supplied end date is also inclusive. Internally, the application converts it to an exclusive boundary before filtering worklogs.

Values already exported in the shell take precedence over matching entries in `.env`. On macOS and Linux, the application rejects a `.env` file that is readable or writable by other users.

## Run the dashboard

Start the local server from the project directory:

```bash
go run . dashboard
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). The first request loads data from Jira. The dashboard then reuses the current report for four minutes and checks for an update every five minutes. **Refresh Jira** forces a new fetch. If a later refresh fails, the dashboard can continue showing the last successful result for the same reporting period and marks it as stale.

Use the date controls to set an inclusive reporting range. The space selector filters the loaded issues to one Jira project without fetching the same issue set again. **Download PDF** uses the active dates and project selection.

Stop the server with `Control-C`.

## Generate the standard report

Run the default report without starting the dashboard:

```bash
go run .
```

To use a specific inclusive date range:

```bash
go run . report --start 2026-08-24 --end 2026-09-06
```

Both commands print the report in the terminal and write:

```text
output/pdf/engineering-utilization-report.pdf
```

On macOS, open the generated file with:

```bash
open output/pdf/engineering-utilization-report.pdf
```

## Generate a custom PDF

The dashboard can ask Codex or Claude Code to research the selected Jira scope and create a purpose-built PDF. This feature is optional; the standard dashboard and PDF do not depend on either agent CLI.

Install one of the supported CLIs, make sure it is on `PATH`, and authenticate it:

```bash
codex login
```

or:

```bash
claude auth login
```

Start the dashboard, choose the dates and project, then select **Custom PDF**. Choose an available agent, describe the report, and wait for the download link. Generated files are stored under `output/custom/<job-id>/`.

Custom generation runs in a temporary workspace with a read-only Jira tool set and a confined PDF writer. Jira credentials are placed in a private temporary configuration file, removed from the agent subprocess environment, and not inserted into the prompt. Jira issue text and comments are treated as untrusted source data. A job may create at most 20 PDFs with a combined size of 50 MB, and it times out after eight minutes.

## How calculations work

The dashboard, terminal output, and standard PDF use the same analytics result. There is no assumed daily capacity, eight-hour workday, employee schedule, or productivity score. The **Team capacity** section reports observed Jira estimates and worklogs only.

### Scope and reporting period

1. `JIRA_JQL` selects the Jira issues included in the report.
2. Choosing a space keeps only issues whose Jira project key matches that selection, then recalculates the report from that subset.
3. The start date is included from `00:00:00` in `ANALYTICS_TIMEZONE`. The human-facing end date is inclusive, so the internal upper boundary is `00:00:00` on the following day.
4. Worklogs, issue actions, comments, and changes count only when their timestamps fall inside that half-open interval: `start <= timestamp < end + 1 day`.
5. Original estimates are the current values on all selected issues. They are not reconstructed historically and are not restricted by worklog date.

Business days are the number of Monday-to-Friday dates in the selected interval. Weekends are excluded; public holidays are not.

### Summary calculations

All time is accumulated in seconds and converted at the response boundary using `hours = seconds / 3,600`.

| Displayed value | Calculation |
| --- | --- |
| Selected sprint issues | Number of issues returned by the JQL after the optional project filter. |
| Issues worked | Selected issues with at least one positive worklog inside the reporting period. Each issue counts once. |
| `Sprint utilisation` | Sum of every positive in-period worklog, including worklogs whose author is missing, divided by 3,600. |
| Planned estimate | Sum of the current original estimate on every selected issue, divided by 3,600. |
| Plan variance | `total actual hours - total planned hours`. A positive value means more time was logged than currently estimated. |
| Tracked resources | Unique assignees or worklog authors whose selected issues give them non-zero planned or actual time. |
| Resources with logged time | Tracked resources whose in-period actual time is greater than zero. |
| Issues without estimates | Selected issues whose original estimate is zero. |
| Issues without worklogs | Selected issues with no positive worklog inside the period. |
| Unassigned issues | Selected issues with no assignee account ID or display name. |
| Unassigned planned time | Original estimates summed from unassigned issues. |
| Unattributed time | Positive in-period worklogs with no author account ID or display name. This time remains in the sprint total but is not credited to a person. |

### Contributor calculations

Jira account ID is the primary person identifier. If it is missing, the application uses a normalized lowercase display name as a fallback.

| Contributor value | Calculation |
| --- | --- |
| Time spent | Sum of the contributor's positive in-period worklogs across selected issues. Work belongs to the worklog author, not the issue assignee. |
| Planned | Sum of current original estimates for selected issues assigned to the contributor. Planned time belongs to the assignee, even when someone else logged the work. |
| Variance | `contributor actual hours - contributor planned hours`. |
| Sprint share | `contributor actual seconds / total actual seconds x 100`. The result is `0%` when total actual time is zero. |
| Issues worked | Number of distinct selected issues on which the contributor logged positive time during the period. |
| Issue drill-down time | All of that contributor's qualifying worklogs on the same issue are added together. The issue estimate appears only for its assignee. |

The contributor work-type filter recalculates the visible hours as the sum of the shown issue rows. Its percentage is `visible filtered hours / that contributor's total actual hours x 100`. The filter keeps Story and Bug separate; Tech Debt, Support, and the base Other category are grouped under **Other** in this contributor-only view.

The application has no separate employee roster. Someone with no selected assignment, worklog, or qualifying recorded action cannot be discovered from the Jira data.

### Work-type and project calculations

Each issue is assigned to one work category. The application compares normalized issue types and labels in this order, so the first matching category wins:

| Category | Matching issue types or labels |
| --- | --- |
| Tech Debt | `tech debt`, `technical debt` |
| Bug | `bug`, `defect` |
| Support | `support`, `customer support`, `service request`, `incident`, `customer request` |
| Story | `story`, `user story`, `task`, `feature`, `improvement`, `epic` |
| Other | Anything that does not match the categories above |

Hyphens, the `_` character, repeated spaces, and letter case are ignored during classification.

For both category and project breakdowns:

- Actual hours are the sum of positive in-period worklogs on matching issues.
- Planned hours are the sum of current original estimates on matching issues.
- Share is `breakdown actual seconds / total actual seconds x 100`, or `0%` when no actual time exists.
- Issue count is the number of unique matching issue keys, regardless of how many worklogs they contain.

### Recorded Jira activity

The **Activities per person** section counts discrete Jira actions, not hours. Only actions inside the reporting period count. Each issue creation, comment addition, comment edit, or qualifying changelog record contributes one action to its actor.

Changelog records are classified by changed fields. Status, assignee, sprint, estimate or story-point, priority, attachment, and issue-link changes use their named buckets. A worklog-ID change counts as **Logged work**. Any other tracked field change counts as **Updated issue details**. A record containing a comment-field change is ignored before the other rules are checked because comments are counted separately from their created and updated timestamps.

One changelog record counts once even if it contains several changed fields. When several recognized fields occur in the same record, the first match in the source precedence is used: worklog, status, assignee, sprint, estimate, priority, attachment, link, then other details. Events without an identifiable actor are ignored.

A comment counts once as **Added a comment** at its creation time. If its update time is later than its creation time, it also counts once as **Edited a comment** at the update time. A person already present through planned or actual effort remains in the activity list with zero actions when none of these events qualify.

The numbered activity buckets group people by their exact action count. The number on a bucket is actions per person; the bucket size is the number of people with that count. Selecting a bucket lists those people and their per-action breakdowns.

### Display precision and limitations

Calculations retain seconds and unrounded percentages internally. The browser formats hours and percentages to one decimal place, so displayed rows may differ slightly from a total obtained by adding the rounded labels.

Report accuracy depends on Jira permissions, worklog visibility, current estimates, changelog availability, and issue metadata. Jira search may briefly lag recent changes. The report measures recorded Jira activity; it does not infer work completed outside Jira.

## HTTP endpoints

The dashboard exposes these local endpoints:

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/` | `GET` | Serves the embedded dashboard. |
| `/api/report` | `GET` | Returns dashboard data as JSON. Accepts `start`, `end`, `space`, and `refresh=1`. |
| `/report.pdf` | `GET` | Creates and downloads the standard PDF for the requested dates and project. |
| `/api/custom/status` | `GET` | Reports whether Codex or Claude Code is available. |
| `/api/custom` | `POST` | Queues a custom PDF job. |
| `/api/custom/{id}` | `GET` | Returns custom-job status and generated file metadata. |
| `/api/custom/{id}/files/{path}` | `GET` | Downloads an allowed file from a completed custom job. |
| `/healthz` | `GET` | Returns `ok` when the local server is running. |

The server accepts only local Host headers and loopback bind addresses. API and PDF responses are marked `no-store`, and the dashboard applies restrictive browser security headers. It is designed as a single-user local tool, not as a public web service.

## Project structure

| File | Description |
| --- | --- |
| `README.md` | Explains how to configure, run, and verify the project. |
| `.gitignore` | Excludes credentials, logs, binaries, temporary files, and generated reports. |
| `go.mod` | Declares the Go module, required Go version, and dependencies. |
| `go.sum` | Locks dependency checksums used by the Go toolchain. |
| `main.go` | Parses commands and starts the dashboard, terminal report, PDF report, or MCP server. |
| `internal/analytics/analytics.go` | Calculates time and estimate totals, contributor issue breakdowns, work categories, and project breakdowns. |
| `internal/config/config.go` | Loads `.env`, validates settings, and builds the reporting period. |
| `internal/customgen/agent.go` | Creates the restricted agent environment and validates generated PDF files. |
| `internal/customgen/claude.go` | Runs authenticated Claude Code custom-PDF jobs. |
| `internal/customgen/codex.go` | Runs authenticated Codex custom-PDF jobs. |
| `internal/customgen/customgen.go` | Queues custom jobs and tracks their status, limits, output, and downloads. |
| `internal/dashboard/assets/app.js` | Loads report data and controls dashboard filters, tables, drill-downs, refreshes, and custom jobs. |
| `internal/dashboard/assets/index.html` | Defines the dashboard page and custom-PDF dialog. |
| `internal/dashboard/assets/styles.css` | Styles the dashboard for desktop and mobile layouts. |
| `internal/dashboard/payload.go` | Builds the dashboard JSON response, including recorded action counts and activity types. |
| `internal/dashboard/server.go` | Serves the dashboard, report endpoints, cache, PDF downloads, and local security checks. |
| `internal/jira/client.go` | Reads and paginates Jira issues, worklogs, comments, and changelogs. |
| `internal/jira/research.go` | Provides broader raw Jira queries for custom-report research. |
| `internal/jiramcp/config.go` | Writes and reads owner-only temporary Jira configuration for agent jobs. |
| `internal/jiramcp/server.go` | Exposes read-only Jira research tools and the confined PDF writer over MCP. |
| `internal/model/model.go` | Defines shared periods, users, issues, worklogs, changes, and work categories. |
| `internal/report/console.go` | Formats the standard analytics report for the terminal. |
| `internal/report/custom_pdf.go` | Validates and renders structured custom-PDF documents. |
| `internal/report/pdf.go` | Renders the standard engineering report as a PDF. |
| `internal/report/fonts/DejaVuSansCondensed.ttf` | Supplies the regular embedded PDF font. |
| `internal/report/fonts/DejaVuSansCondensed-Bold.ttf` | Supplies the bold embedded PDF font. |
| `internal/report/fonts/DejaVuSansCondensed-Oblique.ttf` | Supplies the italic embedded PDF font. |
| `internal/report/fonts/LICENSE-DejaVu.txt` | Contains the license for the bundled DejaVu fonts. |
| `internal/snapshot/snapshot.go` | Loads Jira data, runs analytics, and recalculates project-specific views. |

The main data path is:

```text
configuration -> Jira issues and worklogs -> snapshot -> analytics -> dashboard, terminal, or PDF
```

## Build and verify

Build a standalone executable:

```bash
go build -o jira-project .
```

Run the repository checks:

```bash
go test ./...
go vet ./...
go build ./...
node --check internal/dashboard/assets/app.js
git diff --check
```

The repository currently contains no automated test files, so `go test ./...` verifies that every Go package compiles.
