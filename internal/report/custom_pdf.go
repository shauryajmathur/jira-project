package report

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

type CustomPDFSpec struct {
	Title      string             `json:"title" jsonschema:"Document title"`
	Subtitle   string             `json:"subtitle,omitempty" jsonschema:"Short scope or date subtitle"`
	Highlights []CustomPDFMetric  `json:"highlights,omitempty" jsonschema:"Important label and value pairs shown near the beginning"`
	Sections   []CustomPDFSection `json:"sections" jsonschema:"Ordered report sections"`
	SourceNote string             `json:"source_note,omitempty" jsonschema:"Short source or methodology note"`
}

type CustomPDFMetric struct {
	Label string `json:"label" jsonschema:"Metric label"`
	Value string `json:"value" jsonschema:"Metric value"`
}

type CustomPDFSection struct {
	Heading    string           `json:"heading" jsonschema:"Section heading"`
	Paragraphs []string         `json:"paragraphs,omitempty" jsonschema:"Body paragraphs"`
	Bullets    []string         `json:"bullets,omitempty" jsonschema:"Bullet points without bullet characters"`
	Tables     []CustomPDFTable `json:"tables,omitempty" jsonschema:"Tables shown after the section text"`
}

type CustomPDFTable struct {
	Title   string     `json:"title,omitempty" jsonschema:"Optional table title"`
	Headers []string   `json:"headers" jsonschema:"Column headings, one to six columns"`
	Rows    [][]string `json:"rows" jsonschema:"Rows with the same number of cells as headers"`
}

// WriteCustomPDF renders a structured agent result as a printable PDF.
func WriteCustomPDF(path string, spec CustomPDFSpec) (string, error) {
	if err := validateCustomPDF(spec); err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return "", err
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("DejaVu", "", append([]byte(nil), regularFont...))
	pdf.AddUTF8FontFromBytes("DejaVu", "B", append([]byte(nil), boldFont...))
	pdf.AddUTF8FontFromBytes("DejaVu", "I", append([]byte(nil), italicFont...))
	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.SetAutoPageBreak(true, 16)
	pdf.SetTitle(spec.Title, true)
	pdf.SetAuthor("Jira custom report", true)
	pdf.SetCreator("jira-project", true)
	pdf.SetCreationDate(time.Now())
	pdf.AliasNbPages("{pages}")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont("DejaVu", "", 8)
		pdf.SetTextColor(100, 100, 100)
		pdf.CellFormat(0, 4, fmt.Sprintf("Page %d of {pages}", pdf.PageNo()), "", 0, "C", false, 0, "")
	})

	document := &pdfReport{pdf: pdf}
	pdf.AddPage()
	pdf.SetTextColor(23, 43, 77)
	pdf.SetFont("DejaVu", "B", 19)
	pdf.MultiCell(0, 9, document.text(spec.Title), "", "L", false)
	if strings.TrimSpace(spec.Subtitle) != "" {
		pdf.SetFont("DejaVu", "", 9)
		pdf.SetTextColor(80, 95, 120)
		pdf.MultiCell(0, 5, document.text(spec.Subtitle), "", "L", false)
	}
	pdf.SetDrawColor(193, 199, 208)
	y := pdf.GetY() + 2
	pdf.Line(pageMargin, y, pageWidth-pageMargin, y)
	pdf.SetY(y + 5)

	if len(spec.Highlights) > 0 {
		rows := make([][]string, 0, len(spec.Highlights))
		for _, metric := range spec.Highlights {
			rows = append(rows, []string{metric.Label, metric.Value})
		}
		document.table([]string{"Measure", "Value"}, rows, []float64{112, 70}, []string{"L", "R"})
		pdf.Ln(5)
	}

	for _, section := range spec.Sections {
		document.section(section.Heading)
		for _, paragraph := range section.Paragraphs {
			pdf.SetFont("DejaVu", "", 9)
			pdf.SetTextColor(38, 50, 70)
			pdf.MultiCell(0, 5, document.text(strings.TrimSpace(paragraph)), "", "L", false)
			pdf.Ln(2)
		}
		for _, bullet := range section.Bullets {
			document.ensureSpace(7)
			pdf.SetFont("DejaVu", "", 9)
			pdf.SetTextColor(38, 50, 70)
			pdf.SetX(pageMargin + 2)
			pdf.MultiCell(pageWidth-2*pageMargin-2, 5, document.text("- "+strings.TrimSpace(bullet)), "", "L", false)
		}
		if len(section.Bullets) > 0 {
			pdf.Ln(2)
		}
		for _, table := range section.Tables {
			if strings.TrimSpace(table.Title) != "" {
				document.ensureSpace(12)
				pdf.SetFont("DejaVu", "B", 9)
				pdf.SetTextColor(42, 58, 82)
				pdf.CellFormat(0, 6, document.text(table.Title), "", 1, "L", false, 0, "")
			}
			width := (pageWidth - 2*pageMargin) / float64(len(table.Headers))
			widths := make([]float64, len(table.Headers))
			alignments := make([]string, len(table.Headers))
			for index := range widths {
				widths[index] = width
				alignments[index] = "L"
			}
			document.table(table.Headers, table.Rows, widths, alignments)
			pdf.Ln(4)
		}
	}

	if strings.TrimSpace(spec.SourceNote) != "" {
		document.ensureSpace(16)
		pdf.SetDrawColor(223, 225, 230)
		y := pdf.GetY() + 1
		pdf.Line(pageMargin, y, pageWidth-pageMargin, y)
		pdf.SetY(y + 3)
		pdf.SetFont("DejaVu", "I", 8)
		pdf.SetTextColor(90, 100, 116)
		pdf.MultiCell(0, 4, document.text(spec.SourceNote), "", "L", false)
	}

	temporary, err := os.CreateTemp(filepath.Dir(absPath), ".custom-report-*.pdf")
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

func validateCustomPDF(spec CustomPDFSpec) error {
	if strings.TrimSpace(spec.Title) == "" {
		return errors.New("PDF title is required")
	}
	if len(spec.Title) > 180 || len(spec.Subtitle) > 500 || len(spec.SourceNote) > 2000 {
		return errors.New("PDF title, subtitle, or source note is too long")
	}
	if len(spec.Highlights) > 20 || len(spec.Sections) == 0 || len(spec.Sections) > 30 {
		return errors.New("PDF must contain 1 to 30 sections and no more than 20 highlights")
	}
	totalCharacters := len(spec.Title) + len(spec.Subtitle) + len(spec.SourceNote)
	for _, metric := range spec.Highlights {
		if strings.TrimSpace(metric.Label) == "" || strings.TrimSpace(metric.Value) == "" {
			return errors.New("PDF highlights require both a label and a value")
		}
		totalCharacters += len(metric.Label) + len(metric.Value)
	}
	for _, section := range spec.Sections {
		if strings.TrimSpace(section.Heading) == "" {
			return errors.New("every PDF section requires a heading")
		}
		if len(section.Paragraphs) > 40 || len(section.Bullets) > 100 || len(section.Tables) > 12 {
			return errors.New("a PDF section exceeds its paragraph, bullet, or table limit")
		}
		totalCharacters += len(section.Heading)
		for _, paragraph := range section.Paragraphs {
			totalCharacters += len(paragraph)
		}
		for _, bullet := range section.Bullets {
			totalCharacters += len(bullet)
		}
		for _, table := range section.Tables {
			if len(table.Headers) == 0 || len(table.Headers) > 6 || len(table.Rows) > 1000 {
				return errors.New("PDF tables require 1 to 6 columns and no more than 1000 rows")
			}
			for _, header := range table.Headers {
				totalCharacters += len(header)
			}
			for _, row := range table.Rows {
				if len(row) != len(table.Headers) {
					return errors.New("every PDF table row must match its header count")
				}
				for _, cell := range row {
					totalCharacters += len(cell)
				}
			}
		}
	}
	if totalCharacters > 500_000 {
		return errors.New("PDF content exceeds the 500,000 character limit")
	}
	return nil
}
