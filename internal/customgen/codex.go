package customgen

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"jira-project/internal/config"
)

const codexModel = "gpt-5.4"

type Codex struct {
	mcpCommand string
	cfg        config.Config
}

func NewCodex(mcpCommand string, cfg config.Config) *Codex {
	return &Codex{mcpCommand: mcpCommand, cfg: cfg}
}

func (*Codex) ID() string   { return "codex" }
func (*Codex) Name() string { return "Codex" }

func (c *Codex) Status(ctx context.Context) ProviderStatus {
	status := ProviderStatus{ID: c.ID(), Name: c.Name()}
	path, err := exec.LookPath("codex")
	if err != nil {
		status.Message = "Not installed"
		return status
	}
	statusCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(statusCtx, path, "login", "status")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(strings.ToLower(string(output)), "logged in") {
		status.Message = "Installed · run codex login"
		return status
	}
	status.Ready = true
	status.Message = "Connected · Jira research and PDF tools ready"
	return status
}

func (c *Codex) Generate(ctx context.Context, request Request, outputDir string) (Result, error) {
	capability := c.Status(ctx)
	if !capability.Ready {
		return Result{}, fmt.Errorf("Codex is unavailable: %s", capability.Message)
	}
	if strings.TrimSpace(c.mcpCommand) == "" {
		return Result{}, fmt.Errorf("Codex is unavailable: Jira tool command could not be resolved")
	}
	path, _ := exec.LookPath("codex")
	workspace, err := os.MkdirTemp("", "jira-custom-pdf-")
	if err != nil {
		return Result{}, fmt.Errorf("create isolated workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	mcpConfigPath, removeMCPConfig, err := createMCPConfig(c.cfg)
	if err != nil {
		return Result{}, fmt.Errorf("prepare Jira tools: %w", err)
	}
	defer removeMCPConfig()

	summaryPath := filepath.Join(workspace, ".codex-final-message.txt")
	args := []string{
		"exec", "--ignore-user-config", "--ephemeral", "--json",
		"--sandbox", "read-only", "--skip-git-repo-check", "-C", workspace,
		"--disable", "shell_tool", "--disable", "unified_exec",
		"--model", codexModel, "-c", `approval_policy="never"`,
		"-c", "mcp_servers.jira_pdf.command=" + strconv.Quote(c.mcpCommand),
		"-c", "mcp_servers.jira_pdf.args=" + tomlStrings([]string{"mcp", "--output-dir", workspace, "--config-file", mcpConfigPath}),
		"-c", "mcp_servers.jira_pdf.required=true",
		"-c", "mcp_servers.jira_pdf.enabled=true",
		"-c", "mcp_servers.jira_pdf.enabled_tools=" + tomlStrings(mcpTools),
		"-c", `mcp_servers.jira_pdf.default_tools_approval_mode="auto"`,
		"--output-last-message", summaryPath, "-",
	}
	command := exec.CommandContext(ctx, path, args...)
	command.Env = agentEnvironment()
	command.Stdin = strings.NewReader(buildPrompt(request))
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

	files, err := copyGeneratedPDFs(workspace, outputDir, summaryPath)
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("Codex finished without creating a valid PDF")
	}
	summary, _ := os.ReadFile(summaryPath)
	return Result{Summary: cleanSummary(string(summary)), Files: files}, nil
}
