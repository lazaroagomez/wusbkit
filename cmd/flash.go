package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/lazaroagomez/wusbkit/internal/flash"
	"github.com/lazaroagomez/wusbkit/internal/format"
	"github.com/lazaroagomez/wusbkit/internal/output"
	"github.com/lazaroagomez/wusbkit/internal/powershell"
	"github.com/lazaroagomez/wusbkit/internal/usb"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var (
	flashImage  string
	flashVerify bool
	flashYes    bool
	flashBuffer string
)

var flashCmd = &cobra.Command{
	Use:   "flash <drive>",
	Short: "Write an image to a USB drive",
	Long: `Write a disk image directly to a USB drive (raw write).

WARNING: This will COMPLETELY OVERWRITE the target drive!

The drive can be specified by:
  - Drive letter (e.g., E: or E)
  - Disk number (e.g., 2)

Supported image sources:
  - Local files: .img, .iso, .bin, .raw
  - Local compressed: .zip (streams first image file inside)
  - Remote URLs: HTTP/HTTPS URLs (streams directly without downloading)`,
	Example: `  wusbkit flash 2 --image ubuntu.img
  wusbkit flash E: --image recovery.zip --verify
  wusbkit flash 2 --image debian.iso --yes --json
  wusbkit flash E: --image https://example.com/image.img`,
	Args: cobra.ExactArgs(1),
	RunE: runFlash,
}

func init() {
	flashCmd.Flags().StringVarP(&flashImage, "image", "i", "", "Path to image file or URL (required)")
	flashCmd.Flags().BoolVar(&flashVerify, "verify", false, "Verify write by reading back and comparing")
	flashCmd.Flags().BoolVarP(&flashYes, "yes", "y", false, "Skip confirmation prompt")
	flashCmd.Flags().StringVarP(&flashBuffer, "buffer", "b", "4M", "Buffer size (e.g., 4M, 8MB)")
	flashCmd.MarkFlagRequired("image")
	rootCmd.AddCommand(flashCmd)
}

// parseBufferSize converts buffer size strings like "4M", "8MB", "16m" to megabytes.
// Returns the size in MB or an error if the format is invalid.
func parseBufferSize(s string) (int, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.TrimSuffix(s, "B") // Remove trailing B if present (8MB -> 8M)

	if strings.HasSuffix(s, "M") {
		val, err := strconv.Atoi(strings.TrimSuffix(s, "M"))
		if err != nil {
			return 0, fmt.Errorf("invalid buffer size: %s", s)
		}
		return val, nil
	}

	// Try plain number (assume MB)
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid buffer size: %s (use format like 4M or 8MB)", s)
	}
	return val, nil
}

func runFlash(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	// Check if image is a URL (skip file existence check for URLs)
	isURL := flash.IsURL(flashImage)

	// Validate local image file exists (skip for URLs)
	if !isURL {
		if _, err := os.Stat(flashImage); os.IsNotExist(err) {
			errMsg := fmt.Sprintf("Image file not found: %s", flashImage)
			if jsonOutput {
				output.PrintJSONError(errMsg, output.ErrCodeInvalidInput)
			} else {
				PrintError(errMsg, output.ErrCodeInvalidInput)
			}
			return errors.New(errMsg)
		}
	}

	// Check for admin privileges
	if !format.IsAdmin() {
		errMsg := "Administrator privileges required for flashing"
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodePermDenied)
		} else {
			PrintError(errMsg, output.ErrCodePermDenied)
		}
		return errors.New(errMsg)
	}

	// Check PowerShell availability (needed for device enumeration)
	if err := powershell.CheckPwshAvailable(); err != nil {
		if jsonOutput {
			output.PrintJSONError("PowerShell 7 (pwsh.exe) is required but not found", output.ErrCodePwshNotFound)
		} else {
			PrintError("PowerShell 7 (pwsh.exe) is required but not found", output.ErrCodePwshNotFound)
		}
		return err
	}

	// Find the device
	enum := usb.NewEnumerator()
	var device *usb.Device
	var err error

	// Try to parse as disk number first
	if diskNum, parseErr := strconv.Atoi(identifier); parseErr == nil {
		device, err = enum.GetDeviceByDiskNumber(diskNum)
	} else {
		device, err = enum.GetDeviceByDriveLetter(identifier)
	}

	if err != nil {
		if jsonOutput {
			output.PrintJSONError(err.Error(), output.ErrCodeUSBNotFound)
		} else {
			PrintError(err.Error(), output.ErrCodeUSBNotFound)
		}
		return err
	}

	// Get image info for display
	source, err := flash.OpenSource(flashImage)
	if err != nil {
		if jsonOutput {
			output.PrintJSONError(err.Error(), output.ErrCodeInvalidInput)
		} else {
			PrintError(err.Error(), output.ErrCodeInvalidInput)
		}
		return err
	}
	imageSize := source.Size()
	imageName := source.Name()
	source.Close()

	// Validate image fits on device
	if imageSize > device.Size {
		errMsg := fmt.Sprintf("Image (%s) is larger than device (%s)",
			flash.FormatBytes(imageSize), device.SizeHuman)
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeInvalidInput)
		} else {
			PrintError(errMsg, output.ErrCodeInvalidInput)
		}
		return errors.New(errMsg)
	}

	// Confirmation prompt (unless --yes or --json)
	if !flashYes && !jsonOutput {
		pterm.Warning.Printf("This will COMPLETELY OVERWRITE disk %d (%s - %s)\n",
			device.DiskNumber, device.FriendlyName, device.SizeHuman)
		pterm.Info.Printf("Image: %s (%s)\n", imageName, flash.FormatBytes(imageSize))

		if flashVerify {
			pterm.Info.Println("Verification: enabled")
		}

		confirmed, _ := pterm.DefaultInteractiveConfirm.
			WithDefaultValue(false).
			Show("Continue with flash?")

		if !confirmed {
			pterm.Info.Println("Flash cancelled")
			return nil
		}
	}

	// Setup context with cancellation for Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if !jsonOutput {
			pterm.Warning.Println("\nCancelling... (waiting for current operation)")
		}
		cancel()
	}()

	// Parse and validate buffer size
	bufferMB, err := parseBufferSize(flashBuffer)
	if err != nil {
		if jsonOutput {
			output.PrintJSONError(err.Error(), output.ErrCodeInvalidInput)
		} else {
			PrintError(err.Error(), output.ErrCodeInvalidInput)
		}
		return err
	}
	if bufferMB < 1 || bufferMB > 64 {
		errMsg := fmt.Sprintf("buffer size must be between 1M and 64M (got %dM)", bufferMB)
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeInvalidInput)
		} else {
			PrintError(errMsg, output.ErrCodeInvalidInput)
		}
		return errors.New(errMsg)
	}

	// Prepare flash options
	opts := flash.Options{
		DiskNumber: device.DiskNumber,
		ImagePath:  flashImage,
		Verify:     flashVerify,
		BufferSize: bufferMB,
	}

	flasher := flash.NewFlasher()

	// Start flash in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- flasher.Flash(ctx, opts)
	}()

	// Show progress
	if jsonOutput {
		// Stream JSON progress
		for progress := range flasher.Progress() {
			data, _ := json.Marshal(progress)
			fmt.Println(string(data))
		}
	} else {
		// Show spinner with progress updates
		spinner, _ := pterm.DefaultSpinner.Start("Preparing to write...")

		for progress := range flasher.Progress() {
			switch progress.Status {
			case flash.StatusInProgress:
				text := fmt.Sprintf("%s %d%% | %s / %s",
					progress.Stage,
					progress.Percentage,
					flash.FormatBytes(progress.BytesWritten),
					flash.FormatBytes(progress.TotalBytes))
				if progress.Speed != "" {
					text += fmt.Sprintf(" | %s", progress.Speed)
				}
				spinner.UpdateText(text)

			case flash.StatusError:
				spinner.Fail(progress.Error)

			case flash.StatusComplete:
				if flashVerify {
					spinner.Success("Flash complete! (verified)")
				} else {
					spinner.Success("Flash complete!")
				}
			}
		}
	}

	// Wait for flash to complete
	if err := <-errChan; err != nil {
		if !jsonOutput && err != context.Canceled {
			PrintError(err.Error(), output.ErrCodeFlashFailed)
		}
		return err
	}

	return nil
}
