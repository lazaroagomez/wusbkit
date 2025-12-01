package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lazaroagomez/wusbkit/internal/format"
	"github.com/lazaroagomez/wusbkit/internal/lock"
	"github.com/lazaroagomez/wusbkit/internal/output"
	"github.com/lazaroagomez/wusbkit/internal/powershell"
	"github.com/lazaroagomez/wusbkit/internal/usb"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var (
	formatYes   bool
	formatFS    string
	formatLabel string
	formatQuick bool
)

var formatCmd = &cobra.Command{
	Use:   "format <drive>",
	Short: "Format a USB drive",
	Long: `Format a USB storage device with the specified filesystem.

WARNING: This will ERASE ALL DATA on the drive!

The drive can be specified by:
  - Drive letter (e.g., E: or E)
  - Disk number (e.g., 2)

Supported filesystems: fat32, ntfs, exfat`,
	Example: `  wusbkit format E: --fs fat32 --label MYUSB
  wusbkit format 2 --fs ntfs --yes
  wusbkit format E: --fs exfat --label DATA --quick=false`,
	Args: cobra.ExactArgs(1),
	RunE: runFormat,
}

func init() {
	formatCmd.Flags().BoolVarP(&formatYes, "yes", "y", false, "Skip confirmation prompt")
	formatCmd.Flags().StringVar(&formatFS, "fs", "fat32", "Filesystem type: fat32, ntfs, exfat")
	formatCmd.Flags().StringVar(&formatLabel, "label", "USB", "Volume label")
	formatCmd.Flags().BoolVar(&formatQuick, "quick", true, "Quick format")
	rootCmd.AddCommand(formatCmd)
}

func runFormat(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	// Validate filesystem
	if err := format.ValidateFileSystem(formatFS); err != nil {
		if jsonOutput {
			output.PrintJSONError(err.Error(), output.ErrCodeInvalidInput)
		} else {
			PrintError(err.Error(), output.ErrCodeInvalidInput)
		}
		return err
	}

	// Check for admin privileges
	if !format.IsAdmin() {
		errMsg := "Administrator privileges required for formatting"
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodePermDenied)
		} else {
			PrintError(errMsg, output.ErrCodePermDenied)
		}
		return errors.New(errMsg)
	}

	// Check PowerShell availability
	if err := powershell.CheckPwshAvailable(); err != nil {
		if jsonOutput {
			output.PrintJSONError("PowerShell 7 (pwsh.exe) is required but not found", output.ErrCodePwshNotFound)
		} else {
			PrintError("PowerShell 7 (pwsh.exe) is required but not found", output.ErrCodePwshNotFound)
		}
		return err
	}

	// Check diskpart availability
	if err := format.CheckDiskpartAvailable(); err != nil {
		if jsonOutput {
			output.PrintJSONError(err.Error(), output.ErrCodeInternalError)
		} else {
			PrintError(err.Error(), output.ErrCodeInternalError)
		}
		return err
	}

	// Find the device
	enum := usb.NewEnumerator()
	device, err := enum.GetDevice(identifier)
	if err != nil {
		if jsonOutput {
			output.PrintJSONError(err.Error(), output.ErrCodeUSBNotFound)
		} else {
			PrintError(err.Error(), output.ErrCodeUSBNotFound)
		}
		return err
	}

	// Check if disk is being flashed
	diskLock, err := lock.NewDiskLock(device.DiskNumber)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create disk lock: %v", err)
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeInternalError)
		} else {
			PrintError(errMsg, output.ErrCodeInternalError)
		}
		return err
	}

	if err := diskLock.TryLock(cmd.Context(), 1*time.Second); err != nil {
		errMsg := fmt.Sprintf("disk %d is busy (another operation in progress)", device.DiskNumber)
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeDiskBusy)
		} else {
			PrintError(errMsg, output.ErrCodeDiskBusy)
		}
		return errors.New(errMsg)
	}
	defer diskLock.Unlock()

	// Confirmation prompt (unless --yes or --json)
	if !formatYes && !jsonOutput {
		pterm.Warning.Printf("This will ERASE ALL DATA on disk %d (%s - %s)\n",
			device.DiskNumber, device.FriendlyName, device.SizeHuman)

		confirmed, _ := pterm.DefaultInteractiveConfirm.
			WithDefaultValue(false).
			Show("Continue with format?")

		if !confirmed {
			pterm.Info.Println("Format cancelled")
			return nil
		}
	}

	// Perform format
	opts := format.Options{
		DiskNumber: device.DiskNumber,
		FileSystem: formatFS,
		Label:      formatLabel,
		Quick:      formatQuick,
	}

	formatter := format.NewFormatter()

	// Start format in background
	ctx := context.Background()
	errChan := make(chan error, 1)

	go func() {
		errChan <- formatter.Format(ctx, opts)
	}()

	// Show progress
	if jsonOutput {
		// Stream JSON progress
		for progress := range formatter.Progress() {
			data, _ := json.Marshal(progress)
			fmt.Println(string(data))
		}
	} else {
		// Show spinner with progress updates
		spinner, _ := pterm.DefaultSpinner.Start("Starting format...")

		for progress := range formatter.Progress() {
			switch progress.Status {
			case "in_progress":
				spinner.UpdateText(fmt.Sprintf("%s (%d%%)", progress.Stage, progress.Percentage))
			case "error":
				spinner.Fail(progress.Error)
			case "complete":
				if progress.Drive != "" {
					spinner.Success(fmt.Sprintf("Format complete! Drive assigned: %s", progress.Drive))
				} else {
					spinner.Success("Format complete!")
				}
			}
		}
	}

	// Wait for format to complete
	if err := <-errChan; err != nil {
		if !jsonOutput {
			PrintError(err.Error(), output.ErrCodeFormatFailed)
		}
		return err
	}

	return nil
}
