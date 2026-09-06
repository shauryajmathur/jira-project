package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jira-project/internal/config"
	"jira-project/internal/customgen"
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
	custom     *customgen.Manager
	now        func() time.Time

	mu        sync.RWMutex
	refreshMu sync.Mutex
	pdfMu     sync.Mutex
	data      snapshot.Data
	updatedAt time.Time
	period    model.Period
}

func New(cfg config.Config, outputPath string) http.Handler {
	return newHandler(cfg, outputPath, customgen.New(customOutputDir(outputPath), cfg))
}

func newHandler(cfg config.Config, outputPath string, custom *customgen.Manager) http.Handler {
	server := &Server{cfg: cfg, outputPath: outputPath, custom: custom, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/report", server.reportJSON)
	mux.HandleFunc("GET /api/custom/status", server.customStatus)
	mux.HandleFunc("POST /api/custom", server.createCustom)
	mux.HandleFunc("GET /api/custom/{id}", server.customJob)
	mux.HandleFunc("GET /api/custom/{id}/files/{path...}", server.customFile)
	mux.HandleFunc("GET /report.pdf", server.reportPDF)
	mux.HandleFunc("GET /healthz", server.health)

	assets, err := fs.Sub(assetFiles, "assets")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServerFS(assets))
	return securityHeaders(localRequestsOnly(mux))
}

type customRequest struct {
	Provider string `json:"provider"`
	Prompt   string `json:"prompt"`
	Space    string `json:"space"`
	Start    string `json:"start"`
	End      string `json:"end"`
}

func (s *Server) customStatus(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	writeJSON(writer, http.StatusOK, s.custom.Status(ctx))
}

func (s *Server) createCustom(writer http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "Cross-origin requests are not allowed"})
		return
	}
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 64<<10)
	var input customRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "Invalid custom request"})
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "Invalid custom request"})
		return
	}

	cfg, err := s.configForDates(input.Start, input.End)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	scope := strings.TrimSpace(input.Space)
	if scope == "" {
		scope = "All spaces"
	}
	if len(scope) > 120 || strings.ContainsAny(scope, "\r\n") {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "Invalid Jira space"})
		return
	}
	job, err := s.custom.Create(customgen.Request{
		Provider: input.Provider,
		Prompt:   input.Prompt,
		Scope:    scope,
		Start:    cfg.Period.Start.Format("2006-01-02"),
		End:      cfg.Period.End.AddDate(0, 0, -1).Format("2006-01-02"),
		JQL:      cfg.JQL,
	})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusAccepted, customJobResponse(job))
}

func (s *Server) customJob(writer http.ResponseWriter, request *http.Request) {
	job, ok := s.custom.Get(request.PathValue("id"))
	if !ok {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "Custom job not found"})
		return
	}
	writeJSON(writer, http.StatusOK, customJobResponse(job))
}

func (s *Server) customFile(writer http.ResponseWriter, request *http.Request) {
	file, info, err := s.custom.Open(request.PathValue("id"), request.PathValue("path"))
	if err != nil {
		http.Error(writer, "Custom file not found", http.StatusNotFound)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("close custom file: %v", err)
		}
	}()
	writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeDownloadName(filepath.Base(info.Name()))))
	http.ServeContent(writer, request, info.Name(), info.ModTime(), file)
}

func customJobResponse(job customgen.Job) customgen.Job {
	for index := range job.Files {
		job.Files[index].URL = "/api/custom/" + url.PathEscape(job.ID) + "/files/" + escapePath(job.Files[index].Name)
	}
	return job
}

func escapePath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func safeDownloadName(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	if name == "" {
		return "custom-file"
	}
	return name
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple JSON values")
	}
	return err
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
	return s.configForDates(request.URL.Query().Get("start"), request.URL.Query().Get("end"))
}

func (s *Server) configForDates(start, end string) (config.Config, error) {
	cfg := s.cfg
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
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

func customOutputDir(outputPath string) string {
	parent := filepath.Dir(outputPath)
	if filepath.Base(parent) == "pdf" {
		parent = filepath.Dir(parent)
	}
	return filepath.Join(parent, "custom")
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
		if request.URL.Path == "/api/report" || request.URL.Path == "/report.pdf" || strings.HasPrefix(request.URL.Path, "/api/custom") {
			writer.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(writer, request)
	})
}

func sameOrigin(request *http.Request) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}
	return strings.EqualFold(parsed.Host, request.Host)
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
