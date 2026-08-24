# Final QA

Status: passed on 24 August 2026.

This audit covered the localhost dashboard, Jira ingestion, calculations, filters, PDF output, code quality, performance, and security. No release blocker remains within the project's read-only Jira reporting scope.

## Corrections made

- The Team Capacity view now includes every tracked assignee and worklog author. The current all-space report has 61 tracked resources, including 27 people with planned work and no logged time. Those rows are visible and expandable.
- Resource IDs remain distinct even when two Jira users have the same display name. The contributor filter no longer relies on the display name as its key.
- Planned, recorded, unassigned, and unattributed hours are labeled separately. The UI and reports no longer describe recorded time as theoretical availability.
- Space changes made during a Jira refresh keep the URL and PDF link current, discard the old-scope response, and load the requested scope next.
- Jira pagination now fails safely if Jira repeats a page token instead of looping indefinitely.
- Cached dashboard readers no longer wait behind an otherwise valid forced Jira refresh. PDF generation is atomic and concurrent downloads cannot read a partially written report.
- The dashboard only binds to localhost or a loopback IP. It rejects foreign Host headers, blocks browser caching for report data, and sends a strict CSP plus same-origin browser policies.
- `.env`, PDFs, and QA screenshots are owner-only. PDF generation uses private directories and `0600` files.
- Go was updated from 1.26.5 to 1.26.6 to pick up the current standard-library security fixes.
- The unrelated Firebase debug log, Finder metadata, and the stale MT report were moved to Trash. They can be recovered from there if needed.

## Automated checks

- 21 Go tests pass.
- `go test -race ./...` passes.
- `go vet ./...` passes.
- `go build ./...` passes.
- `staticcheck ./...` passes.
- `gosec ./...` passes with no findings.
- `govulncheck -show verbose ./...` reports no vulnerabilities.
- `node --check internal/dashboard/assets/app.js` passes.
- The code scan reports 0 findings and a slop score of 0.

## Live Jira reconciliation

The final all-space report used Jira Cloud data for 3 to 14 August 2026:

- 535 issues across 8 spaces
- 88 issues with worklogs in the period
- 61 tracked resources, 34 with logged time
- 1,151.25 recorded hours
- 5,018 planned hours
- 710 unassigned planned hours
- 0 unattributed recorded hours

Recorded hours reconcile exactly across the summary, resource rows, work categories, and projects. The CPP scope also passed independently with 53 issues, 16 tracked resources, 315.93 recorded hours, and 478 planned hours. An unknown space returns HTTP 400.

## Browser and PDF checks

- Desktop at 1440 px has no document overflow and no console errors or warnings.
- Mobile at 390 px has no document overflow. Wide tables scroll inside their panels.
- The live refresh and space-change race was reproduced. The final scope, URL, issue totals, and PDF link all resolved to CPP.
- Search returned the two CPP-7134 contributor activities and no unrelated rows.
- Invalid dates were rejected in the browser without changing the active report.
- The all-space PDF is a 17-page A4 document. The CPP PDF is 7 pages. Every page was rendered and visually checked for clipping, overlap, broken glyphs, table continuation, and page numbering.

Evidence:

- `output/playwright/final/final-desktop.png`
- `output/playwright/final/final-mobile.png`
- `output/playwright/final/planned-only-expanded.png`
- `output/pdf/engineering-utilization-report.pdf`
- `output/pdf/engineering-utilization-report-CPP.pdf`

## Reporting boundary

The dashboard can only report Jira data visible to the configured account. A person who is neither an assignee nor a worklog author in the selected issues cannot be discovered without a separate roster. Jira permissions, worklog visibility, original estimates, and short enhanced-search indexing delays still determine what is available to report.
