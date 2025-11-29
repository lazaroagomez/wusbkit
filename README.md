# wusbkit

A command-line toolkit for USB device management on Windows.

## Features

- List all connected USB storage devices
- View detailed information about specific USB drives
- Format USB drives with various filesystems (FAT32, NTFS, exFAT)
- JSON output mode for programmatic use
- PowerShell 7 backend for reliable device enumeration

## Requirements

- Windows 10/11
- [PowerShell 7](https://github.com/PowerShell/PowerShell/releases) (pwsh.exe)
- Administrator privileges (for format operations)

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/lazaroagomez/wusbkit.git
cd wusbkit

# Build
go build -o dist/wusbkit.exe .

# Or with version info
go build -ldflags "-X github.com/lazaroagomez/wusbkit/cmd.Version=1.0.0" -o dist/wusbkit.exe .
```

### Using Go Install

```bash
go install github.com/lazaroagomez/wusbkit@latest
```

## Commands

### List USB Drives

List all connected USB storage devices.

```bash
# Basic list
wusbkit list

# Verbose output (includes serial, VID/PID, filesystem)
wusbkit list --verbose
wusbkit list -v

# JSON output
wusbkit list --json
wusbkit list -j
```

**Example output:**
```
┌────────────┬──────────────────────┬────────┬─────────┐
│ Drive      │ Name                 │ Size   │ Status  │
├────────────┼──────────────────────┼────────┼─────────┤
│ E:         │ SanDisk Cruzer Glide │ 28.7 GB│ Healthy │
└────────────┴──────────────────────┴────────┴─────────┘
```

### Show Drive Information

Display detailed information about a specific USB drive.

```bash
# By drive letter
wusbkit info E:
wusbkit info E

# By disk number
wusbkit info 2

# JSON output
wusbkit info E: --json
```

**Example output:**
```
# SanDisk Cruzer Glide

Drive Letter    │ E:
Disk Number     │ 2
Model           │ Cruzer Glide
Size            │ 28.7 GB
Serial Number   │ 04016209041025010710
File System     │ FAT32
Volume Label    │ MYUSB
Partition Style │ MBR
Bus Type        │ USB
Health Status   │ Healthy
Status          │ Online
```

### Format USB Drive

Format a USB storage device with the specified filesystem.

> **Warning:** This will erase all data on the drive!

```bash
# Basic format (FAT32, quick format)
wusbkit format E:

# Format with specific filesystem
wusbkit format E: --fs ntfs
wusbkit format E: --fs exfat
wusbkit format E: --fs fat32

# Set volume label
wusbkit format E: --fs exfat --label "MY_USB"

# Full format (not quick)
wusbkit format E: --fs ntfs --quick=false

# Skip confirmation prompt
wusbkit format E: --fs fat32 --yes
wusbkit format E: --fs fat32 -y

# Format by disk number
wusbkit format 2 --fs ntfs --label DATA --yes
```

**Filesystem options:**
| Filesystem | Max File Size | Cross-Platform | Best For |
|------------|---------------|----------------|----------|
| FAT32      | 4 GB          | Excellent      | Maximum device compatibility |
| exFAT      | 16 EB         | Good           | Large files, cross-platform |
| NTFS       | 16 EB         | Windows only   | Windows-only, permissions |

### Show Version

Display version and build information.

```bash
wusbkit version

# JSON output
wusbkit version --json
```

## Global Flags

These flags work with all commands:

| Flag | Short | Description |
|------|-------|-------------|
| `--json` | `-j` | Output in JSON format |
| `--verbose` | `-v` | Show detailed/verbose output |
| `--no-color` | | Disable colored output |

## JSON Output

All commands support `--json` flag for programmatic use:

```bash
# List as JSON array
wusbkit list --json

# Device info as JSON object
wusbkit info E: --json

# Format progress as newline-delimited JSON
wusbkit format E: --json --yes
```

**Error format:**
```json
{"error": "USB drive E: not found", "code": "USB_NOT_FOUND"}
```

**Error codes:**
- `USB_NOT_FOUND` - Specified USB device not found
- `PWSH_NOT_FOUND` - PowerShell 7 not installed
- `FORMAT_FAILED` - Format operation failed
- `PERMISSION_DENIED` - Administrator privileges required
- `INVALID_INPUT` - Invalid command input

## Building

### Prerequisites

- Go 1.21 or later
- Windows 10/11

### Build

```bash
# Using build script
build.bat

# Or manually
go build -o dist/wusbkit.exe .
```

Releases are automatically created via GitHub Actions when pushing to main.

## Project Structure

```
wusbkit/
├── main.go                 # Entry point
├── go.mod                  # Go module definition
├── build.bat               # Build script
├── VERSION                 # Current version number
├── cmd/
│   ├── root.go             # Root command, global flags
│   ├── list.go             # list command
│   ├── info.go             # info command
│   ├── format.go           # format command
│   └── version.go          # version command
├── internal/
│   ├── powershell/
│   │   └── executor.go     # PowerShell 7 execution wrapper
│   ├── usb/
│   │   ├── device.go       # USB device data models
│   │   └── enumerate.go    # USB enumeration logic
│   ├── format/
│   │   ├── diskpart.go     # diskpart script generation
│   │   └── format.go       # Format orchestration
│   └── output/
│       ├── json.go         # JSON output helpers
│       └── table.go        # pterm table formatters
└── dist/                   # Build output (gitignored)
```

## Dependencies

- [spf13/cobra](https://github.com/spf13/cobra) - CLI framework
- [pterm/pterm](https://github.com/pterm/pterm) - Terminal output formatting

## License

Apache 2.0
