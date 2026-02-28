package filesystem

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk/filesystem"
)

// ShellPolicy defines security constraints for shell command execution.
type ShellPolicy struct {
	TrustedDirs     []string      // Allowed working directories. Empty = no restriction.
	BlockedCommands []string      // Rejected command patterns (substring match).
	DefaultTimeout  time.Duration // Max execution time per command. 0 = 60s default.
}

// Execute runs a shell command and returns its output.
// Shell: powershell on Windows, zsh on macOS, bash on Linux.
// Working directory: baseDir (or sandboxDir when sandbox is enabled).
//
// When SandboxMode is "codex" and a codex binary is available, the command
// is wrapped with `codex sandbox <platform> --full-auto -- sh -c <command>`
// for OS-level isolation (Seatbelt on macOS, Landlock on Linux).
//
// The command runs in its own process group so that on timeout or cancellation
// the entire process tree (including child processes such as `php artisan serve`)
// is killed immediately, preventing the tool from hanging.
func (b *LocalBackend) Execute(ctx context.Context, req *filesystem.ExecuteRequest) (*filesystem.ExecuteResponse, error) {
	if err := b.validateCommand(req.Command); err != nil {
		exitCode := -1
		return &filesystem.ExecuteResponse{
			Output:   "Command blocked: " + err.Error(),
			ExitCode: &exitCode,
		}, nil
	}

	timeout := 60 * time.Second
	if b.policy != nil && b.policy.DefaultTimeout > 0 {
		timeout = b.policy.DefaultTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workDir := b.baseDir
	if b.sandboxDir != "" {
		workDir = b.sandboxDir
	}

	var cmd *exec.Cmd
	if b.sandboxMode == SandboxModeCodex && b.codexBin != "" {
		cmd = b.buildCodexCommand(req.Command, workDir)
	} else {
		cmd = b.buildNativeCommand(req.Command)
		cmd.Dir = workDir
	}

	// Create a new process group so we can kill the entire tree.
	// (platform-specific; see exec_unix.go / exec_windows.go)
	setProcGroup(cmd)

	// Capture stdout+stderr via a buffer (instead of CombinedOutput) so we
	// can Start + Wait manually and kill the process group on cancellation.
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		exitCode := -1
		errMsg := fmt.Sprintf("failed to start command: %v", err)
		return &filesystem.ExecuteResponse{
			Output:   errMsg,
			ExitCode: &exitCode,
		}, nil
	}

	// Wait for the command in a separate goroutine.
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	// Block until the command finishes, the timeout fires, or the caller cancels.
	var cmdErr error
	timedOut := false
	cancelled := false
	select {
	case cmdErr = <-waitDone:
		// Command finished normally (success or failure).
	case <-execCtx.Done():
		// Timeout or upstream cancellation — kill the entire process group.
		timedOut = execCtx.Err() == context.DeadlineExceeded
		cancelled = !timedOut
		killProcessGroup(cmd)
		// Wait for cmd.Wait to return so we can safely read the buffer.
		cmdErr = <-waitDone
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	const maxOutput = 128 * 1024
	outputStr := buf.String()
	truncated := false
	if len(outputStr) > maxOutput {
		outputStr = outputStr[:maxOutput]
		truncated = true
	}

	if timedOut {
		outputStr += fmt.Sprintf("\n[Command timed out after %s]", timeout)
	} else if cancelled {
		outputStr += "\n[Command cancelled]"
	}

	if cmdErr != nil && len(outputStr) == 0 {
		outputStr = cmdErr.Error()
	}

	// Always include exit code — some LLM APIs reject empty tool result content.
	if outputStr == "" {
		outputStr = fmt.Sprintf("[exit code: %d]", exitCode)
	} else {
		outputStr = fmt.Sprintf("%s\n[exit code: %d]", outputStr, exitCode)
	}

	return &filesystem.ExecuteResponse{
		Output:    outputStr,
		ExitCode:  &exitCode,
		Truncated: truncated,
	}, nil
}

// buildCodexCommand wraps the user command in a codex sandbox invocation.
func (b *LocalBackend) buildCodexCommand(command, workDir string) *exec.Cmd {
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

	cmd := exec.Command(b.codexBin, args...)
	cmd.Dir = workDir
	return cmd
}

// buildNativeCommand creates a direct shell command (no sandbox).
func (b *LocalBackend) buildNativeCommand(command string) *exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		wrappedCmd := "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " +
			"$OutputEncoding = [System.Text.Encoding]::UTF8; " +
			command
		return exec.Command("powershell.exe",
			"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
			"-Command", wrappedCmd,
		)
	case "darwin":
		return exec.Command("/bin/zsh", "-l", "-c", command)
	default:
		return exec.Command("/bin/bash", "-l", "-c", command)
	}
}

func (b *LocalBackend) validateCommand(command string) error {
	if b.policy == nil {
		return nil
	}
	for _, blocked := range b.policy.BlockedCommands {
		if strings.Contains(command, blocked) {
			return fmt.Errorf("command contains blocked pattern: %q", blocked)
		}
	}
	return nil
}
