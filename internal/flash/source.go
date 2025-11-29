package flash

import (
	"archive/zip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
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
// Supports: .img, .iso, .bin, .raw (raw) and .zip (streaming extraction).
// Also supports HTTP/HTTPS URLs for remote image streaming.
func OpenSource(path string) (Source, error) {
	// Check if path is a URL and handle remote sources
	if IsURL(path) {
		return newURLSource(path)
	}

	// Handle local files based on extension
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

// httpClient is a shared HTTP client with appropriate timeouts for streaming.
// Uses longer timeouts since we're streaming large files.
var httpClient = &http.Client{
	Timeout: 0, // No overall timeout - we handle this via context
	Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true, // We want the raw bytes
	},
}

// urlSource streams image data from a remote HTTP/HTTPS URL.
// Supports both direct image files and compressed archives.
type urlSource struct {
	resp     *http.Response
	body     io.ReadCloser
	size     int64
	name     string
	isZip    bool
	zipInner io.ReadCloser // Inner reader when streaming from zip
}

// IsURL returns true if the path looks like an HTTP/HTTPS URL.
func IsURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

// newURLSource creates a new source that streams from a remote URL.
// It performs a HEAD request first to get the content size, then opens
// a GET request for streaming.
func newURLSource(rawURL string) (*urlSource, error) {
	// Validate URL format
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme: %s (use http or https)", parsedURL.Scheme)
	}

	// Perform HEAD request to get content size and type
	headResp, err := httpClient.Head(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to URL: %w", err)
	}
	headResp.Body.Close()

	if headResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned error: %s", headResp.Status)
	}

	// Get content size from Content-Length header
	contentLength := headResp.ContentLength
	if contentLength <= 0 {
		return nil, fmt.Errorf("server did not provide content size (Content-Length header missing or invalid)")
	}

	// Detect filename and format from URL and headers
	filename, isZip := detectURLType(rawURL, headResp)

	// Open GET request for streaming
	getResp, err := httpClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to start download: %w", err)
	}

	if getResp.StatusCode != http.StatusOK {
		getResp.Body.Close()
		return nil, fmt.Errorf("server returned error: %s", getResp.Status)
	}

	src := &urlSource{
		resp:  getResp,
		body:  getResp.Body,
		size:  contentLength,
		name:  filename,
		isZip: isZip,
	}

	// If it's a zip file, we cannot stream-extract from HTTP without downloading first
	// because zip format requires random access to read the central directory.
	// For zip URLs, we'll read the whole response and handle it differently.
	if isZip {
		return nil, fmt.Errorf("zip files from URLs are not supported (zip format requires random access); download the file first or use a direct image URL")
	}

	return src, nil
}

// detectURLType determines the filename and format from a URL and HTTP response.
// It checks the URL path, Content-Disposition header, and Content-Type header.
func detectURLType(rawURL string, resp *http.Response) (filename string, isZip bool) {
	// Try Content-Disposition header first (most reliable for downloads)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, err := mime.ParseMediaType(cd)
		if err == nil {
			if name, ok := params["filename"]; ok && name != "" {
				filename = name
				ext := strings.ToLower(filepath.Ext(filename))
				return filename, ext == ".zip"
			}
		}
	}

	// Try to extract filename from URL path
	parsedURL, err := url.Parse(rawURL)
	if err == nil {
		path := parsedURL.Path
		if path != "" && path != "/" {
			filename = filepath.Base(path)
			// Clean up URL-encoded characters
			if decoded, err := url.PathUnescape(filename); err == nil {
				filename = decoded
			}
			ext := strings.ToLower(filepath.Ext(filename))
			if ext != "" {
				return filename, ext == ".zip"
			}
		}
	}

	// Fall back to Content-Type header
	contentType := resp.Header.Get("Content-Type")
	switch {
	case strings.Contains(contentType, "application/zip"):
		return "download.zip", true
	case strings.Contains(contentType, "application/x-iso9660-image"):
		return "download.iso", false
	case strings.Contains(contentType, "application/octet-stream"):
		return "download.img", false
	default:
		// Default to .img for unknown types
		return "download.img", false
	}
}

func (u *urlSource) Size() int64 {
	return u.size
}

func (u *urlSource) Read(p []byte) (n int, err error) {
	if u.zipInner != nil {
		return u.zipInner.Read(p)
	}
	return u.body.Read(p)
}

func (u *urlSource) Close() error {
	if u.zipInner != nil {
		u.zipInner.Close()
	}
	return u.body.Close()
}

func (u *urlSource) Name() string {
	return u.name
}
