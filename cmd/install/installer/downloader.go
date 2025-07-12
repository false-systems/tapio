package installer

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// downloader implements the Downloader interface
type downloader struct {
	client        *http.Client
	maxConcurrent int
	chunkSize     int64
	resumable     bool
}

// NewDownloader creates a new downloader
func NewDownloader(client *http.Client) Downloader {
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
				MaxIdleConns:       10,
				IdleConnTimeout:    90 * time.Second,
				DisableCompression: true,
			},
		}
	}

	return &downloader{
		client:        client,
		maxConcurrent: 4,
		chunkSize:     1024 * 1024, // 1MB chunks
		resumable:     true,
	}
}

// Download downloads a file
func (d *downloader) Download(ctx context.Context, opts DownloadOptions, dst io.Writer) error {
	return d.download(ctx, opts, dst, nil)
}

// DownloadWithProgress downloads with progress reporting
func (d *downloader) DownloadWithProgress(ctx context.Context, opts DownloadOptions, dst io.Writer, progress func(current, total int64)) error {
	return d.download(ctx, opts, dst, progress)
}

// download performs the actual download
func (d *downloader) download(ctx context.Context, opts DownloadOptions, dst io.Writer, progress func(current, total int64)) error {
	// Apply circuit breaker if provided
	if opts.CircuitBreaker != nil {
		return opts.CircuitBreaker.Execute(func() error {
			return d.performDownload(ctx, opts, dst, progress)
		})
	}

	return d.performDownload(ctx, opts, dst, progress)
}

// performDownload performs the download with retry logic
func (d *downloader) performDownload(ctx context.Context, opts DownloadOptions, dst io.Writer, progress func(current, total int64)) error {
	var lastErr error

	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			delay := opts.RetryDelay
			if delay == 0 {
				delay = time.Duration(attempt) * 5 * time.Second
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := d.doDownload(ctx, opts, dst, progress)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !d.isRetryableError(err) {
			return err
		}
	}

	return fmt.Errorf("download failed after %d attempts: %w", maxRetries, lastErr)
}

// doDownload performs a single download attempt
func (d *downloader) doDownload(ctx context.Context, opts DownloadOptions, dst io.Writer, progress func(current, total int64)) error {
	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", opts.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("User-Agent", "Tapio-Installer/1.0")

	// Handle proxy
	if opts.ProxyURL != "" {
		proxyURL, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}
		d.client.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)
	}

	// Apply timeout
	if opts.Timeout > 0 {
		ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	// Perform request
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	// Get content length
	contentLength := resp.ContentLength

	// Check if server supports partial content for resumable downloads
	acceptRanges := resp.Header.Get("Accept-Ranges")
	supportsResume := acceptRanges == "bytes" && contentLength > 0

	// Download based on size and resume support
	if contentLength > 10*1024*1024 && supportsResume && d.resumable {
		// Use concurrent chunked download for large files
		return d.downloadChunked(ctx, opts.URL, dst, contentLength, progress)
	}

	// Use simple download for small files
	return d.downloadSimple(resp.Body, dst, contentLength, progress)
}

// downloadSimple performs a simple sequential download
func (d *downloader) downloadSimple(src io.Reader, dst io.Writer, total int64, progress func(current, total int64)) error {
	if progress != nil && total > 0 {
		progress(0, total)
	}

	// Create progress reader
	pr := &progressReader{
		reader:   src,
		total:    total,
		progress: progress,
	}

	// Copy data
	_, err := io.Copy(dst, pr)
	return err
}

// downloadChunked performs concurrent chunked download
func (d *downloader) downloadChunked(ctx context.Context, url string, dst io.Writer, total int64, progress func(current, total int64)) error {
	// Calculate chunks
	numChunks := int(total/d.chunkSize) + 1
	if numChunks > d.maxConcurrent {
		numChunks = d.maxConcurrent
	}

	chunkSize := total / int64(numChunks)

	// Create chunks
	chunks := make([]*chunk, numChunks)
	for i := 0; i < numChunks; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == numChunks-1 {
			end = total - 1
		}

		chunks[i] = &chunk{
			index: i,
			start: start,
			end:   end,
			data:  make([]byte, 0, end-start+1),
		}
	}

	// Download chunks concurrently
	var wg sync.WaitGroup
	chunkChan := make(chan *chunk, numChunks)
	errorChan := make(chan error, numChunks)

	// Progress tracking
	var progressMu sync.Mutex
	var currentBytes int64

	updateProgress := func(bytes int64) {
		if progress == nil {
			return
		}
		progressMu.Lock()
		currentBytes += bytes
		progress(currentBytes, total)
		progressMu.Unlock()
	}

	// Start workers
	for i := 0; i < d.maxConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for chunk := range chunkChan {
				if err := d.downloadChunk(ctx, url, chunk, updateProgress); err != nil {
					errorChan <- err
					return
				}
			}
		}()
	}

	// Queue chunks
	go func() {
		for _, chunk := range chunks {
			chunkChan <- chunk
		}
		close(chunkChan)
	}()

	// Wait for completion
	wg.Wait()
	close(errorChan)

	// Check for errors
	if err := <-errorChan; err != nil {
		return err
	}

	// Write chunks in order
	for _, chunk := range chunks {
		if _, err := dst.Write(chunk.data); err != nil {
			return fmt.Errorf("failed to write chunk %d: %w", chunk.index, err)
		}
	}

	return nil
}

// chunk represents a download chunk
type chunk struct {
	index int
	start int64
	end   int64
	data  []byte
}

// downloadChunk downloads a single chunk
func (d *downloader) downloadChunk(ctx context.Context, url string, chunk *chunk, updateProgress func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	// Set range header
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.start, chunk.end))

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("server does not support partial content: %s", resp.Status)
	}

	// Read chunk data
	chunk.data = make([]byte, 0, chunk.end-chunk.start+1)
	buf := make([]byte, 32*1024) // 32KB buffer

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk.data = append(chunk.data, buf[:n]...)
			updateProgress(int64(n))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	expectedSize := chunk.end - chunk.start + 1
	if int64(len(chunk.data)) != expectedSize {
		return fmt.Errorf("chunk %d size mismatch: expected %d, got %d",
			chunk.index, expectedSize, len(chunk.data))
	}

	return nil
}

// isRetryableError checks if an error is retryable
func (d *downloader) isRetryableError(err error) bool {
	// Network errors are typically retryable
	if _, ok := err.(*url.Error); ok {
		return true
	}

	// Check for specific error strings
	errStr := err.Error()
	retryableErrors := []string{
		"connection reset",
		"broken pipe",
		"timeout",
		"temporary failure",
		"no such host",
	}

	for _, re := range retryableErrors {
		if strings.Contains(strings.ToLower(errStr), re) {
			return true
		}
	}

	return false
}

// progressReader wraps a reader with progress reporting
type progressReader struct {
	reader   io.Reader
	total    int64
	current  int64
	progress func(current, total int64)
}

func (r *progressReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		r.current += int64(n)
		if r.progress != nil {
			r.progress(r.current, r.total)
		}
	}
	return
}

// DownloadFile downloads a file to disk with automatic resume support
func DownloadFile(ctx context.Context, url, destPath string, opts DownloadOptions) error {
	// Create temporary file
	tempPath := destPath + ".download"

	// Check if partial download exists
	var resumeOffset int64
	if info, err := os.Stat(tempPath); err == nil {
		resumeOffset = info.Size()
	}

	// Open file for writing
	var file *os.File
	var err error

	if resumeOffset > 0 {
		// Resume download
		file, err = os.OpenFile(tempPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open file for resume: %w", err)
		}
	} else {
		// Create new file
		if err := os.MkdirAll(filepath.Dir(tempPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		file, err = os.Create(tempPath)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
	}
	defer file.Close()

	// Create downloader
	dl := NewDownloader(nil)

	// Download file
	if err := dl.Download(ctx, opts, file); err != nil {
		return err
	}

	// Close file before rename
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	// Rename to final destination
	if err := os.Rename(tempPath, destPath); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}
