package format

import (
	"fmt"
	"strings"
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

// GenerateDiskpartScript creates a diskpart script for formatting
func GenerateDiskpartScript(opts Options) string {
	fs := strings.ToUpper(opts.FileSystem)
	label := opts.Label
	if label == "" {
		label = "USB"
	}

	// Build the format command
	formatCmd := fmt.Sprintf("format fs=%s label=\"%s\"", fs, label)
	if opts.Quick {
		formatCmd += " quick"
	}

	script := fmt.Sprintf(`select disk %d
clean
create partition primary
select partition 1
active
%s
assign
`, opts.DiskNumber, formatCmd)

	return script
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
