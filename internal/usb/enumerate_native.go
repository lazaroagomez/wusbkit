package usb

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/StackExchange/wmi"
	"golang.org/x/sync/errgroup"
)

// Win32_DiskDrive represents WMI Win32_DiskDrive class
type Win32_DiskDrive struct {
	Index         uint32
	Model         string
	SerialNumber  string
	Size          uint64
	InterfaceType string
	PNPDeviceID   string
	MediaType     string
	Status        string
}

// Win32_DiskPartition represents WMI Win32_DiskPartition class
type Win32_DiskPartition struct {
	DiskIndex   uint32
	Index       uint32
	DeviceID    string
	Size        uint64
	Type        string
	Bootable    bool
	PrimaryPartition bool
}

// Win32_LogicalDiskToPartition represents the association
type Win32_LogicalDiskToPartition struct {
	Antecedent string
	Dependent  string
}

// Win32_LogicalDisk represents WMI Win32_LogicalDisk class
type Win32_LogicalDisk struct {
	DeviceID     string
	FileSystem   string
	VolumeName   string
	Size         uint64
	FreeSpace    uint64
	DriveType    uint32
}

// MSFT_Disk represents Storage WMI MSFT_Disk class (more detailed)
type MSFT_Disk struct {
	Number            uint32
	FriendlyName      string
	Model             string
	SerialNumber      string
	Size              uint64
	PartitionStyle    uint16
	HealthStatus      uint16
	OperationalStatus []uint16
	BusType           uint16
}

// MSFT_Partition represents Storage WMI MSFT_Partition class
type MSFT_Partition struct {
	DiskNumber  uint32
	DriveLetter uint16
	Size        uint64
}

// MSFT_Volume represents Storage WMI MSFT_Volume class
type MSFT_Volume struct {
	DriveLetter     uint16
	FileSystemLabel string
	FileSystem      string
	Size            uint64
	SizeRemaining   uint64
}

// listDevicesNative enumerates USB devices using native WMI (no PowerShell)
// This is significantly faster than PowerShell, especially with many devices.
// WMI queries are run in parallel for maximum performance.
func (e *Enumerator) listDevicesNative() ([]Device, error) {
	// Data containers for parallel queries
	var diskDrives []Win32_DiskDrive
	var partitions []Win32_DiskPartition
	var associations []Win32_LogicalDiskToPartition
	var logicalDisks []Win32_LogicalDisk
	var mu sync.Mutex

	// Run all WMI queries in parallel using errgroup
	g, _ := errgroup.WithContext(context.Background())

	// Query USB disk drives (this is the critical one that must succeed)
	g.Go(func() error {
		query := "SELECT Index, Model, SerialNumber, Size, InterfaceType, PNPDeviceID, MediaType, Status FROM Win32_DiskDrive WHERE InterfaceType='USB'"
		var drives []Win32_DiskDrive
		if err := wmi.Query(query, &drives); err != nil {
			return fmt.Errorf("WMI query failed: %w", err)
		}
		mu.Lock()
		diskDrives = drives
		mu.Unlock()
		return nil
	})

	// Query all partitions (non-fatal if fails)
	g.Go(func() error {
		var parts []Win32_DiskPartition
		wmi.Query("SELECT DiskIndex, Index, DeviceID FROM Win32_DiskPartition", &parts)
		mu.Lock()
		partitions = parts
		mu.Unlock()
		return nil
	})

	// Query logical disk to partition associations (non-fatal if fails)
	g.Go(func() error {
		var assocs []Win32_LogicalDiskToPartition
		wmi.Query("SELECT Antecedent, Dependent FROM Win32_LogicalDiskToPartition", &assocs)
		mu.Lock()
		associations = assocs
		mu.Unlock()
		return nil
	})

	// Query logical disks (non-fatal if fails)
	g.Go(func() error {
		var disks []Win32_LogicalDisk
		wmi.Query("SELECT DeviceID, FileSystem, VolumeName FROM Win32_LogicalDisk WHERE DriveType=2", &disks)
		mu.Lock()
		logicalDisks = disks
		mu.Unlock()
		return nil
	})

	// Wait for all queries to complete
	if err := g.Wait(); err != nil {
		return nil, err
	}

	if len(diskDrives) == 0 {
		return []Device{}, nil
	}

	// Build partition to drive letter mapping
	partitionToDrive := make(map[string]string)
	for _, assoc := range associations {
		// Parse: \\COMPUTER\root\cimv2:Win32_DiskPartition.DeviceID="Disk #0, Partition #0"
		// and: \\COMPUTER\root\cimv2:Win32_LogicalDisk.DeviceID="E:"
		partDeviceID := extractDeviceID(assoc.Antecedent)
		driveDeviceID := extractDeviceID(assoc.Dependent)
		if partDeviceID != "" && driveDeviceID != "" {
			partitionToDrive[partDeviceID] = driveDeviceID
		}
	}

	// Build disk index to partition DeviceID mapping
	diskToPartition := make(map[uint32]string)
	for _, part := range partitions {
		if _, exists := diskToPartition[part.DiskIndex]; !exists {
			diskToPartition[part.DiskIndex] = part.DeviceID
		}
	}

	// Build drive letter to logical disk mapping
	driveToLogical := make(map[string]Win32_LogicalDisk)
	for _, ld := range logicalDisks {
		driveToLogical[ld.DeviceID] = ld
	}

	// Build device list
	devices := make([]Device, 0, len(diskDrives))
	for _, disk := range diskDrives {
		vid, pid := ParseVIDPID(disk.PNPDeviceID)

		device := Device{
			DiskNumber:   int(disk.Index),
			FriendlyName: disk.Model,
			Model:        disk.Model,
			SerialNumber: strings.TrimSpace(disk.SerialNumber),
			Size:         int64(disk.Size),
			SizeHuman:    FormatSize(int64(disk.Size)),
			BusType:      "USB",
			VendorID:     vid,
			ProductID:    pid,
			Status:       disk.Status,
			MediaType:    disk.MediaType,
		}

		// Find drive letter via partition association
		if partDeviceID, ok := diskToPartition[disk.Index]; ok {
			if driveLetter, ok := partitionToDrive[partDeviceID]; ok {
				device.DriveLetter = driveLetter
				if ld, ok := driveToLogical[driveLetter]; ok {
					device.FileSystem = ld.FileSystem
					device.VolumeLabel = ld.VolumeName
				}
			}
		}

		devices = append(devices, device)
	}

	return devices, nil
}

// extractDeviceID extracts the DeviceID value from a WMI object path
// Example: \\COMPUTER\root\cimv2:Win32_LogicalDisk.DeviceID="E:" -> "E:"
func extractDeviceID(path string) string {
	// Find DeviceID="..."
	idx := strings.Index(path, `DeviceID="`)
	if idx == -1 {
		return ""
	}
	start := idx + len(`DeviceID="`)
	end := strings.Index(path[start:], `"`)
	if end == -1 {
		return ""
	}
	return path[start : start+end]
}
