package flash

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Source represents an image source that can be read sequentially.
// Implementations handle different formats (raw, zip) transparently.
type Source interface {
	// Size returns the total uncompressed size in bytes
	Size() int64
	// Read reads up to len(p) bytes into p
	Read(p []byte) (n int, err error)
	// Close releases any resources
	Close() error
	// Name returns the source filename for display
	Name() string
}

// OpenSource opens an image file and returns the appropriate Source implementation.
// Supports: .img, .iso, .bin (raw) and .zip (streaming extraction)
func OpenSource(path string) (Source, error) {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".zip":
		return newZipSource(path)
	case ".img", ".iso", ".bin", ".raw":
		return newRawSource(path)
	default:
		// Try as raw file for unknown extensions
		return newRawSource(path)
	}
}

// rawSource reads directly from an uncompressed image file
type rawSource struct {
	file *os.File
	size int64
	name string
}

func newRawSource(path string) (*rawSource, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat image: %w", err)
	}

	if info.Size() == 0 {
		file.Close()
		return nil, fmt.Errorf("image file is empty")
	}

	return &rawSource{
		file: file,
		size: info.Size(),
		name: filepath.Base(path),
	}, nil
}

func (r *rawSource) Size() int64 {
	return r.size
}

func (r *rawSource) Read(p []byte) (n int, err error) {
	return r.file.Read(p)
}

func (r *rawSource) Close() error {
	return r.file.Close()
}

func (r *rawSource) Name() string {
	return r.name
}

// zipSource extracts and streams the first image file from a zip archive
type zipSource struct {
	zipReader  *zip.ReadCloser
	fileReader io.ReadCloser
	size       int64
	name       string
}

// Supported image extensions inside zip files
var imageExtensions = map[string]bool{
	".img": true,
	".iso": true,
	".bin": true,
	".raw": true,
}

func newZipSource(path string) (*zipSource, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}

	// Find the first image file in the archive
	var imageFile *zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if imageExtensions[ext] {
			imageFile = f
			break
		}
	}

	if imageFile == nil {
		zr.Close()
		return nil, fmt.Errorf("no image file found in zip (supported: .img, .iso, .bin, .raw)")
	}

	// Open the image file for streaming
	fr, err := imageFile.Open()
	if err != nil {
		zr.Close()
		return nil, fmt.Errorf("failed to open image in zip: %w", err)
	}

	return &zipSource{
		zipReader:  zr,
		fileReader: fr,
		size:       int64(imageFile.UncompressedSize64),
		name:       filepath.Base(imageFile.Name),
	}, nil
}

func (z *zipSource) Size() int64 {
	return z.size
}

func (z *zipSource) Read(p []byte) (n int, err error) {
	return z.fileReader.Read(p)
}

func (z *zipSource) Close() error {
	z.fileReader.Close()
	return z.zipReader.Close()
}

func (z *zipSource) Name() string {
	return z.name
}
