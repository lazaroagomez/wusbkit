# PowerShell storage cmdlets for USB device management

PowerShell's Storage module provides a complete suite of cmdlets for USB device management, with **BusType value 7** being the key identifier for USB-connected drives. The modern approach uses CIM-based cmdlets in the `Root\Microsoft\Windows\Storage` namespace, replacing deprecated WMI methods. Windows Server 2025 and PowerShell 7.x maintain full compatibility with these cmdlets while adding thin provisioning and improved NVMe support.

## Get-Disk returns logical disk objects visible to the OS

The `Get-Disk` cmdlet returns `MSFT_Disk` objects representing logical disks. For USB management tools, these properties are most critical:

**Identification properties:**
- `Number` (UInt32) — OS disk number, used for all disk operations
- `FriendlyName` (String) — User-friendly display name
- `SerialNumber` (String) — Manufacturer-assigned serial number
- `UniqueId` (String) — VPD Page 0x83 unique identifier
- `Path` (String) — OS handle path to device

**USB identification and hardware:**
- `BusType` (UInt16) — **Value 7 = USB** (other values: 0=Unknown, 11=SATA, 17=NVMe)
- `Model` (String) — Model number
- `Manufacturer` (String) — Hardware manufacturer
- `FirmwareVersion` (String) — Firmware revision

**Size and partition properties:**
- `Size` (UInt64) — Total size in bytes
- `AllocatedSize` (UInt64) — Currently allocated space
- `PartitionStyle` (UInt16) — **0=Unknown, 1=MBR, 2=GPT**
- `NumberOfPartitions` (UInt32) — Partition count
- `LogicalSectorSize` / `PhysicalSectorSize` (UInt32) — Sector sizes in bytes

**Status and flags:**
- `HealthStatus` (UInt16) — **0=Healthy, 1=Warning, 2=Unhealthy**
- `OperationalStatus` (UInt16) — 2=OK, 0xD010=Online, 0xD012=No Media
- `IsSystem`, `IsBoot`, `IsOffline`, `IsReadOnly` (Boolean)

```powershell
# List USB disks only
Get-Disk | Where-Object {$_.BusType -eq 'USB'}

# Get comprehensive USB disk info
Get-Disk | Where-Object {$_.BusType -eq 'USB'} | 
    Select-Object Number, FriendlyName, SerialNumber, Size, PartitionStyle, HealthStatus
```

## Get-PhysicalDisk exposes hardware-level details

The `Get-PhysicalDisk` cmdlet returns `MSFT_PhysicalDisk` objects with deeper hardware information. **Important note:** VendorId and ProductId (VID/PID) are not directly exposed—use `Win32_DiskDrive.PNPDeviceID` via CIM for USB VID/PID.

**Hardware identification:**
- `DeviceId` (String) — Unique device address
- `FriendlyName` (String) — Display name (modifiable)
- `SerialNumber` (String) — Hardware serial number
- `FirmwareVersion` (String) — Firmware revision
- `Model` (String) — Model designation
- `PartNumber` (String) — SKU or part number

**Media and bus classification:**
- `BusType` (UInt16) — **7=USB** (consistent with Get-Disk)
- `MediaType` (UInt16) — **0=Unspecified (typical for USB flash), 3=HDD, 4=SSD**
- `SpindleSpeed` (UInt32) — RPM for HDDs (0 for SSDs)

**Health and pool eligibility:**
- `HealthStatus` (UInt16) — 0=Healthy, 1=Warning, 2=Unhealthy, 5=Unknown
- `CanPool` (Boolean) — Storage Spaces eligibility (usually False for USB)
- `CannotPoolReason` (UInt16[]) — Typically "Removable Media" for USB

```powershell
# USB physical disk details
Get-PhysicalDisk | Where-Object {$_.BusType -eq 'USB'} | 
    Select-Object FriendlyName, SerialNumber, MediaType, Size, HealthStatus

# Correlate with Get-Disk via SerialNumber
$disk = Get-Disk -Number 1
$physicalDisk = Get-PhysicalDisk | Where-Object {$_.SerialNumber -eq $disk.SerialNumber}
```

## Get-Volume and Get-Partition provide filesystem context

**Get-Volume** returns `MSFT_Volume` objects for mounted filesystems:

| Property | Type | Description |
|----------|------|-------------|
| `DriveLetter` | Char16 | Assigned drive letter (NULL if unmounted) |
| `FileSystemLabel` | String | **Volume label** |
| `FileSystem` | String | **"NTFS", "FAT32", "exFAT", "ReFS"** |
| `Size` | UInt64 | Total volume size in bytes |
| `SizeRemaining` | UInt64 | Free space in bytes |
| `DriveType` | UInt32 | **2=Removable, 3=Fixed, 4=Remote, 5=CD-ROM** |
| `HealthStatus` | UInt16 | 0=Healthy, 1=Scan Needed, 2=Spot Fix Needed |
| `AllocationUnitSize` | UInt32 | Cluster size in bytes |

**Get-Partition** returns `MSFT_Partition` objects linking disks to volumes:

| Property | Type | Description |
|----------|------|-------------|
| `DiskNumber` | UInt32 | **Parent disk number (correlates with Get-Disk)** |
| `PartitionNumber` | UInt32 | Partition index (1-based) |
| `DriveLetter` | Char16 | Assigned letter |
| `Size` | UInt64 | Partition size in bytes |
| `Offset` | UInt64 | Starting offset from disk beginning |
| `Type` | String | "Basic", "System", "Recovery", "IFS" |
| `GptType` | String | GPT GUID (e.g., `{ebd0a0a2-b9e5-4433-87c0-68b6b72699c7}` for Basic Data) |
| `MbrType` | UInt16 | MBR type (7=IFS/NTFS, 12=FAT32) |
| `IsSystem`, `IsBoot`, `IsActive`, `IsHidden` | Boolean | Partition flags |

```powershell
# Chain from volume to parent USB disk
Get-Volume -DriveLetter E | Get-Partition | Get-Disk | 
    Where-Object {$_.BusType -eq 'USB'}

# Complete USB device enumeration
Get-Disk | Where-Object {$_.BusType -eq 'USB'} | ForEach-Object {
    $disk = $_
    Get-Partition -DiskNumber $disk.Number | ForEach-Object {
        $vol = Get-Volume -Partition $_
        [PSCustomObject]@{
            DriveLetter = $_.DriveLetter
            VolumeLabel = $vol.FileSystemLabel
            FileSystem = $vol.FileSystem
            SizeGB = [math]::Round($disk.Size/1GB, 2)
            PartitionStyle = $disk.PartitionStyle
        }
    }
}
```

## Disk management cmdlets enable complete USB preparation

**Clear-Disk** wipes partition tables and data:
```powershell
Clear-Disk -Number 1 -RemoveData -RemoveOEM -Confirm:$false
```
Key parameters: `-RemoveData` (required for data volumes), `-RemoveOEM` (removes recovery partitions)

**Initialize-Disk** sets partition style on RAW disks:
```powershell
Initialize-Disk -Number 1 -PartitionStyle GPT  # Default is GPT
Initialize-Disk -Number 1 -PartitionStyle MBR  # Legacy BIOS compatibility
```

**New-Partition** creates partitions with drive letter assignment:
```powershell
# Full disk with auto drive letter
New-Partition -DiskNumber 1 -UseMaximumSize -AssignDriveLetter

# Specific size and letter
New-Partition -DiskNumber 1 -Size 8GB -DriveLetter T

# GPT EFI partition
New-Partition -DiskNumber 1 -Size 500MB -GptType "{c12a7328-f81f-11d2-ba4b-00a0c93ec93b}"

# MBR active boot partition  
New-Partition -DiskNumber 1 -UseMaximumSize -MbrType IFS -IsActive
```

**Format-Volume** applies filesystem formatting:
```powershell
Format-Volume -DriveLetter D -FileSystem exFAT -NewFileSystemLabel "USB_DRIVE"
Format-Volume -DriveLetter D -FileSystem NTFS -AllocationUnitSize 4096 -Full
Format-Volume -DriveLetter D -FileSystem FAT32 -Force  # Max 32GB in Windows
```
File system selection: **exFAT** for cross-platform large files, **NTFS** for Windows-only with permissions, **FAT32** for maximum device compatibility.

**Set-Partition** modifies existing partition attributes:
```powershell
Set-Partition -DriveLetter Y -NewDriveLetter Z
Set-Partition -DriveLetter Y -IsReadOnly $True
Set-Partition -DriveLetter Y -IsActive $True  # MBR bootable
```

## Complete USB preparation workflow in a single pipeline

```powershell
# Wipe, initialize GPT, create partition, format exFAT
Get-Disk -Number 1 | 
    Clear-Disk -RemoveData -RemoveOEM -Confirm:$false -PassThru | 
    Initialize-Disk -PartitionStyle GPT -PassThru | 
    New-Partition -AssignDriveLetter -UseMaximumSize | 
    Format-Volume -FileSystem exFAT -NewFileSystemLabel "USB_DRIVE"
```

## CIM supersedes WMI for storage queries

**Get-CimInstance** is the required replacement for the deprecated `Get-WmiObject` (removed in PowerShell 7). CIM uses WS-Management protocol, offering better firewall compatibility and cross-platform support.

**CIM class reference for USB identification:**

| Class | Namespace | Key USB Properties |
|-------|-----------|-------------------|
| `Win32_DiskDrive` | Root\CIMV2 | `InterfaceType='USB'`, `PNPDeviceID` (contains VID/PID), `SerialNumber`, `Model` |
| `Win32_DiskPartition` | Root\CIMV2 | `DiskIndex`, `Size`, `Bootable`, `Type` |
| `Win32_LogicalDisk` | Root\CIMV2 | `DeviceID`, `DriveType=2` (Removable), `FileSystem`, `VolumeName` |
| `MSFT_PhysicalDisk` | Root\Microsoft\Windows\Storage | `BusType=7`, `SerialNumber`, `MediaType` |
| `MSFT_Disk` | Root\Microsoft\Windows\Storage | `BusType=7`, `PartitionStyle`, `HealthStatus` |

**Association classes** link physical disks to logical volumes: `Win32_DiskDriveToDiskPartition` and `Win32_LogicalDiskToPartition`.

```powershell
# CIM query for USB with VID/PID extraction
Get-CimInstance Win32_DiskDrive -Filter "InterfaceType='USB'" | ForEach-Object {
    $disk = $_
    $partitions = Get-CimAssociatedInstance -InputObject $disk -ResultClassName Win32_DiskPartition
    foreach($partition in $partitions) {
        $logicalDisks = Get-CimAssociatedInstance -InputObject $partition -ResultClassName Win32_LogicalDisk
        foreach($ld in $logicalDisks) {
            [PSCustomObject]@{
                DriveLetter = $ld.DeviceID
                Model = $disk.Model
                SerialNumber = $disk.SerialNumber
                PNPDeviceID = $disk.PNPDeviceID  # Contains USB\VID_xxxx&PID_xxxx
                FileSystem = $ld.FileSystem
                VolumeLabel = $ld.VolumeName
            }
        }
    }
}
```

## Windows Server 2025 and PowerShell 7 maintain existing cmdlet compatibility

No new storage cmdlets specific to USB management were added in PowerShell 7.4/7.5 or Windows Server 2025. The existing Storage module cmdlets remain the standard approach. Notable platform updates include:

- **Convert-PhysicalDisk** — New cmdlet for converting disks to Storage Spaces (not USB-relevant)
- **Thin provisioning** — Storage Spaces now supports dynamic allocation
- **90% IOPS improvement** — NVMe SSD performance gains
- **ReFS deduplication** — Extended to VM workloads

USB flash drives typically report `CanPool = False` with `CannotPoolReason = "Removable Media"`, making them ineligible for Storage Spaces pools by design.

## Property quick reference for USB management tools

| Information Needed | Cmdlet | Property Name |
|-------------------|--------|---------------|
| Serial number | Get-Disk / Get-PhysicalDisk | `SerialNumber` |
| Model/friendly name | Get-Disk | `FriendlyName`, `Model` |
| Total size | Get-Disk / Get-PhysicalDisk | `Size` (bytes) |
| USB identification | Get-Disk / Get-PhysicalDisk | `BusType -eq 'USB'` or `BusType -eq 7` |
| Health status | Get-Disk / Get-PhysicalDisk | `HealthStatus` |
| Partition style | Get-Disk | `PartitionStyle` (MBR/GPT/RAW) |
| Volume label | Get-Volume | `FileSystemLabel` |
| Filesystem type | Get-Volume | `FileSystem` |
| Drive letter | Get-Volume / Get-Partition | `DriveLetter` |
| USB VID/PID | Get-CimInstance Win32_DiskDrive | `PNPDeviceID` |

## Conclusion

The PowerShell Storage module provides comprehensive USB device management through the **Get-Disk**, **Get-PhysicalDisk**, **Get-Volume**, and **Get-Partition** cmdlets for enumeration, combined with **Clear-Disk**, **Initialize-Disk**, **New-Partition**, **Format-Volume**, and **Set-Partition** for manipulation. The critical filter for isolating USB devices is `BusType -eq 'USB'` (numeric value 7). For VID/PID extraction, supplement Storage cmdlets with `Get-CimInstance Win32_DiskDrive` and parse the `PNPDeviceID` property. All cmdlets remain current in PowerShell 7 and Windows Server 2025 with no deprecation concerns.