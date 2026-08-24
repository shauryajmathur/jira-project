package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"jira-project/internal/config"
	"jira-project/internal/dashboard"
	"jira-project/internal/report"
	"jira-project/internal/snapshot"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(time.Now())
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return runReport(cfg)
	}
	if args[0] == "report" {
		reportFlags := flag.NewFlagSet("report", flag.ContinueOnError)
		reportFlags.SetOutput(io.Discard)
		start := reportFlags.String("start", "", "report start date (YYYY-MM-DD)")
		end := reportFlags.String("end", "", "report end date, inclusive (YYYY-MM-DD)")
		if err := reportFlags.Parse(args[1:]); err != nil || reportFlags.NArg() != 0 {
			return fmt.Errorf("usage: jira-project report [--start YYYY-MM-DD --end YYYY-MM-DD]")
		}
		if (*start == "") != (*end == "") {
			return fmt.Errorf("--start and --end must be provided together")
		}
		if *start != "" {
			period, err := config.PeriodFromDates(*start, *end, cfg.Period.Start.Location())
			if err != nil {
				return err
			}
			cfg.Period = period
		}
		return runReport(cfg)
	}
	if len(args) == 1 && args[0] == "dashboard" {
		return runDashboard(cfg)
	}
	return fmt.Errorf("usage: jira-project [report [--start YYYY-MM-DD --end YYYY-MM-DD]|dashboard]")
}

func runReport(cfg config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.RequestTimeout)
	defer cancel()

	data, err := snapshot.Load(ctx, cfg)
	if err != nil {
		return err
	}
	if err := report.PrintConsole(os.Stdout, data.Result, data.Source, data.JQL); err != nil {
		return err
	}

	pdfPath := filepath.Join("output", "pdf", "engineering-utilization-report.pdf")
	savedPath, err := report.WritePDF(pdfPath, data.Result, data.JQL)
	if err != nil {
		return fmt.Errorf("write PDF report: %w", err)
	}
	fmt.Fprintf(os.Stdout, "\nPDF report saved: %s\n", savedPath)
	return nil
}

func runDashboard(cfg config.Config) error {
	outputPath := filepath.Join("output", "pdf", "engineering-utilization-report.pdf")
	handler := dashboard.New(cfg, outputPath)
	server := &http.Server{
		Addr:              cfg.DashboardAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 15*time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	fmt.Fprintf(os.Stdout, "Engineering dashboard: http://%s\n", cfg.DashboardAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
