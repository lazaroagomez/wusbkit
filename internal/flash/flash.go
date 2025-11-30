package flash

import (
	"bytes"
	"context"
	"fmt"
	"time"
)

// Stage constants for flash progress
const (
	StageExtracting = "Extracting"
	StageWriting    = "Writing"
	StageVerifying  = "Verifying"
	StageComplete   = "Complete"
)

// defaultBufferSize is the fallback buffer size (4MB) when not specified
const defaultBufferSize = 4 << 20

// Status constants
const (
	StatusInProgress = "in_progress"
	StatusComplete   = "complete"
	StatusError      = "error"
)

// Progress represents the current state of a flash operation
type Progress struct {
	Stage        string `json:"stage"`
	Percentage   int    `json:"percentage"`
	BytesWritten int64  `json:"bytes_written"`
	TotalBytes   int64  `json:"total_bytes"`
	Speed        string `json:"speed"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

// Options configures the flash operation
type Options struct {
	DiskNumber int
	ImagePath  string
	Verify     bool
	BufferSize int // Buffer size in MB (default: 4)
}

// Flasher handles USB drive flashing operations
type Flasher struct {
	progressChan chan Progress
}

// NewFlasher creates a new flasher
func NewFlasher() *Flasher {
	return &Flasher{
		progressChan: make(chan Progress, 10),
	}
}

// Progress returns a channel that receives progress updates
func (f *Flasher) Progress() <-chan Progress {
	return f.progressChan
}

// Flash writes an image to a USB drive
func (f *Flasher) Flash(ctx context.Context, opts Options) error {
	defer close(f.progressChan)

	// Open the image source
	source, err := OpenSource(opts.ImagePath)
	if err != nil {
		f.sendError(opts, err.Error())
		return err
	}
	defer source.Close()

	totalSize := source.Size()
	f.sendProgress(opts, StageWriting, 0, 0, totalSize, "")

	// Open the disk for writing
	writer := newDiskWriter(opts.DiskNumber)
	if err := writer.Open(); err != nil {
		f.sendError(opts, err.Error())
		return err
	}
	defer writer.Close()

	// Write the image
	if err := f.writeImage(ctx, opts, source, writer, totalSize); err != nil {
		return err
	}

	// Verify if requested
	if opts.Verify {
		if err := f.verifyImage(ctx, opts, writer, totalSize); err != nil {
			return err
		}
	}

	f.sendComplete(opts, totalSize)
	return nil
}

// writeImage writes the source to the disk with progress updates
func (f *Flasher) writeImage(ctx context.Context, opts Options, source Source, writer *diskWriter, totalSize int64) error {
	// Calculate buffer size in bytes (with fallback to 4MB)
	bufSize := opts.BufferSize << 20
	if bufSize <= 0 {
		bufSize = defaultBufferSize
	}
	buffer := alignedBuffer(bufSize)
	var bytesWritten int64
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			f.sendError(opts, "operation cancelled")
			return ctx.Err()
		default:
		}

		// Read from source
		n, err := source.Read(buffer)
		if n == 0 && err != nil {
			if err.Error() == "EOF" {
				break
			}
			f.sendError(opts, fmt.Sprintf("read error: %v", err))
			return err
		}

		if n == 0 {
			break
		}

		// Align write size for unbuffered I/O
		writeSize := alignSize(n)
		writeBuffer := buffer[:writeSize]

		// Zero-pad if needed
		if writeSize > n {
			for i := n; i < writeSize; i++ {
				writeBuffer[i] = 0
			}
		}

		// Write to disk
		written, err := writer.WriteAt(writeBuffer, bytesWritten)
		if err != nil {
			f.sendError(opts, fmt.Sprintf("write error at offset %d: %v", bytesWritten, err))
			return err
		}

		bytesWritten += int64(n)

		// Calculate speed and send progress
		elapsed := time.Since(startTime).Seconds()
		speed := ""
		if elapsed > 0 {
			bytesPerSec := float64(bytesWritten) / elapsed
			speed = formatSpeed(bytesPerSec)
		}

		percentage := int(float64(bytesWritten) / float64(totalSize) * 100)
		f.sendProgress(opts, StageWriting, percentage, bytesWritten, totalSize, speed)

		// Check for actual write vs requested
		if written < writeSize {
			break
		}
	}

	return nil
}

// verifyImage reads back the written data and compares with source
func (f *Flasher) verifyImage(ctx context.Context, opts Options, writer *diskWriter, totalSize int64) error {
	// Reopen the source for verification
	source, err := OpenSource(opts.ImagePath)
	if err != nil {
		f.sendError(opts, fmt.Sprintf("verify: failed to reopen source: %v", err))
		return err
	}
	defer source.Close()

	// Calculate buffer size in bytes (with fallback to 4MB)
	bufSize := opts.BufferSize << 20
	if bufSize <= 0 {
		bufSize = defaultBufferSize
	}
	sourceBuffer := alignedBuffer(bufSize)
	diskBuffer := alignedBuffer(bufSize)
	var bytesVerified int64
	startTime := time.Now()

	f.sendProgress(opts, StageVerifying, 0, 0, totalSize, "")

	for {
		select {
		case <-ctx.Done():
			f.sendError(opts, "verification cancelled")
			return ctx.Err()
		default:
		}

		// Read from source
		n, err := source.Read(sourceBuffer)
		if n == 0 && err != nil {
			if err.Error() == "EOF" {
				break
			}
			f.sendError(opts, fmt.Sprintf("verify: read source error: %v", err))
			return err
		}

		if n == 0 {
			break
		}

		// Read from disk (aligned)
		readSize := alignSize(n)
		_, err = writer.ReadAt(diskBuffer[:readSize], bytesVerified)
		if err != nil {
			f.sendError(opts, fmt.Sprintf("verify: read disk error at offset %d: %v", bytesVerified, err))
			return err
		}

		// Compare only the actual data bytes (not padding)
		if !bytes.Equal(sourceBuffer[:n], diskBuffer[:n]) {
			f.sendError(opts, fmt.Sprintf("verify: data mismatch at offset %d", bytesVerified))
			return fmt.Errorf("verification failed: data mismatch at offset %d", bytesVerified)
		}

		bytesVerified += int64(n)

		// Calculate speed and send progress
		elapsed := time.Since(startTime).Seconds()
		speed := ""
		if elapsed > 0 {
			bytesPerSec := float64(bytesVerified) / elapsed
			speed = formatSpeed(bytesPerSec)
		}

		percentage := int(float64(bytesVerified) / float64(totalSize) * 100)
		f.sendProgress(opts, StageVerifying, percentage, bytesVerified, totalSize, speed)
	}

	return nil
}

func (f *Flasher) sendProgress(opts Options, stage string, percentage int, bytesWritten, totalBytes int64, speed string) {
	select {
	case f.progressChan <- Progress{
		Stage:        stage,
		Percentage:   percentage,
		BytesWritten: bytesWritten,
		TotalBytes:   totalBytes,
		Speed:        speed,
		Status:       StatusInProgress,
	}:
	default:
	}
}

func (f *Flasher) sendError(opts Options, errMsg string) {
	select {
	case f.progressChan <- Progress{
		Stage:  "Error",
		Status: StatusError,
		Error:  errMsg,
	}:
	default:
	}
}

func (f *Flasher) sendComplete(opts Options, totalBytes int64) {
	select {
	case f.progressChan <- Progress{
		Stage:        StageComplete,
		Percentage:   100,
		BytesWritten: totalBytes,
		TotalBytes:   totalBytes,
		Status:       StatusComplete,
	}:
	default:
	}
}

// formatSpeed formats bytes per second into human readable string
func formatSpeed(bytesPerSec float64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytesPerSec >= GB:
		return fmt.Sprintf("%.1f GB/s", bytesPerSec/GB)
	case bytesPerSec >= MB:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/MB)
	case bytesPerSec >= KB:
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/KB)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

// FormatBytes formats bytes into human readable string
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
