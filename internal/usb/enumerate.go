package usb

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lazaroagomez/wusbkit/internal/powershell"
)

// Enumerator provides USB device enumeration capabilities
type Enumerator struct {
	ps *powershell.Executor
}

// NewEnumerator creates a new USB device enumerator
func NewEnumerator() *Enumerator {
	return &Enumerator{
		ps: powershell.NewExecutor(0),
	}
}

// ListDevices returns all connected USB storage devices
func (e *Enumerator) ListDevices() ([]Device, error) {
	// Get USB disks
	disks, err := e.getUSBDisks()
	if err != nil {
		return nil, fmt.Errorf("failed to get USB disks: %w", err)
	}

	if len(disks) == 0 {
		return []Device{}, nil
	}

	// Get VID/PID mapping from Win32_DiskDrive
	vidPidMap, err := e.getVIDPIDMap()
	if err != nil {
		// Non-fatal, continue without VID/PID
		vidPidMap = make(map[int]struct{ VID, PID string })
	}

	// Build device list
	devices := make([]Device, 0, len(disks))
	for _, disk := range disks {
		device := Device{
			DiskNumber:     disk.Number,
			FriendlyName:   disk.FriendlyName,
			Model:          disk.Model,
			SerialNumber:   disk.SerialNumber,
			Size:           disk.Size,
			SizeHuman:      FormatSize(disk.Size),
			PartitionStyle: disk.PartitionStyle,
			HealthStatus:   disk.HealthStatus,
			BusType:        "USB",
			Status:         e.getOperationalStatus(disk.OperationalStatus),
		}

		// Add VID/PID if available
		if vp, ok := vidPidMap[disk.Number]; ok {
			device.VendorID = vp.VID
			device.ProductID = vp.PID
		}

		// Get partition and volume info
		partInfo, err := e.getPartitionInfo(disk.Number)
		if err == nil && partInfo != nil {
			device.DriveLetter = partInfo.DriveLetter
			device.FileSystem = partInfo.FileSystem
			device.VolumeLabel = partInfo.VolumeLabel
		}

		devices = append(devices, device)
	}

	return devices, nil
}

// GetDeviceByDriveLetter returns detailed info for a specific USB device
func (e *Enumerator) GetDeviceByDriveLetter(driveLetter string) (*Device, error) {
	// Normalize drive letter (remove colon if present)
	driveLetter = strings.TrimSuffix(strings.ToUpper(driveLetter), ":")
	if len(driveLetter) != 1 || driveLetter[0] < 'A' || driveLetter[0] > 'Z' {
		return nil, fmt.Errorf("invalid drive letter: %s", driveLetter)
	}

	devices, err := e.ListDevices()
	if err != nil {
		return nil, err
	}

	for _, device := range devices {
		if strings.TrimSuffix(device.DriveLetter, ":") == driveLetter {
			return &device, nil
		}
	}

	return nil, fmt.Errorf("USB drive %s: not found", driveLetter)
}

// GetDeviceByDiskNumber returns detailed info for a specific USB device by disk number
func (e *Enumerator) GetDeviceByDiskNumber(diskNumber int) (*Device, error) {
	devices, err := e.ListDevices()
	if err != nil {
		return nil, err
	}

	for _, device := range devices {
		if device.DiskNumber == diskNumber {
			return &device, nil
		}
	}

	return nil, fmt.Errorf("USB disk %d: not found", diskNumber)
}

// GetDevice returns a USB device by disk number or drive letter.
// It accepts identifiers like "2" (disk number) or "E" / "E:" (drive letter).
func (e *Enumerator) GetDevice(identifier string) (*Device, error) {
	// Try to parse as disk number first
	if diskNum, err := strconv.Atoi(identifier); err == nil {
		return e.GetDeviceByDiskNumber(diskNum)
	}
	return e.GetDeviceByDriveLetter(identifier)
}

// getUSBDisks returns all USB disk drives
func (e *Enumerator) getUSBDisks() ([]psRawDisk, error) {
	cmd := `Get-Disk | Where-Object {$_.BusType -eq 'USB'} | Select-Object Number, FriendlyName, Model, SerialNumber, Size, PartitionStyle, HealthStatus, OperationalStatus, BusType`

	var disks []psRawDisk
	if err := e.ps.ExecuteJSONArray(cmd, &disks); err != nil {
		return nil, err
	}

	return disks, nil
}

// getVIDPIDMap returns a map of disk index to VID/PID
func (e *Enumerator) getVIDPIDMap() (map[int]struct{ VID, PID string }, error) {
	cmd := `Get-CimInstance Win32_DiskDrive -Filter "InterfaceType='USB'" | Select-Object Index, PNPDeviceID`

	var drives []psRawWin32DiskDrive
	if err := e.ps.ExecuteJSONArray(cmd, &drives); err != nil {
		return nil, err
	}

	result := make(map[int]struct{ VID, PID string })
	for _, drive := range drives {
		vid, pid := ParseVIDPID(drive.PNPDeviceID)
		if vid != "" && pid != "" {
			result[drive.Index] = struct{ VID, PID string }{vid, pid}
		}
	}

	return result, nil
}

// partitionInfo holds volume information for a partition
type partitionInfo struct {
	DriveLetter string
	FileSystem  string
	VolumeLabel string
}

// getPartitionInfo returns the first partition's volume info for a disk
func (e *Enumerator) getPartitionInfo(diskNumber int) (*partitionInfo, error) {
	cmd := fmt.Sprintf(`Get-Partition -DiskNumber %d -ErrorAction SilentlyContinue | Where-Object {$_.DriveLetter} | Select-Object -First 1 DriveLetter`, diskNumber)

	var partitions []psRawPartition
	if err := e.ps.ExecuteJSONArray(cmd, &partitions); err != nil {
		return nil, err
	}

	if len(partitions) == 0 {
		return nil, nil
	}

	driveLetter := partitions[0].DriveLetter
	if driveLetter == "" {
		return nil, nil
	}

	// Get volume info
	volCmd := fmt.Sprintf(`Get-Volume -DriveLetter '%s' -ErrorAction SilentlyContinue | Select-Object DriveLetter, FileSystemLabel, FileSystem`, driveLetter)

	var volumes []psRawVolume
	if err := e.ps.ExecuteJSONArray(volCmd, &volumes); err != nil {
		return nil, err
	}

	if len(volumes) == 0 {
		return &partitionInfo{DriveLetter: driveLetter + ":"}, nil
	}

	return &partitionInfo{
		DriveLetter: driveLetter + ":",
		FileSystem:  volumes[0].FileSystem,
		VolumeLabel: volumes[0].FileSystemLabel,
	}, nil
}

// getOperationalStatus converts operational status to string
func (e *Enumerator) getOperationalStatus(status interface{}) string {
	switch v := status.(type) {
	case float64:
		switch int(v) {
		case 2:
			return "Online"
		case 0xD010:
			return "Online"
		case 0xD012:
			return "No Media"
		default:
			return "Unknown"
		}
	case string:
		return v
	default:
		return "Unknown"
	}
}

// IsSystemDisk checks if a disk contains system/boot/recovery partitions
func (e *Enumerator) IsSystemDisk(diskNumber int) (bool, error) {
	// Check for System, Reserved, or Recovery partitions, or if C: drive is on this disk
	cmd := fmt.Sprintf(`
		$parts = Get-Partition -DiskNumber %d -ErrorAction SilentlyContinue | Where-Object {
			$_.Type -eq 'System' -or
			$_.Type -eq 'Reserved' -or
			$_.Type -eq 'Recovery' -or
			$_.DriveLetter -eq 'C'
		}
		if ($parts) { 'true' } else { 'false' }
	`, diskNumber)

	output, err := e.ps.Execute(cmd)
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(string(output)) == "true", nil
}
