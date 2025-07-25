package wal

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// WAL defines the interface for Write-Ahead Logging
type WAL interface {
	// Append writes events to WAL and returns the position
	Append(ctx context.Context, events []*domain.UnifiedEvent) (Position, error)

	// Read reads events from a position range
	Read(ctx context.Context, from, to Position) ([]*domain.UnifiedEvent, error)

	// Checkpoint marks events up to position as safely persisted
	Checkpoint(ctx context.Context, position Position) error

	// GetCheckpoint returns the last checkpoint position
	GetCheckpoint(ctx context.Context) (Position, error)

	// Replay replays events from the last checkpoint
	Replay(ctx context.Context) (<-chan *domain.UnifiedEvent, error)

	// Metrics returns WAL performance metrics
	Metrics() Metrics

	// Close closes the WAL
	Close() error
}

// Position represents a position in the WAL
type Position struct {
	Segment uint64 // Segment number
	Offset  uint64 // Offset within segment
}

// Metrics contains WAL performance metrics
type Metrics struct {
	WrittenEvents   uint64
	WrittenBytes    uint64
	CheckpointLag   uint64 // Events pending checkpoint
	LastWriteTime   time.Time
	LastCheckpoint  time.Time
	SegmentCount    int
	TotalSize       uint64
	WriteLatencyP99 time.Duration
}

// Config contains WAL configuration
type Config struct {
	// Directory for WAL files
	Dir string

	// Maximum size of a segment file
	SegmentSize int64

	// Sync policy
	SyncPolicy SyncPolicy

	// Compression for closed segments
	Compression CompressionType

	// Retention for old segments
	RetentionDuration time.Duration

	// Maximum number of segments
	MaxSegments int
}

// SyncPolicy defines when to sync WAL to disk
type SyncPolicy int

const (
	// SyncImmediate syncs after every write (safest, slowest)
	SyncImmediate SyncPolicy = iota
	// SyncInterval syncs periodically
	SyncInterval
	// SyncBatch syncs after N events or timeout
	SyncBatch
)

// CompressionType defines compression algorithm
type CompressionType int

const (
	CompressionNone CompressionType = iota
	CompressionSnappy
	CompressionZstd
	CompressionLZ4
)
