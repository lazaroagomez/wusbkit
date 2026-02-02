package output

import (
	"github.com/lazaroagomez/wusbkit/internal/usb"
	"github.com/pterm/pterm"
)

// PrintDevicesTable prints a table of USB devices
func PrintDevicesTable(devices []usb.Device, verbose bool) {
	if len(devices) == 0 {
		pterm.Info.Println("No USB drives found")
		return
	}

	if verbose {
		printVerboseTable(devices)
	} else {
		printSimpleTable(devices)
	}
}

func printSimpleTable(devices []usb.Device) {
	tableData := pterm.TableData{
		{"Drive", "Name", "Size", "Status"},
	}

	for _, d := range devices {
		drive := d.DriveLetter
		if drive == "" {
			drive = "(no letter)"
		}

		status := formatStatus(d.HealthStatus)

		tableData = append(tableData, []string{
			drive,
			d.FriendlyName,
			d.SizeHuman,
			status,
		})
	}

	pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(tableData).Render()
}

func printVerboseTable(devices []usb.Device) {
	tableData := pterm.TableData{
		{"Drive", "Name", "Size", "Serial", "VID:PID", "Port", "FS", "Partition", "Status"},
	}

	for _, d := range devices {
		drive := d.DriveLetter
		if drive == "" {
			drive = "(no letter)"
		}

		vidPid := ""
		if d.VendorID != "" && d.ProductID != "" {
			vidPid = d.VendorID + ":" + d.ProductID
		}

		port := usb.ParsePortNumber(d.LocationInfo)
		if port == "" {
			port = "-"
		}

		fs := d.FileSystem
		if fs == "" {
			fs = "-"
		}

		status := formatStatus(d.HealthStatus)

		tableData = append(tableData, []string{
			drive,
			d.FriendlyName,
			d.SizeHuman,
			d.SerialNumber,
			vidPid,
			port,
			fs,
			d.PartitionStyle,
			status,
		})
	}

	pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(tableData).Render()
}

// PrintDeviceInfo prints detailed information about a single device
func PrintDeviceInfo(device *usb.Device) {
	pterm.DefaultSection.Println(device.FriendlyName)

	pairs := [][]string{
		{"Drive Letter", device.DriveLetter},
		{"Disk Number", pterm.Sprintf("%d", device.DiskNumber)},
		{"Model", device.Model},
		{"Size", device.SizeHuman},
		{"Serial Number", device.SerialNumber},
	}

	if device.VendorID != "" {
		pairs = append(pairs, []string{"Vendor ID", device.VendorID})
	}
	if device.ProductID != "" {
		pairs = append(pairs, []string{"Product ID", device.ProductID})
	}

	// Add location info if available
	if device.LocationInfo != "" {
		port := usb.ParsePortNumber(device.LocationInfo)
		pairs = append(pairs, []string{"Hub Port", port})
		pairs = append(pairs, []string{"Location Info", device.LocationInfo})
	}
	if device.ParentInstanceId != "" {
		pairs = append(pairs, []string{"Parent Hub", device.ParentInstanceId})
	}

	pairs = append(pairs,
		[]string{"File System", valueOrDash(device.FileSystem)},
		[]string{"Volume Label", valueOrDash(device.VolumeLabel)},
		[]string{"Partition Style", device.PartitionStyle},
		[]string{"Bus Type", device.BusType},
		[]string{"Health Status", formatStatus(device.HealthStatus)},
		[]string{"Status", device.Status},
	)

	tableData := pterm.TableData{}
	for _, pair := range pairs {
		tableData = append(tableData, pair)
	}

	pterm.DefaultTable.WithData(tableData).Render()
}

func formatStatus(status string) string {
	switch status {
	case "Healthy":
		return pterm.Green(status)
	case "Warning":
		return pterm.Yellow(status)
	case "Unhealthy":
		return pterm.Red(status)
	default:
		return status
	}
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
