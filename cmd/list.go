package cmd

import (
	"errors"

	"github.com/lazaroagomez/wusbkit/internal/output"
	"github.com/lazaroagomez/wusbkit/internal/powershell"
	"github.com/lazaroagomez/wusbkit/internal/usb"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all connected USB drives",
	Long: `List all USB storage devices connected to the system.

By default, shows drive letter, name, size, and status.
Use --verbose to see additional details like serial number, VID/PID, and filesystem.`,
	Example: `  wusbkit list
  wusbkit list --verbose
  wusbkit list --json`,
	RunE: runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	// Check PowerShell availability
	if err := powershell.CheckPwshAvailable(); err != nil {
		if jsonOutput {
			output.PrintJSONError("PowerShell 7 (pwsh.exe) is required but not found", output.ErrCodePwshNotFound)
		} else {
			PrintError("PowerShell 7 (pwsh.exe) is required but not found", output.ErrCodePwshNotFound)
		}
		return err
	}

	// Enumerate USB devices
	enum := usb.NewEnumerator()
	devices, err := enum.ListDevices()
	if err != nil {
		if jsonOutput {
			if errors.Is(err, powershell.ErrPwshNotFound) {
				output.PrintJSONError(err.Error(), output.ErrCodePwshNotFound)
			} else {
				output.PrintJSONError(err.Error(), output.ErrCodeInternalError)
			}
		} else {
			PrintError(err.Error(), output.ErrCodeInternalError)
		}
		return err
	}

	// Output results
	if jsonOutput {
		return output.PrintJSON(devices)
	}

	output.PrintDevicesTable(devices, verbose)
	return nil
}
