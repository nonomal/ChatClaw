// Package sandbox provides pluggable command execution backends with
// optional OS-level isolation via OpenAI Codex CLI.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Mode selects the sandbox execution strategy.
type Mode string

const (
	ModeCodex  Mode = "codex"  // OS-level sandbox via codex CLI
	ModeNative Mode = "native" // Direct execution, no isolation
)

// ExecResult holds the output of a sandboxed command execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Config holds the sandbox configuration read from settings.
type Config struct {
	Mode    Mode   // "codex" or "native"
	WorkDir string // Working directory for command execution
}

// Provider executes commands according to the configured sandbox mode.
type Provider struct {
	config   Config
	codexBin string // resolved path to codex binary (empty = not found)
}

// NewProvider creates a Provider. codexBinDir is the toolchain bin directory
// where the codex binary lives (may be empty if not installed yet).
func NewProvider(cfg Config, codexBinDir string) *Provider {
	p := &Provider{config: cfg}
	if codexBinDir != "" {
		binName := "codex"
		if runtime.GOOS == "windows" {
			binName = "codex.exe"
		}
		candidate := filepath.Join(codexBinDir, binName)
		if _, err := os.Stat(candidate); err == nil {
			p.codexBin = candidate
		}
	}
	return p
}

// IsCodexAvailable returns true if the codex binary was found.
func (p *Provider) IsCodexAvailable() bool {
	return p.codexBin != ""
}

// EffectiveMode returns the mode that will actually be used.
// Falls back to native if codex is requested but not available.
func (p *Provider) EffectiveMode() Mode {
	if p.config.Mode == ModeCodex && p.codexBin != "" {
		return ModeCodex
	}
	return ModeNative
}

// WorkDir returns the configured working directory.
func (p *Provider) WorkDir() string {
	return p.config.WorkDir
}

// Exec runs a shell command using the configured sandbox mode.
func (p *Provider) Exec(ctx context.Context, command string, timeout time.Duration) (*ExecResult, error) {
	if p.EffectiveMode() == ModeCodex {
		return p.execCodex(ctx, command, timeout)
	}
	return p.execNative(ctx, command, timeout)
}

// execCodex runs the command inside the codex sandbox.
// Usage: codex sandbox <platform> --full-auto -- sh -c <command>
func (p *Provider) execCodex(ctx context.Context, command string, timeout time.Duration) (*ExecResult, error) {
	start := time.Now()

	platform := "macos"
	if runtime.GOOS == "linux" {
		platform = "linux"
	}

	args := []string{
		"sandbox", platform,
		"--full-auto",
		"--",
		"sh", "-c", command,
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, p.codexBin, args...)
	cmd.Dir = p.config.WorkDir
	cmd.Env = append(os.Environ(), "CODEX_SANDBOX_NETWORK_DISABLED=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: time.Since(start),
	}

	if execCtx.Err() == context.DeadlineExceeded {
		result.Stderr += "\n[Command timed out]"
	}

	if err != nil && result.Stdout == "" && result.Stderr == "" {
		return nil, fmt.Errorf("codex sandbox exec failed: %w", err)
	}

	return result, nil
}

// execNative runs the command directly on the host.
func (p *Provider) execNative(ctx context.Context, command string, timeout time.Duration) (*ExecResult, error) {
	start := time.Now()

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		wrappedCmd := "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " +
			"$OutputEncoding = [System.Text.Encoding]::UTF8; " +
			command
		cmd = exec.CommandContext(execCtx, "powershell.exe",
			"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
			"-Command", wrappedCmd,
		)
	case "darwin":
		cmd = exec.CommandContext(execCtx, "/bin/zsh", "-l", "-c", command)
	default:
		cmd = exec.CommandContext(execCtx, "/bin/bash", "-l", "-c", command)
	}
	cmd.Dir = p.config.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: time.Since(start),
	}

	if execCtx.Err() == context.DeadlineExceeded {
		result.Stderr += "\n[Command timed out]"
	}

	if err != nil && result.Stdout == "" && result.Stderr == "" {
		return nil, fmt.Errorf("native exec failed: %w", err)
	}

	return result, nil
}

// DefaultWorkDir returns the default workspace directory (~/.chatclaw).
func DefaultWorkDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".chatclaw")
}

// EnsureWorkDir creates the working directory if it doesn't exist.
func EnsureWorkDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = DefaultWorkDir()
	}
	return os.MkdirAll(dir, 0o755)
}
