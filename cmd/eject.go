package cmd

import (
	"fmt"
	"strconv"

	"github.com/lazaroagomez/wusbkit/internal/output"
	"github.com/lazaroagomez/wusbkit/internal/powershell"
	"github.com/lazaroagomez/wusbkit/internal/usb"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var ejectYes bool

var ejectCmd = &cobra.Command{
	Use:   "eject <drive>",
	Short: "Safely eject a USB drive",
	Long: `Safely eject a USB storage device.

This performs the same action as "Safely Remove Hardware" in Windows,
ensuring all pending writes are flushed before ejecting.

The drive can be specified by:
  - Drive letter (e.g., E: or E)
  - Disk number (e.g., 2)`,
	Example: `  wusbkit eject E:
  wusbkit eject E
  wusbkit eject 2
  wusbkit eject E: --yes`,
	Args: cobra.ExactArgs(1),
	RunE: runEject,
}

func init() {
	ejectCmd.Flags().BoolVarP(&ejectYes, "yes", "y", false, "Skip confirmation prompt")
	rootCmd.AddCommand(ejectCmd)
}

func runEject(cmd *cobra.Command, args []string) error {
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

	// Check if device has a drive letter (required for eject)
	if device.DriveLetter == "" {
		errMsg := fmt.Sprintf("USB disk %d has no drive letter assigned - cannot eject", device.DiskNumber)
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeInvalidInput)
		} else {
			PrintError(errMsg, output.ErrCodeInvalidInput)
		}
		return fmt.Errorf(errMsg)
	}

	// Confirmation prompt (unless --yes or --json)
	if !ejectYes && !jsonOutput {
		pterm.Info.Printf("Ejecting %s (%s - %s)\n",
			device.DriveLetter, device.FriendlyName, device.SizeHuman)

		confirmed, _ := pterm.DefaultInteractiveConfirm.
			WithDefaultValue(true).
			Show("Continue?")

		if !confirmed {
			pterm.Info.Println("Eject cancelled")
			return nil
		}
	}

	// Perform eject using Shell.Application COM object
	driveLetter := device.DriveLetter
	if len(driveLetter) == 2 && driveLetter[1] == ':' {
		driveLetter = driveLetter[:1] + ":"
	}

	ps := powershell.NewExecutor(0)
	ejectScript := fmt.Sprintf(`
$shell = New-Object -ComObject Shell.Application
$drive = $shell.Namespace(17).ParseName("%s")
if ($drive) {
    $drive.InvokeVerb("Eject")
    Write-Output "OK"
} else {
    Write-Error "Drive not found"
    exit 1
}
`, driveLetter)

	_, err = ps.Execute(ejectScript)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to eject %s: %v", device.DriveLetter, err)
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeInternalError)
		} else {
			PrintError(errMsg, output.ErrCodeInternalError)
		}
		return err
	}

	// Output success
	if jsonOutput {
		result := map[string]interface{}{
			"success":     true,
			"driveLetter": device.DriveLetter,
			"diskNumber":  device.DiskNumber,
			"message":     fmt.Sprintf("Successfully ejected %s", device.DriveLetter),
		}
		return output.PrintJSON(result)
	}

	pterm.Success.Printf("Successfully ejected %s (%s)\n", device.DriveLetter, device.FriendlyName)
	return nil
}
