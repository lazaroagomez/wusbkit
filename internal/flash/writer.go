package flash

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// Buffer size for read/write operations (1 MB, matching Chrome)
	bufferSize = 1 << 20

	// Alignment required for unbuffered I/O (4 KB)
	alignment = 4096

	// Windows IOCTL codes
	FSCTL_LOCK_VOLUME    = 0x00090018
	FSCTL_DISMOUNT_VOLUME = 0x00090020
	FSCTL_ALLOW_EXTENDED_DASD_IO = 0x00090083
)

// diskWriter handles raw disk write operations on Windows
type diskWriter struct {
	diskNumber int
	handle     windows.Handle
	volumes    []windows.Handle
}

// newDiskWriter creates a writer for raw disk access
func newDiskWriter(diskNumber int) *diskWriter {
	return &diskWriter{
		diskNumber: diskNumber,
		handle:     windows.InvalidHandle,
		volumes:    make([]windows.Handle, 0),
	}
}

// Open prepares the disk for writing by locking and dismounting volumes
func (w *diskWriter) Open() error {
	// First, lock and dismount all volumes on this disk
	if err := w.lockVolumes(); err != nil {
		return fmt.Errorf("failed to lock volumes: %w", err)
	}

	// Open the physical disk for writing
	path := fmt.Sprintf(`\\.\PhysicalDrive%d`, w.diskNumber)
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("invalid disk path: %w", err)
	}

	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_NO_BUFFERING|windows.FILE_FLAG_WRITE_THROUGH,
		0,
	)
	if err != nil {
		w.Close()
		return fmt.Errorf("failed to open disk: %w", err)
	}

	w.handle = handle

	// Enable extended DASD I/O for large disks
	var bytesReturned uint32
	_ = windows.DeviceIoControl(
		w.handle,
		FSCTL_ALLOW_EXTENDED_DASD_IO,
		nil, 0, nil, 0,
		&bytesReturned, nil,
	)

	return nil
}

// lockVolumes finds and locks all volumes on this physical disk
func (w *diskWriter) lockVolumes() error {
	// Get volume letters for this disk via PowerShell
	letters, err := w.getVolumeLetters()
	if err != nil {
		// Non-fatal: disk might not have volumes
		return nil
	}

	for _, letter := range letters {
		volumePath := fmt.Sprintf(`\\.\%s:`, letter)
		pathPtr, err := syscall.UTF16PtrFromString(volumePath)
		if err != nil {
			continue
		}

		handle, err := windows.CreateFile(
			pathPtr,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
			nil,
			windows.OPEN_EXISTING,
			0,
			0,
		)
		if err != nil {
			continue
		}

		// Lock the volume
		var bytesReturned uint32
		err = windows.DeviceIoControl(
			handle,
			FSCTL_LOCK_VOLUME,
			nil, 0, nil, 0,
			&bytesReturned, nil,
		)
		if err != nil {
			windows.CloseHandle(handle)
			continue
		}

		// Dismount the volume
		_ = windows.DeviceIoControl(
			handle,
			FSCTL_DISMOUNT_VOLUME,
			nil, 0, nil, 0,
			&bytesReturned, nil,
		)

		w.volumes = append(w.volumes, handle)
	}

	return nil
}

// getVolumeLetters returns drive letters for volumes on this disk
func (w *diskWriter) getVolumeLetters() ([]string, error) {
	cmd := exec.Command("pwsh.exe", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf(`(Get-Partition -DiskNumber %d -ErrorAction SilentlyContinue | Where-Object {$_.DriveLetter}).DriveLetter -join ','`, w.diskNumber))

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return nil, nil
	}

	return strings.Split(result, ","), nil
}

// WriteAt writes data at the specified offset (must be aligned to 4096)
func (w *diskWriter) WriteAt(data []byte, offset int64) (int, error) {
	if w.handle == windows.InvalidHandle {
		return 0, fmt.Errorf("disk not opened")
	}

	// Seek to position
	_, err := windows.Seek(w.handle, offset, 0)
	if err != nil {
		return 0, fmt.Errorf("seek failed: %w", err)
	}

	// Write data
	var written uint32
	err = windows.WriteFile(w.handle, data, &written, nil)
	if err != nil {
		return int(written), fmt.Errorf("write failed: %w", err)
	}

	return int(written), nil
}

// ReadAt reads data at the specified offset (for verification)
func (w *diskWriter) ReadAt(data []byte, offset int64) (int, error) {
	if w.handle == windows.InvalidHandle {
		return 0, fmt.Errorf("disk not opened")
	}

	// Seek to position
	_, err := windows.Seek(w.handle, offset, 0)
	if err != nil {
		return 0, fmt.Errorf("seek failed: %w", err)
	}

	// Read data
	var read uint32
	err = windows.ReadFile(w.handle, data, &read, nil)
	if err != nil {
		return int(read), fmt.Errorf("read failed: %w", err)
	}

	return int(read), nil
}

// Close releases all handles
func (w *diskWriter) Close() error {
	// Close volume handles (unlocks them)
	for _, h := range w.volumes {
		windows.CloseHandle(h)
	}
	w.volumes = nil

	// Close disk handle
	if w.handle != windows.InvalidHandle {
		windows.CloseHandle(w.handle)
		w.handle = windows.InvalidHandle
	}

	return nil
}

// alignedBuffer allocates a buffer aligned to 4096 bytes for unbuffered I/O
func alignedBuffer(size int) []byte {
	// Allocate extra space for alignment
	buf := make([]byte, size+alignment)

	// Calculate aligned start
	addr := uintptr(unsafe.Pointer(&buf[0]))
	offset := (alignment - int(addr%alignment)) % alignment

	return buf[offset : offset+size]
}

// alignSize rounds up size to the nearest alignment boundary
func alignSize(size int) int {
	return ((size + alignment - 1) / alignment) * alignment
}
