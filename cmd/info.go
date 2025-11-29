package cmd

import (
	"errors"
	"strconv"
	"strings"

	"github.com/lazaroagomez/wusbkit/internal/output"
	"github.com/lazaroagomez/wusbkit/internal/powershell"
	"github.com/lazaroagomez/wusbkit/internal/usb"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <drive>",
	Short: "Show detailed information for a USB drive",
	Long: `Display detailed information about a specific USB storage device.

The drive can be specified by:
  - Drive letter (e.g., E: or E)
  - Disk number (e.g., 2)`,
	Example: `  wusbkit info E:
  wusbkit info E
  wusbkit info 2
  wusbkit info E: --json`,
	Args: cobra.ExactArgs(1),
	RunE: runInfo,
}

func init() {
	rootCmd.AddCommand(infoCmd)
}

func runInfo(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	// Check PowerShell availability
	if err := powershell.CheckPwshAvailable(); err != nil {
		if jsonOutput {
			output.PrintJSONError("PowerShell 7 (pwsh.exe) is required but not found", output.ErrCodePwshNotFound)
		} else {
			PrintError("PowerShell 7 (pwsh.exe) is required but not found", output.ErrCodePwshNotFound)
		}
		return err
	}

	enum := usb.NewEnumerator()

	var device *usb.Device
	var err error

	// Try to parse as disk number first
	if diskNum, parseErr := strconv.Atoi(identifier); parseErr == nil {
		device, err = enum.GetDeviceByDiskNumber(diskNum)
	} else {
		// Try as drive letter
		device, err = enum.GetDeviceByDriveLetter(identifier)
	}

	if err != nil {
		errMsg := err.Error()
		if jsonOutput {
			if strings.Contains(errMsg, "not found") {
				output.PrintJSONError(errMsg, output.ErrCodeUSBNotFound)
			} else if strings.Contains(errMsg, "invalid") {
				output.PrintJSONError(errMsg, output.ErrCodeInvalidInput)
			} else if errors.Is(err, powershell.ErrPwshNotFound) {
				output.PrintJSONError(errMsg, output.ErrCodePwshNotFound)
			} else {
				output.PrintJSONError(errMsg, output.ErrCodeInternalError)
			}
		} else {
			PrintError(errMsg, output.ErrCodeUSBNotFound)
		}
		return err
	}

	// Output results
	if jsonOutput {
		return output.PrintJSON(device)
	}

	output.PrintDeviceInfo(device)
	return nil
}
