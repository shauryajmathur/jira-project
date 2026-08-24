package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLoadDemoDefaults(t *testing.T) {
	clearJiraEnvironment(t)
	t.Setenv("DEMO", "true")
	t.Setenv("ANALYTICS_TIMEZONE", "UTC")
	t.Setenv("ANALYTICS_START", "2026-08-03")
	t.Setenv("ANALYTICS_END", "2026-08-07")

	cfg, err := Load(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Demo {
		t.Fatal("expected demo mode")
	}
	if days := int(cfg.Period.End.Sub(cfg.Period.Start).Hours() / 24); days != 5 {
		t.Fatalf("period contains %d days, want 5", days)
	}
	if cfg.JQL != "sprint in openSprints() ORDER BY key" {
		t.Fatalf("default JQL = %q", cfg.JQL)
	}
}

func TestLoadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	contents := "# local settings\nTEST_CLOUD_ID=abc-123\nTEST_JQL='project = ENG AND sprint = 42'\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_CLOUD_ID", "shell-wins")
	t.Cleanup(func() { _ = os.Unsetenv("TEST_JQL") })

	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TEST_CLOUD_ID"); got != "shell-wins" {
		t.Fatalf("TEST_CLOUD_ID = %q, want shell-wins", got)
	}
	if got := os.Getenv("TEST_JQL"); got != "project = ENG AND sprint = 42" {
		t.Fatalf("TEST_JQL = %q", got)
	}
}

func TestLoadEnvFileRejectsBroadPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("TEST_VALUE=private\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvFile(path); err == nil {
		t.Fatal("expected an error for a group-readable .env")
	}
}

func TestLoadRejectsPartialCredentials(t *testing.T) {
	clearJiraEnvironment(t)
	t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
	if _, err := Load(time.Now()); err == nil {
		t.Fatal("expected an error for partial Jira credentials")
	}
}

func TestLoadRejectsMissingCredentials(t *testing.T) {
	clearJiraEnvironment(t)
	if _, err := Load(time.Now()); err == nil {
		t.Fatal("expected an error without credentials or DEMO=true")
	}
}

func TestLoadRejectsNonLoopbackDashboardAddress(t *testing.T) {
	clearJiraEnvironment(t)
	t.Setenv("DEMO", "true")
	t.Setenv("DASHBOARD_ADDR", "0.0.0.0:8080")
	if _, err := Load(time.Now()); err == nil {
		t.Fatal("expected an error for a non-loopback dashboard address")
	}
}

func TestPeriodFromDatesUsesInclusiveEnd(t *testing.T) {
	location, err := time.LoadLocation("Asia/Dubai")
	if err != nil {
		t.Fatal(err)
	}
	period, err := PeriodFromDates("2026-08-03", "2026-08-14", location)
	if err != nil {
		t.Fatal(err)
	}
	if got := period.Start.Format("2006-01-02"); got != "2026-08-03" {
		t.Fatalf("start = %s", got)
	}
	if got := period.End.Format("2006-01-02"); got != "2026-08-15" {
		t.Fatalf("exclusive end = %s, want 2026-08-15", got)
	}
	if period.Start.Location() != location || period.End.Location() != location {
		t.Fatal("period did not retain the analytics timezone")
	}
}

func TestPeriodFromDatesRejectsInvalidRange(t *testing.T) {
	if _, err := PeriodFromDates("2026-08-15", "2026-08-14", time.UTC); err == nil {
		t.Fatal("expected an error when start is after end")
	}
}

func clearJiraEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"DEMO",
		"JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_API_TOKEN", "JIRA_JQL",
		"ANALYTICS_START", "ANALYTICS_END", "ANALYTICS_TIMEZONE",
		"JIRA_PARALLELISM", "REQUEST_TIMEOUT_SECONDS", "DASHBOARD_ADDR",
	} {
		t.Setenv(name, "")
	}
}
