package customgen

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jira-project/internal/config"
)

const maxPromptLength = 8000

type Capability struct {
	Ready     bool             `json:"ready"`
	Message   string           `json:"message"`
	Providers []ProviderStatus `json:"providers"`
}

type ProviderStatus struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

type Request struct {
	Provider string
	Prompt   string
	Scope    string
	Start    string
	End      string
	JQL      string
}

type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url,omitempty"`
}

type Job struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	ProviderName string    `json:"providerName"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Summary      string    `json:"summary,omitempty"`
	Error        string    `json:"error,omitempty"`
	Files        []File    `json:"files"`
}

type Result struct {
	Summary string
	Files   []File
}

type Generator interface {
	ID() string
	Name() string
	Status(context.Context) ProviderStatus
	Generate(context.Context, Request, string) (Result, error)
}

type ReadSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

type Manager struct {
	baseDir    string
	generators map[string]Generator
	order      []string
	now        func() time.Time
	queue      chan struct{}

	mu   sync.RWMutex
	jobs map[string]Job
}

func New(baseDir string, cfg config.Config) *Manager {
	command, err := os.Executable()
	if err != nil {
		command = os.Args[0]
	}
	return NewWithGenerators(baseDir, NewCodex(command, cfg), NewClaude(command, cfg))
}

func NewWithGenerator(baseDir string, generator Generator) *Manager {
	return NewWithGenerators(baseDir, generator)
}

func NewWithGenerators(baseDir string, generators ...Generator) *Manager {
	byID := make(map[string]Generator, len(generators))
	order := make([]string, 0, len(generators))
	for _, generator := range generators {
		if generator == nil || strings.TrimSpace(generator.ID()) == "" {
			continue
		}
		byID[generator.ID()] = generator
		order = append(order, generator.ID())
	}
	return &Manager{
		baseDir:    baseDir,
		generators: byID,
		order:      order,
		now:        time.Now,
		queue:      make(chan struct{}, 1),
		jobs:       make(map[string]Job),
	}
}

func (m *Manager) Status(ctx context.Context) Capability {
	providers := make([]ProviderStatus, len(m.order))
	var wait sync.WaitGroup
	for index, id := range m.order {
		wait.Add(1)
		go func() {
			defer wait.Done()
			providers[index] = m.generators[id].Status(ctx)
		}()
	}
	wait.Wait()
	ready := false
	for _, provider := range providers {
		ready = ready || provider.Ready
	}
	message := "No supported agent is ready"
	if ready {
		message = "Choose an available agent"
	}
	return Capability{Ready: ready, Message: message, Providers: providers}
}

func (m *Manager) Create(request Request) (Job, error) {
	request.Provider = strings.TrimSpace(request.Provider)
	generator := m.generators[request.Provider]
	if generator == nil {
		return Job{}, errors.New("choose an available agent")
	}
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Prompt == "" {
		return Job{}, errors.New("describe the PDF you want the agent to create")
	}
	if len(request.Prompt) > maxPromptLength {
		return Job{}, fmt.Errorf("custom prompt must be at most %d characters", maxPromptLength)
	}
	id, err := jobID(m.now())
	if err != nil {
		return Job{}, fmt.Errorf("create job ID: %w", err)
	}
	now := m.now()
	job := Job{
		ID: id, Provider: generator.ID(), ProviderName: generator.Name(), Status: "queued",
		CreatedAt: now, UpdatedAt: now, Files: []File{},
	}
	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()

	go m.run(id, request)
	return job, nil
}

func (m *Manager) Get(id string) (Job, bool) {
	m.mu.RLock()
	job, ok := m.jobs[id]
	m.mu.RUnlock()
	job.Files = append([]File(nil), job.Files...)
	return job, ok
}

func (m *Manager) Open(id, name string) (ReadSeekCloser, os.FileInfo, error) {
	job, ok := m.Get(id)
	if !ok || job.Status != "completed" {
		return nil, nil, os.ErrNotExist
	}
	allowed := false
	for _, file := range job.Files {
		if file.Name == name {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, nil, os.ErrNotExist
	}

	root, err := os.OpenRoot(filepath.Join(m.baseDir, id))
	if err != nil {
		return nil, nil, err
	}
	file, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		_ = root.Close()
		return nil, nil, err
	}
	return &rootedFile{File: file, root: root}, info, nil
}

func (m *Manager) run(id string, request Request) {
	m.queue <- struct{}{}
	defer func() { <-m.queue }()
	m.update(id, func(job *Job) {
		job.Status = "running"
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	outputDir := filepath.Join(m.baseDir, id)
	result, err := m.generators[request.Provider].Generate(ctx, request, outputDir)
	if err != nil {
		m.update(id, func(job *Job) {
			job.Status = "failed"
			job.Error = err.Error()
		})
		return
	}
	m.update(id, func(job *Job) {
		job.Status = "completed"
		job.Summary = result.Summary
		job.Files = append([]File(nil), result.Files...)
	})
}

func (m *Manager) update(id string, change func(*Job)) {
	m.mu.Lock()
	job := m.jobs[id]
	change(&job)
	job.UpdatedAt = m.now()
	m.jobs[id] = job
	m.mu.Unlock()
}

func jobID(now time.Time) (string, error) {
	var suffix [5]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return now.UTC().Format("20060102-150405-") + hex.EncodeToString(suffix[:]), nil
}

type rootedFile struct {
	*os.File
	root *os.Root
}

func (f *rootedFile) Close() error {
	fileErr := f.File.Close()
	rootErr := f.root.Close()
	if fileErr != nil {
		return fileErr
	}
	return rootErr
}

var _ io.ReadSeeker = (*rootedFile)(nil)
