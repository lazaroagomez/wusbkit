# wusbkit v2 - Migration to C#/.NET

## Overview

**wusbkit** is a CLI tool for USB device management on Windows. The current version (v1) is written in Go and works well, but for v2 we're migrating to C#/.NET to take advantage of native Windows integration.

## Why Migrate to C#/.NET?

### Current Limitations (Go v1)

| Feature | Go Implementation | Issue |
|---------|-------------------|-------|
| Device listing | WMI via `StackExchange/wmi` | Works, but limited to queries only |
| Format | PowerShell subprocess | Sequential only, ~5s per device |
| WMI Methods | Not easily accessible | Requires complex COM interop via `go-ole` |

### Benefits of C#/.NET

| Feature | C# Implementation | Benefit |
|---------|-------------------|---------|
| Device listing | `System.Management` | Native WMI, no external packages |
| Format | WMI `InvokeMethod()` | **Parallel formatting** with `Task.WhenAll()` |
| WMI Methods | Built-in support | Direct access to `MSFT_Disk`, `MSFT_Volume` |
| Windows APIs | First-class support | No CGO, no subprocess overhead |

### Performance Comparison

| Operation | Go v1 | C# v2 (expected) |
|-----------|-------|------------------|
| List 9 USB devices | ~100ms | ~100ms |
| Format 1 device | ~5s | ~5s |
| **Format 9 devices** | **~45s** (sequential) | **~5-8s** (parallel) |

---

## Target Specifications

### Platform
- **Windows 10/11 only**
- No cross-platform support needed
- No PowerShell 7 dependency

### Output
- JSON for programmatic use (`--json` flag)
- NDJSON streaming for long operations (format, flash progress)
- Human-readable tables for interactive use

### Supported Image Formats
| Format | Support | Notes |
|--------|---------|-------|
| `.img`, `.iso`, `.bin` | ✅ Required | Direct file read |
| `.zip` | ✅ Required | Built-in `ZipArchive` |
| `.gz` | ✅ Optional | Built-in `GZipStream` |
| `.xz`, `.zst` | ❌ No | User must decompress manually |
| HTTP/HTTPS | ❌ No | Out of scope for v2 |

---

## Commands to Implement

### 1. `list`
List all connected USB storage devices.

**Requirements:**
- Query `Win32_DiskDrive` where `InterfaceType='USB'`
- Include: disk number, model, serial, size, drive letter, filesystem
- Extract VID/PID from `PNPDeviceID`

**Technology:** `System.Management.ManagementObjectSearcher`

### 2. `info <drive>`
Show detailed information for a specific USB device.

**Requirements:**
- Accept drive letter (E:) or disk number (2)
- Same data as list, but single device

### 3. `format <drive>`
Format one or more USB drives.

**Requirements:**
- Support FAT32, NTFS, exFAT
- Quick format by default
- **Parallel execution** when multiple drives specified
- Progress reporting via NDJSON

**Technology:**
- `MSFT_Disk.Clear()` - Clean disk
- `MSFT_Disk.CreatePartition()` - Create partition
- `MSFT_Volume.Format()` - Format volume
- `Task.WhenAll()` - Parallel execution

### 4. `flash <drive> --image <path>`
Write disk image to USB drive.

**Requirements:**
- Raw disk write to `\\.\PhysicalDriveN`
- Support `.img`, `.iso`, `.zip`, `.gz`
- Progress reporting (percentage, speed, ETA)
- Optional verification (`--verify`)
- Optional hash calculation (`--hash`)

**Technology:**
- `FileStream` with `FileOptions.WriteThrough | FileOptions.NoBuffering`
- 4KB alignment for unbuffered I/O
- `SHA256` for hash calculation

### 5. `eject <drive>`
Safely eject USB device.

**Requirements:**
- Flush buffers
- Unmount volumes
- Eject device

**Technology:** `CM_Request_Device_Eject` via P/Invoke or WMI

---

## Architecture

```
wusbkit/
├── Program.cs              # Entry point, CLI setup
├── Commands/
│   ├── ListCommand.cs
│   ├── InfoCommand.cs
│   ├── FormatCommand.cs
│   ├── FlashCommand.cs
│   └── EjectCommand.cs
├── Services/
│   ├── DeviceService.cs    # WMI queries and methods
│   ├── FlashService.cs     # Raw disk I/O
│   └── ImageSource.cs      # File/ZIP/GZ handling
├── Models/
│   ├── UsbDevice.cs
│   └── ProgressInfo.cs
└── wusbkit.csproj
```

---

## Key Implementation Notes

### WMI Namespaces
- `root\cimv2` - Basic disk info (`Win32_DiskDrive`, `Win32_LogicalDisk`)
- `root\Microsoft\Windows\Storage` - Storage management (`MSFT_Disk`, `MSFT_Volume`)

### Parallel Format Strategy
```
1. User requests: format E: F: G: --fs FAT32
2. Create Task for each drive
3. Execute all Tasks with Task.WhenAll()
4. Stream progress for each drive independently
5. Return consolidated results
```

### Raw Disk Access
- Open `\\.\PhysicalDriveN` with admin privileges
- Use `FileOptions.WriteThrough | FileOptions.NoBuffering`
- Buffer must be aligned to 4KB (sector size)
- Lock volumes before writing with `FSCTL_LOCK_VOLUME`

### JSON Output Format
Maintain compatibility with v1 JSON schema:
- `list --json` → Array of device objects
- `format --json` → NDJSON progress lines
- `flash --json` → NDJSON progress lines
- Errors to stderr as JSON object with `error` and `code` fields

---

## Dependencies

### Required NuGet Packages
```xml
<PackageReference Include="System.CommandLine" Version="2.0.0-beta4.*" />
```

### Built-in .NET Libraries (no packages needed)
- `System.Management` - WMI
- `System.IO.Compression` - ZIP, GZip
- `System.Text.Json` - JSON serialization
- `System.Security.Cryptography` - SHA256

---

## Build & Distribution

### Single Executable
```bash
dotnet publish -c Release -r win-x64 \
  --self-contained true \
  -p:PublishSingleFile=true \
  -p:EnableCompressionInSingleFile=true
```

### Expected Binary Size
- Self-contained: ~50-60 MB
- Framework-dependent: ~500 KB (requires .NET runtime on target)

---

## Migration Checklist

- [ ] Set up .NET 8+ project with `System.CommandLine`
- [ ] Implement `DeviceService` with WMI queries
- [ ] Implement `list` command
- [ ] Implement `info` command
- [ ] Implement `format` command with parallel support
- [ ] Implement `FlashService` with raw disk I/O
- [ ] Implement `flash` command with progress
- [ ] Implement `eject` command
- [ ] Add ZIP and GZ support to `ImageSource`
- [ ] Test JSON output compatibility with v1
- [ ] Test with multiple USB devices
- [ ] Package as single executable

---

## References

- [System.Management Namespace](https://docs.microsoft.com/en-us/dotnet/api/system.management)
- [MSFT_Disk Class](https://docs.microsoft.com/en-us/windows-hardware/drivers/storage/msft-disk)
- [MSFT_Volume Class](https://docs.microsoft.com/en-us/windows-hardware/drivers/storage/msft-volume)
- [System.CommandLine](https://github.com/dotnet/command-line-api)
- [Raw Disk Access in .NET](https://docs.microsoft.com/en-us/windows/win32/fileio/naming-a-file#win32-device-namespaces)
