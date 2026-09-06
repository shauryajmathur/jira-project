package customgen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"jira-project/internal/config"
)

type Claude struct {
	mcpCommand string
	cfg        config.Config
}

func NewClaude(mcpCommand string, cfg config.Config) *Claude {
	return &Claude{mcpCommand: mcpCommand, cfg: cfg}
}

func (*Claude) ID() string   { return "claude" }
func (*Claude) Name() string { return "Claude Code" }

func (c *Claude) Status(ctx context.Context) ProviderStatus {
	status := ProviderStatus{ID: c.ID(), Name: c.Name()}
	path, err := exec.LookPath("claude")
	if err != nil {
		status.Message = "Not installed"
		return status
	}
	statusCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	versionOutput, err := exec.CommandContext(statusCtx, path, "--version").CombinedOutput()
	cancel()
	if err != nil || strings.TrimSpace(string(versionOutput)) == "" {
		status.Message = "Installed but unavailable"
		return status
	}
	statusCtx, cancel = context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(statusCtx, path, "auth", "status").CombinedOutput(); err != nil || !claudeLoggedIn(output) {
		status.Message = "Installed · run claude auth login"
		return status
	}
	status.Ready = true
	status.Message = "Connected · Jira research and PDF tools ready"
	return status
}

func (c *Claude) Generate(ctx context.Context, request Request, outputDir string) (Result, error) {
	capability := c.Status(ctx)
	if !capability.Ready {
		return Result{}, fmt.Errorf("Claude Code is unavailable: %s", capability.Message)
	}
	if strings.TrimSpace(c.mcpCommand) == "" {
		return Result{}, fmt.Errorf("Claude Code is unavailable: Jira tool command could not be resolved")
	}
	path, _ := exec.LookPath("claude")
	workspace, err := os.MkdirTemp("", "jira-custom-pdf-")
	if err != nil {
		return Result{}, fmt.Errorf("create isolated workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	mcpRuntimePath, removeMCPConfig, err := createMCPConfig(c.cfg)
	if err != nil {
		return Result{}, fmt.Errorf("prepare Jira tools: %w", err)
	}
	defer removeMCPConfig()

	mcpConfigPath := filepath.Join(workspace, ".mcp.json")
	mcpConfig := map[string]any{"mcpServers": map[string]any{
		"jira_pdf": map[string]any{
			"type": "stdio", "command": c.mcpCommand,
			"args": []string{"mcp", "--output-dir", workspace, "--config-file", mcpRuntimePath},
		},
	}}
	encodedConfig, err := json.Marshal(mcpConfig)
	if err != nil {
		return Result{}, fmt.Errorf("prepare Claude Code Jira tools: %w", err)
	}
	if err := os.WriteFile(mcpConfigPath, encodedConfig, 0o600); err != nil {
		return Result{}, fmt.Errorf("prepare Claude Code Jira tools: %w", err)
	}

	args := []string{
		"--bare", "-p", "--no-session-persistence",
		"--strict-mcp-config", "--mcp-config", mcpConfigPath,
		"--tools", "", "--allowedTools", "mcp__jira_pdf__*",
		"--permission-mode", "dontAsk", "--output-format", "json",
		buildPrompt(request),
	}
	command := exec.CommandContext(ctx, path, args...)
	command.Dir = workspace
	command.Env = agentEnvironment()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, errorsForUser(ctx.Err().Error())
		}
		return Result{}, errorsForUser(commandError(c.Name(), stdout.String(), stderr.String()))
	}

	files, err := copyGeneratedPDFs(workspace, outputDir, mcpConfigPath)
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("Claude Code finished without creating a valid PDF")
	}
	return Result{Summary: cleanSummary(claudeSummary(stdout.Bytes())), Files: files}, nil
}

func claudeLoggedIn(output []byte) bool {
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if json.Unmarshal(output, &status) == nil {
		return status.LoggedIn
	}
	text := strings.ToLower(string(output))
	return strings.Contains(text, "logged in") && !strings.Contains(text, "not logged in")
}

func claudeSummary(output []byte) string {
	var response struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(output, &response) == nil && strings.TrimSpace(response.Result) != "" {
		return response.Result
	}
	return string(output)
}
