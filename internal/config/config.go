// Package config loads and validates runtime settings from environment variables.
package config

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"jira-project/internal/model"
)

type Config struct {
	Demo           bool
	JiraBaseURL    string
	JiraEmail      string
	JiraToken      string
	JQL            string
	Period         model.Period
	Parallelism    int
	RequestTimeout time.Duration
	DashboardAddr  string
}

// Load reads the app configuration from .env and the process environment.
func Load(now time.Time) (Config, error) {
	if err := loadEnvFile(".env"); err != nil {
		return Config{}, err
	}

	locationName := value("ANALYTICS_TIMEZONE", "UTC")
	location, err := time.LoadLocation(locationName)
	if err != nil {
		return Config{}, fmt.Errorf("invalid ANALYTICS_TIMEZONE %q: %w", locationName, err)
	}

	period, err := loadPeriod(now.In(location), location)
	if err != nil {
		return Config{}, err
	}

	parallelism, err := intValue("JIRA_PARALLELISM", 6)
	if err != nil || parallelism < 1 || parallelism > 20 {
		return Config{}, fmt.Errorf("JIRA_PARALLELISM must be between 1 and 20")
	}
	timeoutSeconds, err := intValue("REQUEST_TIMEOUT_SECONDS", 120)
	if err != nil || timeoutSeconds < 1 {
		return Config{}, fmt.Errorf("REQUEST_TIMEOUT_SECONDS must be positive")
	}
	dashboardAddr := value("DASHBOARD_ADDR", "127.0.0.1:8080")
	if err := validateDashboardAddr(dashboardAddr); err != nil {
		return Config{}, err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("JIRA_BASE_URL")), "/")
	email := strings.TrimSpace(os.Getenv("JIRA_EMAIL"))
	token := strings.TrimSpace(os.Getenv("JIRA_API_TOKEN"))
	demo, err := boolValue("DEMO", false)
	if err != nil {
		return Config{}, fmt.Errorf("DEMO must be true or false")
	}
	credentialsSet := baseURL != "" || email != "" || token != ""
	if credentialsSet && (baseURL == "" || email == "" || token == "") {
		return Config{}, fmt.Errorf("JIRA_BASE_URL, JIRA_EMAIL, and JIRA_API_TOKEN must all be set")
	}
	if demo && credentialsSet {
		return Config{}, fmt.Errorf("DEMO cannot be combined with Jira credentials")
	}
	if !demo && !credentialsSet {
		return Config{}, fmt.Errorf("credentials for Jira are required; set DEMO=true to use sample data")
	}

	jql := strings.TrimSpace(os.Getenv("JIRA_JQL"))
	if jql == "" {
		jql = "sprint in openSprints() ORDER BY key"
	}

	return Config{
		Demo:           demo,
		JiraBaseURL:    baseURL,
		JiraEmail:      email,
		JiraToken:      token,
		JQL:            jql,
		Period:         period,
		Parallelism:    parallelism,
		RequestTimeout: time.Duration(timeoutSeconds) * time.Second,
		DashboardAddr:  dashboardAddr,
	}, nil
}

// loadEnvFile adds values from a local .env without replacing variables that
// were already set by the shell or test environment.
func loadEnvFile(path string) error {
	// #nosec G304 -- production passes the fixed local .env path; tests use temporary files.
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	if runtime.GOOS != "windows" {
		info, err := file.Stat()
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%s must not be readable or writable by other users; run chmod 600 %s", path, path)
		}
	}

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, rawValue, found := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !found || !validEnvironmentName(name) {
			return fmt.Errorf("%s:%d: expected NAME=value", path, lineNumber)
		}
		value, err := environmentValue(strings.TrimSpace(rawValue))
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, lineNumber, err)
		}
		if _, alreadySet := os.LookupEnv(name); !alreadySet {
			if err := os.Setenv(name, value); err != nil {
				return fmt.Errorf("set %s: %w", name, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func validateDashboardAddr(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("DASHBOARD_ADDR must use host:port format")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("DASHBOARD_ADDR must use a port between 1 and 65535")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("DASHBOARD_ADDR must use localhost or a loopback IP address")
	}
	return nil
}

func environmentValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	first := value[0]
	if first != '\'' && first != '"' {
		return value, nil
	}
	if len(value) < 2 || value[len(value)-1] != first {
		return "", fmt.Errorf("value has an unmatched quote")
	}
	return value[1 : len(value)-1], nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if character == '_' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' {
			continue
		}
		if index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func loadPeriod(now time.Time, location *time.Location) (model.Period, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	defaultEnd := today.AddDate(0, 0, 1)
	defaultStart := defaultEnd.AddDate(0, 0, -14)

	start, err := dateValue("ANALYTICS_START", defaultStart, location)
	if err != nil {
		return model.Period{}, err
	}
	endDay, err := dateValue("ANALYTICS_END", defaultEnd.AddDate(0, 0, -1), location)
	if err != nil {
		return model.Period{}, err
	}
	end := endDay.AddDate(0, 0, 1) // ANALYTICS_END is inclusive for humans.
	if !start.Before(end) {
		return model.Period{}, fmt.Errorf("ANALYTICS_START must not be after ANALYTICS_END")
	}
	return model.Period{Start: start, End: end}, nil
}

// PeriodFromDates parses an inclusive human date range in the configured
// analytics timezone. Internally Period.End remains exclusive.
func PeriodFromDates(startDate, endDate string, location *time.Location) (model.Period, error) {
	start, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(startDate), location)
	if err != nil {
		return model.Period{}, fmt.Errorf("start date must use YYYY-MM-DD")
	}
	endDay, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(endDate), location)
	if err != nil {
		return model.Period{}, fmt.Errorf("end date must use YYYY-MM-DD")
	}
	end := endDay.AddDate(0, 0, 1)
	if !start.Before(end) {
		return model.Period{}, fmt.Errorf("start date must not be after end date")
	}
	return model.Period{Start: start, End: end}, nil
}

func dateValue(name string, fallback time.Time, location *time.Location) (time.Time, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must use YYYY-MM-DD", name)
	}
	return parsed, nil
}

func intValue(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}

func boolValue(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	return strconv.ParseBool(raw)
}

func value(name, fallback string) string {
	if result := strings.TrimSpace(os.Getenv(name)); result != "" {
		return result
	}
	return fallback
}
