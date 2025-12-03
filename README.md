# wusbkit

A command-line toolkit for USB device management on Windows.

## Features

- List all connected USB storage devices
- View detailed information about specific USB drives
- Format USB drives with various filesystems (FAT32, NTFS, exFAT)
- Flash disk images to USB drives (local files or remote URLs)
- Safely eject USB drives
- JSON output mode for programmatic use and external application integration
- **Native WMI enumeration** for ultra-fast device listing (no PowerShell overhead)
- PowerShell 7 backend for format/flash operations
- Streaming decompression support (gzip, xz, zstd)
- Real-time progress reporting with speed metrics
- Disk locking to prevent concurrent operations
- Signal handling for graceful cancellation (Ctrl+C)

## Performance

Device enumeration uses native Windows WMI queries via COM, bypassing PowerShell entirely for the `list` and `info` commands. This provides:

- **10-40x faster** device listing compared to PowerShell-based enumeration
- Sub-200ms response time even with many USB devices connected
- Automatic fallback to PowerShell if WMI fails

## Requirements

- Windows 10/11
- [PowerShell 7](https://github.com/PowerShell/PowerShell/releases) (pwsh.exe) - required for format/flash/eject operations
- Administrator privileges (for format and flash operations)

> **Note:** The `list` and `info` commands use native WMI and do not require PowerShell 7.

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

### Flash USB Drive

Write a disk image directly to a USB drive (raw write).

> **Warning:** This will completely overwrite the target drive!

```bash
# Flash from local image file
wusbkit flash E: --image ubuntu.img
wusbkit flash 2 --image debian.iso

# Flash from compressed image (streaming decompression)
wusbkit flash E: --image ubuntu.img.gz
wusbkit flash E: --image debian.iso.xz
wusbkit flash E: --image arch.img.zst

# Flash from ZIP archive (extracts first image file)
wusbkit flash E: --image recovery.zip

# Flash directly from URL (streams without downloading)
wusbkit flash E: --image https://example.com/image.img

# Verify after writing
wusbkit flash E: --image ubuntu.img --verify

# Calculate SHA-256 hash during write
wusbkit flash E: --image ubuntu.img --hash

# Skip unchanged sectors (faster for partial updates)
wusbkit flash E: --image ubuntu.img --skip-unchanged

# Custom buffer size (default: 4M, range: 1M-64M)
wusbkit flash E: --image ubuntu.img --buffer 8M
wusbkit flash E: --image ubuntu.img -b 16MB

# Skip confirmation prompt
wusbkit flash E: --image ubuntu.img --yes

# JSON output for progress
wusbkit flash 2 --image debian.iso --json --yes
```

**Supported image sources:**
| Source | Formats | Notes |
|--------|---------|-------|
| Local files | .img, .iso, .bin, .raw | Direct raw write |
| Compressed | .gz, .xz, .zst, .zstd | Streaming decompression |
| Archives | .zip | Extracts first image file |
| Remote URLs | HTTP/HTTPS | Streams directly to drive |

### Eject USB Drive

Safely eject a USB storage device (same as "Safely Remove Hardware").

```bash
# By drive letter
wusbkit eject E:
wusbkit eject E

# By disk number
wusbkit eject 2

# Skip confirmation
wusbkit eject E: --yes
wusbkit eject E: -y

# JSON output
wusbkit eject E: --json
```

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

## JSON Output and Integration

All commands support `--json` flag for programmatic use. This section provides detailed information for integrating wusbkit with external applications (Electron, Node.js, Python, etc.).

### Output Protocol

- **stdout**: JSON data (success responses and progress updates)
- **stderr**: JSON error objects
- **Exit code 0**: Success
- **Exit code 1**: Error (details in stderr)

### JSON Schemas

#### Device Object

Returned by `list` (as array) and `info` (as single object):

```json
{
  "driveLetter": "E:",
  "diskNumber": 2,
  "friendlyName": "SanDisk Cruzer Glide",
  "model": "Cruzer Glide",
  "size": 30850000000,
  "sizeHuman": "28.7 GB",
  "serialNumber": "04016209041025010710",
  "vendorId": "0781",
  "productId": "5567",
  "fileSystem": "FAT32",
  "volumeLabel": "MYUSB",
  "partitionStyle": "MBR",
  "status": "Online",
  "healthStatus": "Healthy",
  "busType": "USB",
  "mediaType": ""
}
```

#### Version Object

Returned by `version --json`:

```json
{
  "version": "1.2.6",
  "buildDate": "2024-01-15",
  "goVersion": "go1.21.5",
  "platform": "windows/amd64",
  "pwshVersion": "7.4.1"
}
```

#### Eject Result

Returned by `eject --json`:

```json
{
  "success": true,
  "driveLetter": "E:",
  "diskNumber": 2,
  "message": "USB drive E: ejected successfully"
}
```

#### Error Object

Written to stderr on errors:

```json
{
  "error": "USB drive E: not found",
  "code": "USB_NOT_FOUND"
}
```

### Progress Streaming

Long-running operations (`format` and `flash`) emit progress as **newline-delimited JSON** (NDJSON). Each line is a complete JSON object.

#### Format Progress

```json
{"drive":"E:","diskNumber":2,"stage":"Cleaning","percentage":10,"status":"in_progress"}
{"drive":"E:","diskNumber":2,"stage":"Creating partition","percentage":30,"status":"in_progress"}
{"drive":"E:","diskNumber":2,"stage":"Formatting","percentage":50,"status":"in_progress"}
{"drive":"E:","diskNumber":2,"stage":"Assigning drive letter","percentage":80,"status":"in_progress"}
{"drive":"E:","diskNumber":2,"stage":"Complete","percentage":100,"status":"complete"}
```

#### Flash Progress

```json
{"stage":"Writing","percentage":15,"bytes_written":524288000,"total_bytes":3500000000,"speed":"45.2 MB/s","status":"in_progress"}
{"stage":"Writing","percentage":30,"bytes_written":1048576000,"total_bytes":3500000000,"speed":"48.1 MB/s","status":"in_progress"}
{"stage":"Verifying","percentage":50,"bytes_written":1750000000,"total_bytes":3500000000,"speed":"52.3 MB/s","status":"in_progress"}
{"stage":"Complete","percentage":100,"bytes_written":3500000000,"total_bytes":3500000000,"status":"complete","hash":"a1b2c3d4..."}
```

**Progress fields:**
| Field | Type | Description |
|-------|------|-------------|
| `stage` | string | Current operation stage |
| `percentage` | int | Progress 0-100 |
| `bytes_written` | int64 | Bytes written so far (flash only) |
| `total_bytes` | int64 | Total bytes to write (flash only) |
| `speed` | string | Write speed formatted (flash only) |
| `status` | string | `in_progress`, `complete`, or `error` |
| `error` | string | Error message (only when status is `error`) |
| `hash` | string | SHA-256 hash (flash only, when `--hash` flag used) |
| `bytes_skipped` | int64 | Bytes skipped (flash only, when `--skip-unchanged` used) |

### Error Codes

| Code | Description |
|------|-------------|
| `USB_NOT_FOUND` | Specified USB device not found |
| `PWSH_NOT_FOUND` | PowerShell 7 not installed or not in PATH |
| `FORMAT_FAILED` | Format operation failed |
| `FLASH_FAILED` | Flash operation failed |
| `PERMISSION_DENIED` | Administrator privileges required |
| `INVALID_INPUT` | Invalid command arguments |
| `DISK_BUSY` | Another operation is in progress on this disk |
| `INTERNAL_ERROR` | Unexpected internal error |

### Integration Examples

#### Node.js / Electron

```javascript
const { spawn } = require('child_process');

// List devices
function listDevices() {
  return new Promise((resolve, reject) => {
    const proc = spawn('wusbkit.exe', ['list', '--json']);
    let stdout = '';
    let stderr = '';

    proc.stdout.on('data', (data) => stdout += data);
    proc.stderr.on('data', (data) => stderr += data);

    proc.on('close', (code) => {
      if (code === 0) {
        resolve(JSON.parse(stdout));
      } else {
        reject(JSON.parse(stderr));
      }
    });
  });
}

// Flash with progress
function flashDrive(drive, imagePath, onProgress) {
  const proc = spawn('wusbkit.exe', [
    'flash', drive,
    '--image', imagePath,
    '--json', '--yes'
  ]);

  proc.stdout.on('data', (data) => {
    const lines = data.toString().trim().split('\n');
    lines.forEach(line => {
      try {
        const progress = JSON.parse(line);
        onProgress(progress);
      } catch (e) {
        // Handle partial JSON chunks if needed
      }
    });
  });

  proc.stderr.on('data', (data) => {
    const error = JSON.parse(data.toString());
    onProgress({ status: 'error', error: error.error, code: error.code });
  });

  return proc;
}

// Usage
flashDrive('E:', 'ubuntu.iso', (progress) => {
  console.log(`${progress.stage}: ${progress.percentage}% - ${progress.speed}`);
});
```

#### Python

```python
import subprocess
import json

def list_devices():
    result = subprocess.run(
        ['wusbkit.exe', 'list', '--json'],
        capture_output=True, text=True
    )
    if result.returncode == 0:
        return json.loads(result.stdout)
    else:
        raise Exception(json.loads(result.stderr))

def flash_with_progress(drive, image_path):
    proc = subprocess.Popen(
        ['wusbkit.exe', 'flash', drive, '--image', image_path, '--json', '--yes'],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
    )

    for line in proc.stdout:
        progress = json.loads(line.strip())
        yield progress
```

### Integration Tips

1. **Always use `--json --yes` together** for non-interactive operation
2. **Parse stdout line-by-line** for progress streaming
3. **Check exit code AND stderr** for error detection
4. **Handle `PERMISSION_DENIED`** by re-spawning with elevation
5. **Check PowerShell first** with `wusbkit version --json` to verify `pwshVersion` is present
6. **Graceful cancellation**: Send SIGINT (Ctrl+C) to cancel flash operations cleanly

### Privilege Elevation

Format and flash operations require administrator privileges. When running from a non-elevated process:

**PowerShell elevation:**
```powershell
Start-Process -FilePath "wusbkit.exe" -ArgumentList "flash","E:","-i","image.iso","--json","--yes" -Verb RunAs -Wait
```

**Node.js with sudo-prompt:**
```javascript
const sudo = require('sudo-prompt');
sudo.exec('wusbkit.exe flash E: --image image.iso --json --yes', options, callback);
```

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
│   ├── flash.go            # flash command
│   ├── eject.go            # eject command
│   └── version.go          # version command
├── internal/
│   ├── powershell/
│   │   └── executor.go     # PowerShell 7 execution wrapper
│   ├── usb/
│   │   ├── device.go           # USB device data models
│   │   ├── enumerate.go        # USB enumeration logic (PowerShell fallback)
│   │   └── enumerate_native.go # Native WMI enumeration (fast path)
│   ├── format/
│   │   └── format.go       # Format orchestration
│   ├── flash/
│   │   ├── flash.go        # Flash orchestration
│   │   ├── source.go       # Image sources (file, zip, URL, compressed)
│   │   └── writer.go       # Raw disk writer with Windows API
│   ├── lock/
│   │   └── disklock.go     # Disk locking for concurrency control
│   └── output/
│       ├── json.go         # JSON output helpers and error codes
│       └── table.go        # pterm table formatters
└── dist/                   # Build output (gitignored)
```

## Dependencies

### CLI Framework
- [spf13/cobra](https://github.com/spf13/cobra) - Command-line interface framework

### Terminal UI
- [pterm/pterm](https://github.com/pterm/pterm) - Terminal output formatting, tables, spinners

### Compression
- [klauspost/compress](https://github.com/klauspost/compress) - Zstandard (zstd) decompression
- [ulikunitz/xz](https://github.com/ulikunitz/xz) - XZ/LZMA decompression
- Standard library `compress/gzip` - Gzip decompression
- Standard library `archive/zip` - ZIP archive extraction

### System
- [StackExchange/wmi](https://github.com/StackExchange/wmi) - Native WMI queries for fast device enumeration
- [gofrs/flock](https://github.com/gofrs/flock) - File-based locking for disk operations
- [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) - Windows system calls for raw disk I/O

## License

Apache 2.0
