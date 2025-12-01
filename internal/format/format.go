package format

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/lazaroagomez/wusbkit/internal/powershell"
)

// Options configures the format operation
type Options struct {
	DiskNumber int
	FileSystem string // fat32, ntfs, exfat
	Label      string
	Quick      bool
}

// ValidateFileSystem checks if the filesystem is supported
func ValidateFileSystem(fs string) error {
	fs = strings.ToLower(fs)
	switch fs {
	case "fat32", "ntfs", "exfat":
		return nil
	default:
		return fmt.Errorf("unsupported filesystem: %s (supported: fat32, ntfs, exfat)", fs)
	}
}

// Progress represents the current state of a format operation
type Progress struct {
	Drive      string `json:"drive"`
	DiskNumber int    `json:"diskNumber"`
	Stage      string `json:"stage"`
	Percentage int    `json:"percentage"`
	Status     string `json:"status"` // in_progress, complete, error
	Error      string `json:"error,omitempty"`
}

// Stage constants for format progress
const (
	StageCleaning          = "Cleaning disk"
	StageCreatingPartition = "Creating partition"
	StageFormatting        = "Formatting"
	StageAssigningLetter   = "Assigning drive letter"
	StageComplete          = "Complete"
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

// FormatResult represents the result of a PowerShell format operation
type FormatResult struct {
	Success     bool   `json:"Success"`
	DriveLetter string `json:"DriveLetter"`
	Message     string `json:"Message"`
}

// Format formats a USB drive using PowerShell 7
func (f *Formatter) Format(ctx context.Context, opts Options) error {
	defer close(f.progressChan)

	// Send initial progress
	f.sendProgress(opts, StageCleaning, 10)

	// Generate PowerShell script
	script := generateFormatScript(opts)

	// Use longer timeout for full format
	timeout := 60 * time.Second
	if !opts.Quick {
		timeout = 300 * time.Second
	}

	executor := powershell.NewExecutor(timeout)

	f.sendProgress(opts, StageFormatting, 50)

	// Execute PowerShell script
	output, err := executor.ExecuteContext(ctx, script)
	if err != nil {
		f.sendError(opts, "PowerShell execution failed: "+err.Error())
		return fmt.Errorf("powershell execution failed: %w", err)
	}

	// Parse JSON result
	var result FormatResult
	if err := json.Unmarshal(output, &result); err != nil {
		f.sendError(opts, "Failed to parse result: "+err.Error())
		return fmt.Errorf("failed to parse powershell result: %w", err)
	}

	if !result.Success {
		f.sendError(opts, result.Message)
		return fmt.Errorf("format failed: %s", result.Message)
	}

	f.sendComplete(opts, result.DriveLetter)
	return nil
}

// generateFormatScript creates a PowerShell script for formatting
func generateFormatScript(opts Options) string {
	fs := strings.ToUpper(opts.FileSystem)
	label := opts.Label
	if label == "" {
		label = "USB"
	}

	formatFull := "$true"
	if opts.Quick {
		formatFull = "$false"
	}

	return fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$disk = %d

try {
    # Step 1: Ensure disk is writable (required for Initialize-Disk and Set-Disk)
    Set-Disk -Number $disk -IsReadOnly $false -ErrorAction SilentlyContinue

    # Step 2: Remove all existing partitions
    Get-Partition -DiskNumber $disk -ErrorAction SilentlyContinue |
        Remove-Partition -Confirm:$false -ErrorAction SilentlyContinue

    # Step 3: Set partition style to MBR
    # For removable media, use Set-Disk (Clear-Disk doesn't uninitialize removable media)
    Set-Disk -Number $disk -PartitionStyle MBR -ErrorAction Stop

    # Step 4: Refresh disk info
    Update-Disk -Number $disk -ErrorAction SilentlyContinue

    # Step 5: Create bootable partition (-IsActive only valid on MBR disks)
    $partition = New-Partition -DiskNumber $disk -UseMaximumSize -AssignDriveLetter -IsActive -ErrorAction Stop

    # Step 6: Format volume
    Format-Volume -DriveLetter $partition.DriveLetter -FileSystem %s -NewFileSystemLabel '%s' -Full:%s -ErrorAction Stop | Out-Null

    @{
        Success = $true
        DriveLetter = "$($partition.DriveLetter):"
        Message = "Format complete"
    } | ConvertTo-Json -Compress
}
catch {
    @{
        Success = $false
        DriveLetter = ""
        Message = $_.Exception.Message
    } | ConvertTo-Json -Compress
}
`, opts.DiskNumber, fs, label, formatFull)
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

// IsAdmin checks if the current process has administrator privileges
func IsAdmin() bool {
	// Try to open a privileged registry key
	cmd := exec.Command("net", "session")
	err := cmd.Run()
	return err == nil
}
