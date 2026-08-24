package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jira-project/internal/config"
	"jira-project/internal/model"
	"jira-project/internal/report"
	"jira-project/internal/snapshot"
)

const cacheLifetime = 4 * time.Minute

//go:embed assets/*
var assetFiles embed.FS

type Server struct {
	cfg        config.Config
	outputPath string
	now        func() time.Time

	mu        sync.RWMutex
	refreshMu sync.Mutex
	pdfMu     sync.Mutex
	data      snapshot.Data
	updatedAt time.Time
	period    model.Period
}

func New(cfg config.Config, outputPath string) http.Handler {
	server := &Server{cfg: cfg, outputPath: outputPath, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/report", server.reportJSON)
	mux.HandleFunc("GET /report.pdf", server.reportPDF)
	mux.HandleFunc("GET /healthz", server.health)

	assets, err := fs.Sub(assetFiles, "assets")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServerFS(assets))
	return securityHeaders(localRequestsOnly(mux))
}

func (s *Server) reportJSON(writer http.ResponseWriter, request *http.Request) {
	cfg, err := s.requestConfig(request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	force := request.URL.Query().Get("refresh") == "1"
	allData, updatedAt, stale, warning, err := s.load(request.Context(), cfg, force)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "Could not refresh Jira data"})
		return
	}
	data, selectedSpace, ok := s.selectSpace(allData, request.URL.Query().Get("space"), cfg)
	if !ok {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "Unknown Jira space"})
		return
	}
	writeJSON(writer, http.StatusOK, makePayload(data, allData.Issues, selectedSpace, updatedAt, stale, warning))
}

func (s *Server) reportPDF(writer http.ResponseWriter, request *http.Request) {
	cfg, err := s.requestConfig(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	allData, _, _, _, err := s.load(request.Context(), cfg, false)
	if err != nil {
		http.Error(writer, "Could not refresh Jira data", http.StatusBadGateway)
		return
	}
	data, selectedSpace, ok := s.selectSpace(allData, request.URL.Query().Get("space"), cfg)
	if !ok {
		http.Error(writer, "Unknown Jira space", http.StatusBadRequest)
		return
	}

	outputPath := scopedReportPath(s.outputPath, selectedSpace)
	s.pdfMu.Lock()
	path, err := report.WritePDF(outputPath, data.Result, data.JQL)
	if err != nil {
		s.pdfMu.Unlock()
		http.Error(writer, "Could not create PDF report", http.StatusInternalServerError)
		return
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		s.pdfMu.Unlock()
		http.Error(writer, "Could not open PDF report", http.StatusInternalServerError)
		return
	}
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		s.pdfMu.Unlock()
		_ = root.Close()
		http.Error(writer, "Could not open PDF report", http.StatusInternalServerError)
		return
	}
	info, err := file.Stat()
	s.pdfMu.Unlock()
	if err != nil {
		_ = file.Close()
		_ = root.Close()
		http.Error(writer, "Could not read PDF report", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("close PDF report: %v", err)
		}
		if err := root.Close(); err != nil {
			log.Printf("close PDF directory: %v", err)
		}
	}()

	writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(path)))
	http.ServeContent(writer, request, filepath.Base(path), info.ModTime(), file)
}

func (s *Server) selectSpace(data snapshot.Data, requested string, cfg config.Config) (snapshot.Data, string, bool) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return data, "", true
	}
	for _, project := range data.Result.Projects {
		if strings.EqualFold(project.Name, requested) {
			filtered, ok := snapshot.ForProject(data, cfg, project.Name)
			return filtered, project.Name, ok
		}
	}
	return snapshot.Data{}, "", false
}

func (s *Server) requestConfig(request *http.Request) (config.Config, error) {
	cfg := s.cfg
	start := strings.TrimSpace(request.URL.Query().Get("start"))
	end := strings.TrimSpace(request.URL.Query().Get("end"))
	if start == "" && end == "" {
		return cfg, nil
	}
	if start == "" || end == "" {
		return config.Config{}, fmt.Errorf("start and end dates must be provided together")
	}
	period, err := config.PeriodFromDates(start, end, cfg.Period.Start.Location())
	if err != nil {
		return config.Config{}, err
	}
	cfg.Period = period
	return cfg, nil
}

func scopedReportPath(basePath, space string) string {
	if space == "" {
		return basePath
	}
	var safe strings.Builder
	for _, character := range space {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			safe.WriteRune(character)
		}
	}
	if safe.Len() == 0 {
		return basePath
	}
	extension := filepath.Ext(basePath)
	name := strings.TrimSuffix(filepath.Base(basePath), extension)
	return filepath.Join(filepath.Dir(basePath), name+"-"+safe.String()+extension)
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte("ok\n"))
}

func (s *Server) load(ctx context.Context, cfg config.Config, force bool) (snapshot.Data, time.Time, bool, string, error) {
	now := s.now()
	s.mu.RLock()
	samePeriod := s.period.Start.Equal(cfg.Period.Start) && s.period.End.Equal(cfg.Period.End)
	if !force && samePeriod && !s.updatedAt.IsZero() && now.Sub(s.updatedAt) < cacheLifetime {
		data, updatedAt := s.data, s.updatedAt
		s.mu.RUnlock()
		return data, updatedAt, false, "", nil
	}
	startedWith := s.updatedAt
	s.mu.RUnlock()

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	now = s.now()
	s.mu.RLock()
	samePeriod = s.period.Start.Equal(cfg.Period.Start) && s.period.End.Equal(cfg.Period.End)
	cacheFresh := samePeriod && !s.updatedAt.IsZero() && now.Sub(s.updatedAt) < cacheLifetime
	refreshedWhileWaiting := !startedWith.Equal(s.updatedAt)
	if cacheFresh && (!force || refreshedWhileWaiting) {
		data, updatedAt := s.data, s.updatedAt
		s.mu.RUnlock()
		return data, updatedAt, false, "", nil
	}
	s.mu.RUnlock()

	requestCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	defer cancel()
	data, err := snapshot.Load(requestCtx, cfg)
	if err != nil {
		log.Printf("dashboard Jira refresh failed: %v", err)
		s.mu.RLock()
		defer s.mu.RUnlock()
		samePeriod = s.period.Start.Equal(cfg.Period.Start) && s.period.End.Equal(cfg.Period.End)
		if samePeriod && !s.updatedAt.IsZero() {
			return s.data, s.updatedAt, true, "Jira refresh failed; showing the last successful report.", nil
		}
		return snapshot.Data{}, time.Time{}, false, "", err
	}

	now = s.now()
	s.mu.Lock()
	s.data = data
	s.updatedAt = now
	s.period = cfg.Period
	s.mu.Unlock()
	return data, now, false, "", nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		writer.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.URL.Path == "/api/report" || request.URL.Path == "/report.pdf" {
			writer.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(writer, request)
	})
}

func localRequestsOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host := request.Host
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
		if strings.EqualFold(host, "localhost") {
			next.ServeHTTP(writer, request)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(writer, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
