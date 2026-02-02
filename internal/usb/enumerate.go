package usb

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lazaroagomez/wusbkit/internal/powershell"
)

// deviceCache holds cached device enumeration results
type deviceCache struct {
	devices   []Device
	timestamp time.Time
	mu        sync.RWMutex
}

const cacheTTL = 2 * time.Second

// Enumerator provides USB device enumeration capabilities
type Enumerator struct {
	ps    *powershell.Executor
	cache deviceCache
}

// NewEnumerator creates a new USB device enumerator
func NewEnumerator() *Enumerator {
	return &Enumerator{
		ps: powershell.NewExecutor(0),
	}
}

// batchEnumerateResult holds all data from a single batched PowerShell query
type batchEnumerateResult struct {
	Disks      []psRawDisk           `json:"Disks"`
	VidPid     []psRawWin32DiskDrive `json:"VidPid"`
	Partitions []psRawPartition      `json:"Partitions"`
	Volumes    []psRawVolume         `json:"Volumes"`
	Locations  []psRawLocation       `json:"Locations"`
}

// psRawLocation holds USB hub port location info from PowerShell
type psRawLocation struct {
	Index            int    `json:"Index"`
	LocationInfo     string `json:"LocationInfo"`
	ParentInstanceId string `json:"ParentInstanceId"`
}

// Batched PowerShell script that collects all USB device data in one execution
// This reduces 8+ PowerShell process spawns to just 1
const batchEnumerateScript = `
$disks = @(Get-Disk | Where-Object {$_.BusType -eq 'USB'} | Select-Object Number, FriendlyName, Model, SerialNumber, Size, PartitionStyle, HealthStatus, OperationalStatus, BusType)
$vidpid = @(Get-CimInstance Win32_DiskDrive -Filter "InterfaceType='USB'" -ErrorAction SilentlyContinue | Select-Object Index, PNPDeviceID)
$partitions = @(Get-Partition -ErrorAction SilentlyContinue | Where-Object {$_.DriveLetter} | Select-Object DiskNumber, DriveLetter)
$volumes = @(Get-Volume -ErrorAction SilentlyContinue | Where-Object {$_.DriveLetter} | Select-Object DriveLetter, FileSystemLabel, FileSystem)

# Get USB hub port location info by walking up the device tree
$locations = @()
foreach ($drive in $vidpid) {
    $currentId = $drive.PNPDeviceID
    $locInfo = $null
    $parentId = $null
    for ($i = 0; $i -lt 10 -and $currentId; $i++) {
        $loc = (Get-PnpDeviceProperty -InstanceId $currentId -KeyName 'DEVPKEY_Device_LocationInfo' -ErrorAction SilentlyContinue).Data
        if ($loc -and $loc -match 'Port') {
            $locInfo = $loc
            $parentId = (Get-PnpDeviceProperty -InstanceId $currentId -KeyName 'DEVPKEY_Device_Parent' -ErrorAction SilentlyContinue).Data
            break
        }
        $currentId = (Get-PnpDeviceProperty -InstanceId $currentId -KeyName 'DEVPKEY_Device_Parent' -ErrorAction SilentlyContinue).Data
    }
    $locations += @{Index=$drive.Index; LocationInfo=$locInfo; ParentInstanceId=$parentId}
}

@{Disks=$disks; VidPid=$vidpid; Partitions=$partitions; Volumes=$volumes; Locations=$locations} | ConvertTo-Json -Depth 10 -Compress
`

// ListDevices returns all connected USB storage devices
// Uses caching to avoid repeated calls within the TTL window
func (e *Enumerator) ListDevices() ([]Device, error) {
	// Check cache first
	e.cache.mu.RLock()
	if time.Since(e.cache.timestamp) < cacheTTL && e.cache.devices != nil {
		devices := e.cache.devices
		e.cache.mu.RUnlock()
		return devices, nil
	}
	e.cache.mu.RUnlock()

	// Try native WMI first (fastest - no PowerShell process spawn)
	devices, err := e.listDevicesNative()
	if err != nil {
		// Fallback to batched PowerShell if native WMI fails
		devices, err = e.listDevicesBatched()
		if err != nil {
			return nil, err
		}
	}

	// Update cache
	e.cache.mu.Lock()
	e.cache.devices = devices
	e.cache.timestamp = time.Now()
	e.cache.mu.Unlock()

	return devices, nil
}

// listDevicesBatched retrieves all USB device data in a single PowerShell execution
// This is the main performance optimization - reduces 8+ process spawns to 1
func (e *Enumerator) listDevicesBatched() ([]Device, error) {
	output, err := e.ps.Execute(batchEnumerateScript)
	if err != nil {
		// Fallback to legacy method if batch fails
		return e.listDevicesLegacy()
	}

	var result batchEnumerateResult
	if err := parseJSON(output, &result); err != nil {
		// Fallback to legacy method if parsing fails
		return e.listDevicesLegacy()
	}

	if len(result.Disks) == 0 {
		return []Device{}, nil
	}

	// Build VID/PID lookup map
	vidPidMap := make(map[int]struct{ VID, PID string })
	for _, drive := range result.VidPid {
		vid, pid := ParseVIDPID(drive.PNPDeviceID)
		if vid != "" && pid != "" {
			vidPidMap[drive.Index] = struct{ VID, PID string }{vid, pid}
		}
	}

	// Build location lookup map (disk index -> location info)
	locationMap := make(map[int]struct{ LocationInfo, ParentInstanceId string })
	for _, loc := range result.Locations {
		locationMap[loc.Index] = struct{ LocationInfo, ParentInstanceId string }{
			LocationInfo:     loc.LocationInfo,
			ParentInstanceId: loc.ParentInstanceId,
		}
	}

	// Build partition lookup map (DiskNumber -> DriveLetter)
	partitionMap := make(map[int]string)
	for _, part := range result.Partitions {
		if part.DriveLetter != "" {
			partitionMap[part.DiskNumber] = part.DriveLetter
		}
	}

	// Build volume lookup map (DriveLetter -> volume info)
	volumeMap := make(map[string]psRawVolume)
	for _, vol := range result.Volumes {
		if vol.DriveLetter != "" {
			volumeMap[vol.DriveLetter] = vol
		}
	}

	// Build device list
	devices := make([]Device, 0, len(result.Disks))
	for _, disk := range result.Disks {
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

		// Add partition/volume info if available
		if driveLetter, ok := partitionMap[disk.Number]; ok {
			device.DriveLetter = driveLetter + ":"
			if vol, ok := volumeMap[driveLetter]; ok {
				device.FileSystem = vol.FileSystem
				device.VolumeLabel = vol.FileSystemLabel
			}
		}

		// Add location info if available
		if loc, ok := locationMap[disk.Number]; ok {
			device.LocationInfo = loc.LocationInfo
			device.ParentInstanceId = loc.ParentInstanceId
		}

		devices = append(devices, device)
	}

	return devices, nil
}

// parseJSON unmarshals JSON output from PowerShell
func parseJSON(data []byte, target interface{}) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return json.Unmarshal([]byte(trimmed), target)
}

// listDevicesLegacy is the original implementation used as fallback
func (e *Enumerator) listDevicesLegacy() ([]Device, error) {
	// Get USB disks
	disks, err := e.getUSBDisks()
	if err != nil {
		return nil, fmt.Errorf("failed to get USB disks: %w", err)
	}

	if len(disks) == 0 {
		return []Device{}, nil
	}

	// Get VID/PID mapping from Win32_DiskDrive
	vidPidMap, pnpDeviceIDMap, err := e.getVIDPIDAndPNPMap()
	if err != nil {
		// Non-fatal, continue without VID/PID
		vidPidMap = make(map[int]struct{ VID, PID string })
		pnpDeviceIDMap = make(map[int]string)
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

		// Get hub port location info using native cfgmgr32
		if pnpID, ok := pnpDeviceIDMap[disk.Number]; ok {
			locInfo, parentID, _ := GetHubPortLocation(pnpID)
			device.LocationInfo = locInfo
			device.ParentInstanceId = parentID
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

// getVIDPIDMap returns a map of disk index to VID/PID (for backwards compatibility)
func (e *Enumerator) getVIDPIDMap() (map[int]struct{ VID, PID string }, error) {
	vidPidMap, _, err := e.getVIDPIDAndPNPMap()
	return vidPidMap, err
}

// getVIDPIDAndPNPMap returns maps of disk index to VID/PID and PNPDeviceID
func (e *Enumerator) getVIDPIDAndPNPMap() (map[int]struct{ VID, PID string }, map[int]string, error) {
	cmd := `Get-CimInstance Win32_DiskDrive -Filter "InterfaceType='USB'" | Select-Object Index, PNPDeviceID`

	var drives []psRawWin32DiskDrive
	if err := e.ps.ExecuteJSONArray(cmd, &drives); err != nil {
		return nil, nil, err
	}

	vidPidResult := make(map[int]struct{ VID, PID string })
	pnpResult := make(map[int]string)
	for _, drive := range drives {
		vid, pid := ParseVIDPID(drive.PNPDeviceID)
		if vid != "" && pid != "" {
			vidPidResult[drive.Index] = struct{ VID, PID string }{vid, pid}
		}
		pnpResult[drive.Index] = drive.PNPDeviceID
	}

	return vidPidResult, pnpResult, nil
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
