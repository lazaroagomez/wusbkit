package powershell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	DefaultTimeout = 30 * time.Second
)

var (
	ErrPwshNotFound = errors.New("pwsh.exe not found - PowerShell 7 is required")
	ErrTimeout      = errors.New("PowerShell command timed out")
	ErrExecution    = errors.New("PowerShell execution failed")
)

// Executor runs PowerShell 7 commands and parses their output
type Executor struct {
	timeout time.Duration
}

// NewExecutor creates a new PowerShell executor with the specified timeout
func NewExecutor(timeout time.Duration) *Executor {
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &Executor{timeout: timeout}
}

// Execute runs a PowerShell command and returns the raw output
func (e *Executor) Execute(command string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	return e.ExecuteContext(ctx, command)
}

// ExecuteContext runs a PowerShell command with the given context
func (e *Executor) ExecuteContext(ctx context.Context, command string) ([]byte, error) {
	// Check if pwsh.exe is available
	pwshPath, err := exec.LookPath("pwsh.exe")
	if err != nil {
		// Try pwsh without .exe extension
		pwshPath, err = exec.LookPath("pwsh")
		if err != nil {
			return nil, ErrPwshNotFound
		}
	}

	cmd := exec.CommandContext(ctx, pwshPath, "-NoProfile", "-NonInteractive", "-Command", command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return nil, ErrTimeout
	}

	if err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return nil, fmt.Errorf("%w: %s", ErrExecution, stderrStr)
		}
		return nil, fmt.Errorf("%w: %v", ErrExecution, err)
	}

	return stdout.Bytes(), nil
}

// ExecuteJSON runs a PowerShell command and parses the JSON output into the target
func (e *Executor) ExecuteJSON(command string, target interface{}) error {
	// Wrap command to output JSON
	jsonCommand := fmt.Sprintf("%s | ConvertTo-Json -Depth 10 -Compress", command)

	output, err := e.Execute(jsonCommand)
	if err != nil {
		return err
	}

	// Handle empty output
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return nil
	}

	// Parse JSON
	if err := json.Unmarshal(trimmed, target); err != nil {
		return fmt.Errorf("failed to parse PowerShell JSON output: %w", err)
	}

	return nil
}

// ExecuteJSONArray runs a PowerShell command that returns an array
// PowerShell ConvertTo-Json returns a single object (not array) when there's only one result
// This method normalizes the output to always be an array using -AsArray flag (PowerShell 7+)
func (e *Executor) ExecuteJSONArray(command string, target interface{}) error {
	// Use -AsArray to ensure array output even with single item (PowerShell 7+ feature)
	arrayCommand := fmt.Sprintf("@(%s) | ConvertTo-Json -Depth 10 -Compress -AsArray", command)

	output, err := e.Execute(arrayCommand)
	if err != nil {
		return err
	}

	// Handle empty output
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "[]" {
		return nil
	}

	// Parse JSON
	if err := json.Unmarshal(trimmed, target); err != nil {
		return fmt.Errorf("failed to parse PowerShell JSON output: %w", err)
	}

	return nil
}

// CheckPwshAvailable verifies that PowerShell 7 is installed and accessible
func CheckPwshAvailable() error {
	_, err := exec.LookPath("pwsh.exe")
	if err != nil {
		_, err = exec.LookPath("pwsh")
		if err != nil {
			return ErrPwshNotFound
		}
	}
	return nil
}

// GetPwshVersion returns the PowerShell version string
func GetPwshVersion() (string, error) {
	e := NewExecutor(5 * time.Second)
	output, err := e.Execute("$PSVersionTable.PSVersion.ToString()")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
