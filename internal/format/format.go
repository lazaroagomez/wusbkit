package format

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Formatter handles USB drive formatting operations
type Formatter struct {
	progressChan chan Progress
}

// NewFormatter creates a new formatter
func NewFormatter() *Formatter {
	return &Formatter{
		progressChan: make(chan Progress, 10),
	}
}

// Progress returns a channel that receives progress updates
func (f *Formatter) Progress() <-chan Progress {
	return f.progressChan
}

// Format formats a USB drive using diskpart
func (f *Formatter) Format(ctx context.Context, opts Options) error {
	defer close(f.progressChan)

	// Generate diskpart script
	script := GenerateDiskpartScript(opts)

	// Create temp file for script
	tmpFile, err := os.CreateTemp("", "wusbkit-diskpart-*.txt")
	if err != nil {
		f.sendError(opts, "Failed to create temp file: "+err.Error())
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(script); err != nil {
		tmpFile.Close()
		f.sendError(opts, "Failed to write script: "+err.Error())
		return fmt.Errorf("failed to write script: %w", err)
	}
	tmpFile.Close()

	// Send initial progress
	f.sendProgress(opts, StageCleaning, 10)

	// Run diskpart
	cmd := exec.CommandContext(ctx, "diskpart", "/s", tmpFile.Name())

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		f.sendError(opts, "Failed to create pipe: "+err.Error())
		return fmt.Errorf("failed to create pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		f.sendError(opts, "Failed to start diskpart: "+err.Error())
		return fmt.Errorf("failed to start diskpart: %w", err)
	}

	// Parse output and update progress
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		f.parseOutputLine(opts, line)
	}

	if err := cmd.Wait(); err != nil {
		f.sendError(opts, "Diskpart failed: "+err.Error())
		return fmt.Errorf("diskpart failed: %w", err)
	}

	// Wait a moment for the drive letter to be assigned
	time.Sleep(500 * time.Millisecond)

	// Get the assigned drive letter
	driveLetter, err := f.getAssignedDriveLetter(opts.DiskNumber)
	if err != nil {
		// Non-fatal, format succeeded but we don't know the letter
		f.sendComplete(opts, "")
	} else {
		f.sendComplete(opts, driveLetter)
	}

	return nil
}

func (f *Formatter) parseOutputLine(opts Options, line string) {
	lineLower := strings.ToLower(line)

	switch {
	case strings.Contains(lineLower, "diskpart succeeded in cleaning"):
		f.sendProgress(opts, StageCreatingPartition, 30)
	case strings.Contains(lineLower, "diskpart succeeded in creating"):
		f.sendProgress(opts, StageFormatting, 50)
	case strings.Contains(lineLower, "percent complete"):
		// Try to parse format percentage
		f.sendProgress(opts, StageFormatting, 60)
	case strings.Contains(lineLower, "format complete"):
		f.sendProgress(opts, StageAssigningLetter, 90)
	case strings.Contains(lineLower, "diskpart assigned"):
		f.sendProgress(opts, StageComplete, 100)
	}
}

func (f *Formatter) sendProgress(opts Options, stage string, percentage int) {
	select {
	case f.progressChan <- Progress{
		DiskNumber: opts.DiskNumber,
		Stage:      stage,
		Percentage: percentage,
		Status:     "in_progress",
	}:
	default:
	}
}

func (f *Formatter) sendError(opts Options, errMsg string) {
	select {
	case f.progressChan <- Progress{
		DiskNumber: opts.DiskNumber,
		Stage:      "Error",
		Percentage: 0,
		Status:     "error",
		Error:      errMsg,
	}:
	default:
	}
}

func (f *Formatter) sendComplete(opts Options, driveLetter string) {
	select {
	case f.progressChan <- Progress{
		Drive:      driveLetter,
		DiskNumber: opts.DiskNumber,
		Stage:      StageComplete,
		Percentage: 100,
		Status:     "complete",
	}:
	default:
	}
}

func (f *Formatter) getAssignedDriveLetter(diskNumber int) (string, error) {
	// Use PowerShell to get the drive letter
	cmd := exec.Command("pwsh.exe", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf(`(Get-Partition -DiskNumber %d | Where-Object {$_.DriveLetter} | Select-Object -First 1).DriveLetter`, diskNumber))

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	letter := strings.TrimSpace(string(output))
	if letter == "" {
		return "", fmt.Errorf("no drive letter assigned")
	}

	return letter + ":", nil
}

// CheckDiskpartAvailable verifies that diskpart is available
func CheckDiskpartAvailable() error {
	diskpartPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "diskpart.exe")
	if _, err := os.Stat(diskpartPath); err != nil {
		return fmt.Errorf("diskpart not found at %s", diskpartPath)
	}
	return nil
}

// IsAdmin checks if the current process has administrator privileges
func IsAdmin() bool {
	// Try to open a privileged registry key
	cmd := exec.Command("net", "session")
	err := cmd.Run()
	return err == nil
}
