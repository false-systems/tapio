package pipeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CorrelationWAL provides Write-Ahead Logging for correlation findings reliability
// Ensures no correlation findings are lost during transit to persistent storage
type CorrelationWAL struct {
	// WAL file path
	walPath string

	// Current log file
	logFile *os.File
	writer  *bufio.Writer

	// Synchronization
	mu sync.Mutex

	// Configuration
	maxFileSize  int64
	syncInterval time.Duration

	// Metrics
	entriesWritten uint64
	bytesWritten   uint64
}

// WALEntry represents a single WAL entry
type WALEntry struct {
	Timestamp   time.Time          `json:"timestamp"`
	EntryType   string             `json:"entry_type"`
	Correlation *CorrelationOutput `json:"correlation,omitempty"`
	Metadata    map[string]string  `json:"metadata,omitempty"`
}

// WALConfig configures the Write-Ahead Log
type WALConfig struct {
	WALPath      string
	MaxFileSize  int64
	SyncInterval time.Duration
}

// NewCorrelationWAL creates a new Write-Ahead Log for correlations
func NewCorrelationWAL(config WALConfig) (*CorrelationWAL, error) {
	if config.WALPath == "" {
		config.WALPath = "./tapio-correlation.wal"
	}

	if config.MaxFileSize <= 0 {
		config.MaxFileSize = 100 * 1024 * 1024 // 100MB default
	}

	if config.SyncInterval <= 0 {
		config.SyncInterval = 1 * time.Second
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(config.WALPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	// Open WAL file
	logFile, err := os.OpenFile(config.WALPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	wal := &CorrelationWAL{
		walPath:      config.WALPath,
		logFile:      logFile,
		writer:       bufio.NewWriter(logFile),
		maxFileSize:  config.MaxFileSize,
		syncInterval: config.SyncInterval,
	}

	// Start periodic sync
	go wal.startPeriodicSync()

	return wal, nil
}

// WriteCorrelation writes a correlation output to WAL
func (wal *CorrelationWAL) WriteCorrelation(correlation *CorrelationOutput) error {
	wal.mu.Lock()
	defer wal.mu.Unlock()

	entry := WALEntry{
		Timestamp:   time.Now(),
		EntryType:   "correlation",
		Correlation: correlation,
		Metadata: map[string]string{
			"event_id":    correlation.OriginalEvent.ID,
			"confidence":  fmt.Sprintf("%.2f", correlation.Confidence),
			"result_type": string(correlation.ResultType),
		},
	}

	// Serialize entry to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal WAL entry: %w", err)
	}

	// Write to log with newline
	if _, err := wal.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}

	if _, err := wal.writer.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline to WAL: %w", err)
	}

	// Update metrics
	wal.entriesWritten++
	wal.bytesWritten += uint64(len(data) + 1) // +1 for newline

	return nil
}

// WriteBatch writes multiple correlations to WAL efficiently
func (wal *CorrelationWAL) WriteBatch(correlations []*CorrelationOutput) error {
	wal.mu.Lock()
	defer wal.mu.Unlock()

	for _, correlation := range correlations {
		entry := WALEntry{
			Timestamp:   time.Now(),
			EntryType:   "correlation",
			Correlation: correlation,
			Metadata: map[string]string{
				"event_id":    correlation.OriginalEvent.ID,
				"confidence":  fmt.Sprintf("%.2f", correlation.Confidence),
				"result_type": string(correlation.ResultType),
			},
		}

		// Serialize entry to JSON
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal WAL entry: %w", err)
		}

		// Write to log with newline
		if _, err := wal.writer.Write(data); err != nil {
			return fmt.Errorf("failed to write to WAL: %w", err)
		}

		if _, err := wal.writer.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write newline to WAL: %w", err)
		}

		// Update metrics
		wal.entriesWritten++
		wal.bytesWritten += uint64(len(data) + 1)
	}

	return nil
}

// Sync flushes the WAL to disk
func (wal *CorrelationWAL) Sync() error {
	wal.mu.Lock()
	defer wal.mu.Unlock()

	if err := wal.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL buffer: %w", err)
	}

	if err := wal.logFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL to disk: %w", err)
	}

	return nil
}

// startPeriodicSync performs periodic WAL synchronization
func (wal *CorrelationWAL) startPeriodicSync() {
	ticker := time.NewTicker(wal.syncInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := wal.Sync(); err != nil {
			// Log error but continue (WAL should be resilient)
			fmt.Printf("WAL sync error: %v\n", err)
		}
	}
}

// Close closes the WAL file
func (wal *CorrelationWAL) Close() error {
	wal.mu.Lock()
	defer wal.mu.Unlock()

	// Final sync
	if err := wal.Sync(); err != nil {
		fmt.Printf("Final WAL sync error: %v\n", err)
	}

	// Close file
	if err := wal.logFile.Close(); err != nil {
		return fmt.Errorf("failed to close WAL file: %w", err)
	}

	return nil
}

// GetMetrics returns WAL metrics
func (wal *CorrelationWAL) GetMetrics() WALMetrics {
	wal.mu.Lock()
	defer wal.mu.Unlock()

	// Get file size
	fileInfo, _ := wal.logFile.Stat()
	fileSize := int64(0)
	if fileInfo != nil {
		fileSize = fileInfo.Size()
	}

	return WALMetrics{
		EntriesWritten: wal.entriesWritten,
		BytesWritten:   wal.bytesWritten,
		FileSize:       fileSize,
		FilePath:       wal.walPath,
	}
}

// WALMetrics contains WAL statistics
type WALMetrics struct {
	EntriesWritten uint64 `json:"entries_written"`
	BytesWritten   uint64 `json:"bytes_written"`
	FileSize       int64  `json:"file_size"`
	FilePath       string `json:"file_path"`
}

// ReplayWAL reads and replays WAL entries (for recovery)
func ReplayWAL(walPath string, processor func(*CorrelationOutput) error) error {
	file, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No WAL file exists, nothing to replay
		}
		return fmt.Errorf("failed to open WAL file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if line == "" {
			continue // Skip empty lines
		}

		var entry WALEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			fmt.Printf("WAL replay: failed to unmarshal line %d: %v\n", lineNum, err)
			continue
		}

		// Process correlation entry
		if entry.EntryType == "correlation" && entry.Correlation != nil {
			if err := processor(entry.Correlation); err != nil {
				fmt.Printf("WAL replay: failed to process correlation at line %d: %v\n", lineNum, err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading WAL file: %w", err)
	}

	return nil
}
