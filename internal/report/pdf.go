package report

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"jira-project/internal/analytics"
)

//go:embed fonts/DejaVuSansCondensed.ttf
var regularFont []byte

//go:embed fonts/DejaVuSansCondensed-Bold.ttf
var boldFont []byte

//go:embed fonts/DejaVuSansCondensed-Oblique.ttf
var italicFont []byte

const (
	pageWidth   = 210.0
	pageHeight  = 297.0
	pageMargin  = 14.0
	footerLimit = pageHeight - 16.0
)

type pdfReport struct {
	pdf *fpdf.Fpdf
}

type chartBar struct {
	label string
	value float64
	note  string
}

// WritePDF writes the same analytics shown in the console to a printable file.
func WritePDF(path string, result analytics.Result, jql string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return "", err
	}
	// #nosec G302 -- this is a directory mode; generated report files use 0600.
	if err := os.Chmod(filepath.Dir(absPath), 0o700); err != nil {
		return "", err
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("DejaVu", "", append([]byte(nil), regularFont...))
	pdf.AddUTF8FontFromBytes("DejaVu", "B", append([]byte(nil), boldFont...))
	pdf.AddUTF8FontFromBytes("DejaVu", "I", append([]byte(nil), italicFont...))
	report := &pdfReport{pdf: pdf}
	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.SetAutoPageBreak(true, 16)
	pdf.SetTitle("Engineering Sprint Activity Report", true)
	pdf.SetAuthor("jira-project", true)
	pdf.SetCreator("jira-project", true)
	pdf.SetCreationDate(time.Now())
	pdf.AliasNbPages("{pages}")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont("DejaVu", "", 8)
		pdf.SetTextColor(100, 100, 100)
		pdf.CellFormat(0, 4, fmt.Sprintf("Page %d of {pages}", pdf.PageNo()), "", 0, "C", false, 0, "")
	})

	report.writeOverview(result, jql)
	report.writeBreakdowns(result)
	report.writeEngineerDetails(result)
	report.writeDataQuality(result)

	temporary, err := os.CreateTemp(filepath.Dir(absPath), ".engineering-report-*.pdf")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", err
	}
	defer os.Remove(temporaryPath)

	if err := pdf.OutputFileAndClose(temporaryPath); err != nil {
		return "", err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, absPath); err != nil {
		return "", err
	}
	return absPath, nil
}

func (r *pdfReport) writeOverview(result analytics.Result, jql string) {
	r.pdf.AddPage()
	r.pdf.SetTextColor(25, 25, 25)
	r.pdf.SetFont("DejaVu", "B", 18)
	r.pdf.CellFormat(0, 9, r.text("Engineering Sprint Activity Report"), "", 1, "L", false, 0, "")
	r.pdf.SetFont("DejaVu", "", 9)
	r.pdf.SetTextColor(80, 80, 80)
	r.pdf.CellFormat(0, 5, r.text(fmt.Sprintf("Period: %s to %s (%d business days)",
		result.Period.Start.Format("02 Jan 2006"),
		result.Period.End.AddDate(0, 0, -1).Format("02 Jan 2006"),
		result.BusinessDays,
	)), "", 1, "L", false, 0, "")
	if strings.TrimSpace(jql) != "" {
		r.pdf.SetFont("DejaVu", "", 8)
		r.pdf.MultiCell(0, 4, r.text("JQL: "+oneLine(jql)), "", "L", false)
	}
	r.pdf.Ln(2)

	r.section("Team summary")
	metrics := [][]string{
		{"Resources with logged time", fmt.Sprintf("%d", result.ResourcesWithTime), "Tracked resources", fmt.Sprintf("%d", len(result.Engineers))},
		{"Sprint utilization", hours(result.ActualSeconds), "Issues worked", fmt.Sprintf("%d / %d", result.WorkedIssueCount, result.IssueCount)},
		{"Planned estimate", hours(result.PlannedSeconds), "Plan variance", signedHours(result.ActualSeconds - result.PlannedSeconds)},
	}
	r.table([]string{"Metric", "Value", "Metric", "Value"}, metrics, []float64{48, 43, 48, 43}, []string{"L", "R", "L", "R"})
	r.pdf.Ln(3)

	r.barChart("Planned estimate and recorded sprint work", []chartBar{
		{label: "Planned", value: toHours(result.PlannedSeconds), note: hours(result.PlannedSeconds)},
		{label: "Sprint work", value: toHours(result.ActualSeconds), note: hours(result.ActualSeconds)},
	}, 10)

	categoryBars := make([]chartBar, 0, len(result.Categories))
	for _, category := range result.Categories {
		categoryBars = append(categoryBars, chartBar{
			label: category.Name,
			value: toHours(category.ActualSeconds),
			note:  fmt.Sprintf("%s  %.1f%%", hours(category.ActualSeconds), category.ShareOfActual),
		})
	}
	r.barChart("Recorded time by work type", categoryBars, 10)

	projectBars := make([]chartBar, 0, len(result.Projects))
	for _, project := range result.Projects {
		projectBars = append(projectBars, chartBar{
			label: project.Name,
			value: toHours(project.ActualSeconds),
			note:  fmt.Sprintf("%s  %.1f%%", hours(project.ActualSeconds), project.ShareOfActual),
		})
	}
	r.barChart("Recorded time by project", projectBars, 10)
}

func (r *pdfReport) writeBreakdowns(result analytics.Result) {
	r.pdf.AddPage()
	r.section("Team capacity - tracked resource activity")
	engineerRows := make([][]string, 0, len(result.Engineers))
	engineerBars := make([]chartBar, 0, len(result.Engineers))
	for _, engineer := range result.Engineers {
		engineerRows = append(engineerRows, []string{
			engineer.Name,
			hours(engineer.ActualSeconds),
			fmt.Sprintf("%.1f%%", engineer.ShareOfSprint),
			hours(engineer.PlannedSeconds),
			signedHours(engineer.ActualSeconds - engineer.PlannedSeconds),
			fmt.Sprintf("%d", engineer.WorkedIssueCount),
		})
		engineerBars = append(engineerBars, chartBar{
			label: engineer.Name,
			value: toHours(engineer.ActualSeconds),
			note:  hours(engineer.ActualSeconds),
		})
	}
	r.table(
		[]string{"Resource", "Time spent", "Sprint share", "Planned", "Variance", "Issues worked"},
		engineerRows,
		[]float64{44, 27, 29, 27, 27, 28},
		[]string{"L", "R", "R", "R", "R", "R"},
	)
	r.pdf.Ln(4)
	r.barChart("Recorded hours by resource", engineerBars, 10)
	r.pdf.Ln(6)

	r.section("Work type distribution")
	categoryRows := make([][]string, 0, len(result.Categories))
	for _, category := range result.Categories {
		categoryRows = append(categoryRows, []string{
			category.Name,
			hours(category.ActualSeconds),
			hours(category.PlannedSeconds),
			fmt.Sprintf("%.1f%%", category.ShareOfActual),
			fmt.Sprintf("%d", category.IssueCount),
		})
	}
	r.table(
		[]string{"Category", "Time spent", "Planned", "Sprint share", "Issues"},
		categoryRows,
		[]float64{55, 32, 32, 38, 25},
		[]string{"L", "R", "R", "R", "R"},
	)
	r.pdf.Ln(4)

	r.section("Project distribution")
	projectRows := make([][]string, 0, len(result.Projects))
	for _, project := range result.Projects {
		projectRows = append(projectRows, []string{
			project.Name,
			hours(project.ActualSeconds),
			hours(project.PlannedSeconds),
			fmt.Sprintf("%.1f%%", project.ShareOfActual),
			fmt.Sprintf("%d", project.IssueCount),
		})
	}
	r.table(
		[]string{"Project", "Time spent", "Planned", "Sprint share", "Issues"},
		projectRows,
		[]float64{55, 32, 32, 38, 25},
		[]string{"L", "R", "R", "R", "R"},
	)
}

func (r *pdfReport) writeEngineerDetails(result analytics.Result) {
	if result.ResourcesWithTime == 0 {
		return
	}
	r.pdf.AddPage()
	for index, engineer := range result.Engineers {
		if engineer.ActualSeconds == 0 {
			continue
		}
		if index > 0 {
			r.pdf.Ln(5)
			r.ensureSpace(32)
		}
		r.section("What " + engineer.Name + " worked on")
		r.pdf.SetFont("DejaVu", "", 9)
		r.pdf.SetTextColor(70, 70, 70)
		r.pdf.CellFormat(0, 5, r.text(fmt.Sprintf(
			"Time spent %s | Planned %s | Sprint share %.1f%% | Issues worked %d",
			hours(engineer.ActualSeconds),
			hours(engineer.PlannedSeconds),
			engineer.ShareOfSprint,
			engineer.WorkedIssueCount,
		)), "", 1, "L", false, 0, "")
		r.pdf.Ln(2)

		rows := make([][]string, 0, len(engineer.Activities))
		for _, activity := range engineer.Activities {
			rows = append(rows, []string{
				activity.IssueKey,
				activity.ProjectKey,
				fmt.Sprintf("%s / %s", activity.IssueType, activity.Category),
				hours(activity.ActualSeconds),
				hours(activity.PlannedSeconds),
				activity.Summary,
			})
		}
		if len(rows) == 0 {
			r.pdf.SetFont("DejaVu", "", 9)
			r.pdf.SetTextColor(35, 35, 35)
			r.pdf.CellFormat(0, 6, r.text("No assigned or logged issues in this report."), "", 1, "L", false, 0, "")
			continue
		}
		r.table(
			[]string{"Key", "Project", "Type / category", "Time spent", "Planned", "Summary"},
			rows,
			[]float64{25, 19, 34, 19, 19, 66},
			[]string{"L", "L", "L", "R", "R", "L"},
		)
	}
}

func (r *pdfReport) writeDataQuality(result analytics.Result) {
	r.ensureSpace(55)
	r.pdf.Ln(5)
	r.section("Data quality")
	qualityRows := [][]string{
		{"Issues without original estimate", fmt.Sprintf("%d", result.IssuesNoEstimate)},
		{"Issues without worklogs in period", fmt.Sprintf("%d", result.IssuesNoWorklogs)},
		{"Unassigned issues", fmt.Sprintf("%d", result.UnassignedIssues)},
		{"Planned time without an assignee", hours(result.UnassignedPlanned)},
		{"Logged time without an author", hours(result.UnattributedSeconds)},
	}
	r.table([]string{"Check", "Result"}, qualityRows, []float64{130, 52}, []string{"L", "R"})
	r.pdf.Ln(3)
	r.pdf.SetFont("DejaVu", "I", 8)
	r.pdf.SetTextColor(80, 80, 80)
	r.pdf.MultiCell(0, 4, r.text("Sprint utilization is total Jira worklog time in the reporting period. Team capacity compares planned estimates by assignee with recorded time by worklog author."), "", "L", false)
}

func (r *pdfReport) section(title string) {
	r.ensureSpace(12)
	r.pdf.SetTextColor(25, 25, 25)
	r.pdf.SetFont("DejaVu", "B", 12)
	r.pdf.CellFormat(0, 7, r.text(title), "", 1, "L", false, 0, "")
	r.pdf.SetDrawColor(150, 150, 150)
	y := r.pdf.GetY()
	r.pdf.Line(pageMargin, y, pageWidth-pageMargin, y)
	r.pdf.Ln(3)
}

func (r *pdfReport) barChart(title string, bars []chartBar, limit int) {
	if len(bars) == 0 {
		return
	}
	shown := bars
	if limit > 0 && len(shown) > limit {
		shown = shown[:limit]
	}
	needed := 15 + float64(len(shown))*7
	r.ensureSpace(needed)
	r.pdf.SetTextColor(35, 35, 35)
	r.pdf.SetFont("DejaVu", "B", 10)
	r.pdf.CellFormat(0, 6, r.text(title), "", 1, "L", false, 0, "")

	maxValue := 0.0
	for _, bar := range shown {
		if bar.value > maxValue {
			maxValue = bar.value
		}
	}
	if maxValue == 0 {
		maxValue = 1
	}

	labelWidth := 43.0
	barWidth := 94.0
	for _, bar := range shown {
		y := r.pdf.GetY()
		r.pdf.SetFont("DejaVu", "", 8)
		r.pdf.SetTextColor(45, 45, 45)
		r.pdf.CellFormat(labelWidth, 6, r.text(r.fit(bar.label, labelWidth-2)), "", 0, "L", false, 0, "")
		r.pdf.SetFillColor(228, 231, 234)
		r.pdf.Rect(pageMargin+labelWidth, y+1.3, barWidth, 3.5, "F")
		filled := barWidth * bar.value / maxValue
		r.pdf.SetFillColor(70, 96, 122)
		r.pdf.Rect(pageMargin+labelWidth, y+1.3, filled, 3.5, "F")
		r.pdf.SetXY(pageMargin+labelWidth+barWidth+3, y)
		r.pdf.CellFormat(42, 6, r.text(bar.note), "", 1, "L", false, 0, "")
	}
	if len(shown) < len(bars) {
		r.pdf.SetFont("DejaVu", "I", 7)
		r.pdf.SetTextColor(90, 90, 90)
		r.pdf.CellFormat(0, 4, r.text(fmt.Sprintf("Chart shows the first %d entries; the table contains all entries.", len(shown))), "", 1, "L", false, 0, "")
	}
	r.pdf.Ln(3)
}

func (r *pdfReport) table(headers []string, rows [][]string, widths []float64, alignments []string) {
	r.writeTableRow(headers, widths, alignments, true)
	for _, row := range rows {
		height := r.tableRowHeight(row, widths)
		if r.pdf.GetY()+height > footerLimit {
			r.pdf.AddPage()
			r.writeTableRow(headers, widths, alignments, true)
		}
		r.writeTableRow(row, widths, alignments, false)
	}
}

func (r *pdfReport) writeTableRow(cells []string, widths []float64, alignments []string, header bool) {
	height := r.tableRowHeight(cells, widths)
	x, y := r.pdf.GetXY()
	if header {
		r.pdf.SetFont("DejaVu", "B", 8)
		r.pdf.SetFillColor(225, 228, 231)
	} else {
		r.pdf.SetFont("DejaVu", "", 8)
		r.pdf.SetFillColor(255, 255, 255)
	}
	r.pdf.SetTextColor(30, 30, 30)
	r.pdf.SetDrawColor(180, 180, 180)

	for index, cell := range cells {
		width := widths[index]
		align := "L"
		if index < len(alignments) {
			align = alignments[index]
		}
		r.pdf.Rect(x, y, width, height, "DF")
		r.pdf.SetXY(x+1.5, y+1.2)
		r.pdf.MultiCell(width-3, 4, r.text(oneLine(cell)), "", align, false)
		x += width
	}
	r.pdf.SetXY(pageMargin, y+height)
}

func (r *pdfReport) tableRowHeight(cells []string, widths []float64) float64 {
	maxLines := 1
	for index, cell := range cells {
		if index >= len(widths) {
			break
		}
		lines := r.pdf.SplitText(r.text(oneLine(cell)), widths[index]-3)
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	return float64(maxLines)*4 + 2.4
}

func (r *pdfReport) ensureSpace(height float64) {
	if r.pdf.GetY()+height > footerLimit {
		r.pdf.AddPage()
	}
}

func (r *pdfReport) fit(value string, width float64) string {
	value = oneLine(value)
	if r.pdf.GetStringWidth(r.text(value)) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 3 {
		runes = runes[:len(runes)-1]
		shortened := strings.TrimSpace(string(runes))
		if r.pdf.GetStringWidth(r.text(shortened+"...")) <= width {
			return shortened + "..."
		}
	}
	return "..."
}

func (r *pdfReport) text(value string) string {
	return value
}

func toHours(seconds int64) float64 {
	return float64(seconds) / 3600
}
