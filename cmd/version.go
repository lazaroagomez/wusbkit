package cmd

import (
	"runtime"

	"github.com/lazaroagomez/wusbkit/internal/output"
	"github.com/lazaroagomez/wusbkit/internal/powershell"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display version information for wusbkit including build details.`,
	RunE:  runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

type versionInfo struct {
	Version     string `json:"version"`
	BuildDate   string `json:"buildDate"`
	GoVersion   string `json:"goVersion"`
	Platform    string `json:"platform"`
	PwshVersion string `json:"pwshVersion,omitempty"`
}

func runVersion(cmd *cobra.Command, args []string) error {
	info := versionInfo{
		Version:   Version,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	// Try to get PowerShell version
	if pwshVer, err := powershell.GetPwshVersion(); err == nil {
		info.PwshVersion = pwshVer
	}

	if jsonOutput {
		return output.PrintJSON(info)
	}

	pterm.DefaultSection.Println("wusbkit")

	tableData := pterm.TableData{
		{"Version", info.Version},
		{"Build Date", info.BuildDate},
		{"Go Version", info.GoVersion},
		{"Platform", info.Platform},
	}

	if info.PwshVersion != "" {
		tableData = append(tableData, []string{"PowerShell", info.PwshVersion})
	}

	pterm.DefaultTable.WithData(tableData).Render()

	return nil
}
