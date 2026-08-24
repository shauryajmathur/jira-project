package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"jira-project/internal/config"
	"jira-project/internal/model"
)

func TestDashboardRoutes(t *testing.T) {
	handler := New(testConfig(), filepath.Join(t.TempDir(), "report.pdf"))

	index := httptest.NewRecorder()
	handler.ServeHTTP(index, dashboardRequest(http.MethodGet, "/"))
	if index.Code != http.StatusOK || !bytes.Contains(index.Body.Bytes(), []byte("Sprint activity")) {
		t.Fatalf("index response = %d, %q", index.Code, index.Body.String())
	}
	if !bytes.Contains(index.Body.Bytes(), []byte("All spaces")) || bytes.Contains(index.Body.Bytes(), []byte("Data quality")) || bytes.Contains(index.Body.Bytes(), []byte("Jira query and calculation notes")) || bytes.Contains(index.Body.Bytes(), []byte("Observed utilization")) || bytes.Contains(index.Body.Bytes(), []byte("100% capacity")) {
		t.Fatal("dashboard index does not contain the expected space filter and removals")
	}
	if !bytes.Contains(index.Body.Bytes(), []byte("contributor-table")) || bytes.Contains(index.Body.Bytes(), []byte("Tracked assignees and worklog authors")) || bytes.Contains(index.Body.Bytes(), []byte("Sprint utilisation uses Jira worklogs")) || bytes.Contains(index.Body.Bytes(), []byte("<span class=\"sync-label\">Source</span>")) || bytes.Contains(index.Body.Bytes(), []byte("Read-only Jira reporting")) || bytes.Contains(index.Body.Bytes(), []byte("<footer")) {
		t.Fatal("dashboard index is missing the contributor table or still contains removed copy")
	}
	if index.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("dashboard response is missing its content security policy")
	}
	if index.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatal("dashboard response is missing its same-origin resource policy")
	}

	api := httptest.NewRecorder()
	handler.ServeHTTP(api, dashboardRequest(http.MethodGet, "/api/report"))
	if api.Code != http.StatusOK {
		t.Fatalf("API response = %d, %q", api.Code, api.Body.String())
	}
	var response payload
	if err := json.NewDecoder(api.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Source != "demo data" || response.Summary.TrackedResources != 4 {
		t.Fatalf("unexpected dashboard payload: source %q, resources %d", response.Source, response.Summary.TrackedResources)
	}
	if api.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("report API must not be cached")
	}
	if response.Summary.SprintUtilizationHours <= 0 || response.Summary.ResourcesWithTime != 3 || len(response.Categories) != 5 {
		t.Fatalf("dashboard payload is missing analytics: %+v", response.Summary)
	}
	resourceHours := 0.0
	for _, resource := range response.Engineers {
		resourceHours += resource.TimeSpentHours
	}
	if resourceHours != response.Summary.SprintUtilizationHours {
		t.Fatalf("resource hours %.1f do not reconcile to sprint utilization %.1f", resourceHours, response.Summary.SprintUtilizationHours)
	}
	if len(response.Spaces) != 4 || response.SelectedSpace != "" {
		t.Fatalf("unexpected space options: selected %q, spaces %+v", response.SelectedSpace, response.Spaces)
	}
	if len(response.Engineers) != 4 {
		t.Fatalf("engineers = %d, want all 4 tracked resources", len(response.Engineers))
	}
	plannedOnly := false
	for _, engineer := range response.Engineers {
		if engineer.ID == "" {
			t.Fatalf("engineer payload is missing its stable ID: %+v", engineer)
		}
		if engineer.Name == "Noah Patel" && engineer.TimeSpentHours == 0 && engineer.PlannedHours == 16 {
			plannedOnly = true
		}
	}
	if !plannedOnly {
		t.Fatal("dashboard payload omitted the planned-only tracked resource")
	}

	periodAPI := httptest.NewRecorder()
	handler.ServeHTTP(periodAPI, dashboardRequest(http.MethodGet, "/api/report?start=2026-07-01&end=2026-07-31"))
	if periodAPI.Code != http.StatusOK {
		t.Fatalf("period API response = %d, %q", periodAPI.Code, periodAPI.Body.String())
	}
	var periodResponse payload
	if err := json.NewDecoder(periodAPI.Body).Decode(&periodResponse); err != nil {
		t.Fatal(err)
	}
	if periodResponse.Period.Start != "2026-07-01" || periodResponse.Period.End != "2026-07-31" {
		t.Fatalf("period response = %+v", periodResponse.Period)
	}

	invalidPeriod := httptest.NewRecorder()
	handler.ServeHTTP(invalidPeriod, dashboardRequest(http.MethodGet, "/api/report?start=2026-08-03"))
	if invalidPeriod.Code != http.StatusBadRequest {
		t.Fatalf("invalid period response = %d, want %d", invalidPeriod.Code, http.StatusBadRequest)
	}

	spaceAPI := httptest.NewRecorder()
	handler.ServeHTTP(spaceAPI, dashboardRequest(http.MethodGet, "/api/report?space=API"))
	if spaceAPI.Code != http.StatusOK {
		t.Fatalf("space API response = %d, %q", spaceAPI.Code, spaceAPI.Body.String())
	}
	var spaceResponse payload
	if err := json.NewDecoder(spaceAPI.Body).Decode(&spaceResponse); err != nil {
		t.Fatal(err)
	}
	if spaceResponse.SelectedSpace != "API" || spaceResponse.Summary.IssueCount != 2 || len(spaceResponse.Projects) != 1 || spaceResponse.Projects[0].Name != "API" {
		t.Fatalf("unexpected API space payload: %+v", spaceResponse)
	}

	unknownSpace := httptest.NewRecorder()
	handler.ServeHTTP(unknownSpace, dashboardRequest(http.MethodGet, "/api/report?space=UNKNOWN"))
	if unknownSpace.Code != http.StatusBadRequest {
		t.Fatalf("unknown space response = %d, want %d", unknownSpace.Code, http.StatusBadRequest)
	}

	pdf := httptest.NewRecorder()
	handler.ServeHTTP(pdf, dashboardRequest(http.MethodGet, "/report.pdf"))
	if pdf.Code != http.StatusOK || !bytes.HasPrefix(pdf.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("PDF response = %d, %d bytes", pdf.Code, pdf.Body.Len())
	}
	if disposition := pdf.Header().Get("Content-Disposition"); disposition == "" {
		t.Fatal("PDF response is missing its download filename")
	}

	spacePDF := httptest.NewRecorder()
	handler.ServeHTTP(spacePDF, dashboardRequest(http.MethodGet, "/report.pdf?space=API"))
	if spacePDF.Code != http.StatusOK || !bytes.HasPrefix(spacePDF.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("space PDF response = %d, %d bytes", spacePDF.Code, spacePDF.Body.Len())
	}
	if disposition := spacePDF.Header().Get("Content-Disposition"); !bytes.Contains([]byte(disposition), []byte("-API.pdf")) {
		t.Fatalf("space PDF filename = %q", disposition)
	}

	periodPDF := httptest.NewRecorder()
	handler.ServeHTTP(periodPDF, dashboardRequest(http.MethodGet, "/report.pdf?start=2026-07-01&end=2026-07-31"))
	if periodPDF.Code != http.StatusOK || !bytes.HasPrefix(periodPDF.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("period PDF response = %d, %d bytes", periodPDF.Code, periodPDF.Body.Len())
	}

	remoteHost := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://attacker.example/api/report", nil)
	handler.ServeHTTP(remoteHost, request)
	if remoteHost.Code != http.StatusForbidden {
		t.Fatalf("remote Host response = %d, want %d", remoteHost.Code, http.StatusForbidden)
	}
}

func dashboardRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Host = "127.0.0.1:8080"
	return request
}

func testConfig() config.Config {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	return config.Config{
		Demo:           true,
		JQL:            "sprint in openSprints() ORDER BY key",
		Period:         model.Period{Start: start, End: start.AddDate(0, 0, 10)},
		Parallelism:    2,
		RequestTimeout: 2 * time.Second,
	}
}
