package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lazaroagomez/wusbkit/internal/output"
	"github.com/lazaroagomez/wusbkit/internal/usb"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var labelName string

var labelCmd = &cobra.Command{
	Use:   "label <drive>",
	Short: "Set volume label for a USB drive",
	Long: `Changes the volume label of a USB drive without reformatting.

The drive must be specified by drive letter (e.g., E: or E).
This operation does not require administrator privileges for USB drives.`,
	Example: `  wusbkit label E: --name "BACKUP_001"
  wusbkit label F --name "USB_DATA" --json`,
	Args: cobra.ExactArgs(1),
	RunE: runLabel,
}

func init() {
	labelCmd.Flags().StringVar(&labelName, "name", "", "New volume label (required)")
	labelCmd.MarkFlagRequired("name")
	rootCmd.AddCommand(labelCmd)
}

func runLabel(cmd *cobra.Command, args []string) error {
	// Parse and validate drive letter
	driveLetter := strings.TrimSuffix(strings.ToUpper(args[0]), ":")
	if len(driveLetter) != 1 || driveLetter[0] < 'A' || driveLetter[0] > 'Z' {
		errMsg := fmt.Sprintf("invalid drive letter: %s", args[0])
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeInvalidInput)
		} else {
			PrintError(errMsg, output.ErrCodeInvalidInput)
		}
		return errors.New(errMsg)
	}

	// Validate label is not empty
	if strings.TrimSpace(labelName) == "" {
		errMsg := "label name cannot be empty"
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeInvalidInput)
		} else {
			PrintError(errMsg, output.ErrCodeInvalidInput)
		}
		return errors.New(errMsg)
	}

	// Verify it's a USB drive
	enum := usb.NewEnumerator()
	device, err := enum.GetDeviceByDriveLetter(driveLetter)
	if err != nil {
		if jsonOutput {
			output.PrintJSONError(err.Error(), output.ErrCodeUSBNotFound)
		} else {
			PrintError(err.Error(), output.ErrCodeUSBNotFound)
		}
		return err
	}
	if device == nil {
		errMsg := fmt.Sprintf("drive %s: not found or not a USB device", driveLetter)
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeUSBNotFound)
		} else {
			PrintError(errMsg, output.ErrCodeUSBNotFound)
		}
		return errors.New(errMsg)
	}

	// Use Windows label command (no admin required for USB drives)
	labelExec := exec.Command("label", driveLetter+":", labelName)
	if err := labelExec.Run(); err != nil {
		errMsg := fmt.Sprintf("failed to set label: %v", err)
		if jsonOutput {
			output.PrintJSONError(errMsg, output.ErrCodeInternalError)
		} else {
			PrintError(errMsg, output.ErrCodeInternalError)
		}
		return err
	}

	// Output result
	if jsonOutput {
		return PrintJSON(map[string]interface{}{
			"success":     true,
			"driveLetter": driveLetter + ":",
			"label":       labelName,
		})
	}

	pterm.Success.Printf("Label set to \"%s\" on drive %s:\n", labelName, driveLetter)
	return nil
}
