package internal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// collector implements the core.Collector interface
type collector struct {
	// Configuration
	config core.Config

	// State management
	started atomic.Bool
	stopped atomic.Bool

	// Event processing
	eventChan chan domain.Event
	processor core.EventProcessor

	// Cursor management
	cursorManager core.CursorManager
	lastCursor    atomic.Value // string

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Statistics
	stats struct {
		eventsCollected atomic.Uint64
		eventsDropped   atomic.Uint64
		entriesRead     atomic.Uint64
		bytesRead       atomic.Uint64
		cursorUpdates   atomic.Uint64
		journalSeeks    atomic.Uint64
		readErrors      atomic.Uint64
	}

	// Health tracking
	lastEventTime atomic.Value // time.Time
	startTime     time.Time

	// Platform-specific implementation
	impl platformImpl
}

// platformImpl is the platform-specific interface (defined in platform.go)
// type platformImpl interface { ... }

// NewCollector creates a new journald collector
func NewCollector(config core.Config) (core.Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	c := &collector{
		config:    config,
		eventChan: make(chan domain.Event, config.EventBufferSize),
		startTime: time.Now(),
		processor: newEventProcessor(),
	}

	// Initialize cursor manager
	if config.PersistCursor {
		c.cursorManager = newFileCursorManager(config.CursorFile)
	} else {
		c.cursorManager = newMemoryCursorManager()
	}

	// Initialize platform-specific implementation
	impl, err := newPlatformImpl()
	if err != nil {
		return nil, fmt.Errorf("failed to create platform implementation: %w", err)
	}

	if err := impl.Init(config); err != nil {
		return nil, fmt.Errorf("failed to initialize platform implementation: %w", err)
	}

	c.impl = impl
	c.lastEventTime.Store(time.Now())
	c.lastCursor.Store("")

	return c, nil
}

// Start begins log collection
func (c *collector) Start(ctx context.Context) error {
	if !c.config.Enabled {
		return fmt.Errorf("collector is disabled")
	}

	if c.started.Load() {
		return core.ErrAlreadyStarted
	}

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start platform implementation
	if err := c.impl.Start(c.ctx); err != nil {
		return fmt.Errorf("failed to start platform implementation: %w", err)
	}

	// Seek to appropriate position
	if err := c.seekToPosition(); err != nil {
		return fmt.Errorf("failed to seek to position: %w", err)
	}

	// Mark as started
	c.started.Store(true)

	// Start reading loop
	c.wg.Add(1)
	go c.readLoop()

	// Start cursor management
	if c.config.PersistCursor {
		c.wg.Add(1)
		go c.cursorFlushLoop()
	}

	return nil
}

// Stop gracefully stops the collector
func (c *collector) Stop() error {
	if !c.started.Load() {
		return core.ErrNotStarted
	}

	if c.stopped.Load() {
		return nil
	}

	// Mark as stopping
	c.stopped.Store(true)

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Save final cursor
	if c.config.PersistCursor {
		cursor := c.lastCursor.Load().(string)
		if cursor != "" {
			c.cursorManager.SaveCursor(cursor)
		}
	}

	// Stop platform implementation
	if err := c.impl.Stop(); err != nil {
		return fmt.Errorf("failed to stop platform implementation: %w", err)
	}

	// Wait for goroutines
	c.wg.Wait()

	// Close event channel
	close(c.eventChan)

	return nil
}

// Events returns the event channel
func (c *collector) Events() <-chan domain.Event {
	return c.eventChan
}

// Health returns the current health status
func (c *collector) Health() core.Health {
	status := core.HealthStatusHealthy
	message := "journald collector is healthy"

	if !c.started.Load() {
		status = core.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = core.HealthStatusUnhealthy
		message = "Collector stopped"
	} else if !c.impl.IsOpen() {
		status = core.HealthStatusUnhealthy
		message = "Journal not open"
	} else if c.stats.readErrors.Load() > 100 {
		status = core.HealthStatusDegraded
		message = fmt.Sprintf("High read error count: %d", c.stats.readErrors.Load())
	}

	lastEvent := c.lastEventTime.Load().(time.Time)
	if time.Since(lastEvent) > 5*time.Minute && c.started.Load() {
		status = core.HealthStatusDegraded
		message = "No events received in 5 minutes"
	}

	return core.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   lastEvent,
		EventsProcessed: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		ErrorCount:      c.stats.readErrors.Load(),
		JournalOpen:     c.impl.IsOpen(),
		CurrentCursor:   c.impl.CurrentCursor(),
		BootID:          c.impl.BootID(),
		MachineID:       c.impl.MachineID(),
		Metrics: map[string]float64{
			"entries_read":      float64(c.stats.entriesRead.Load()),
			"bytes_read":        float64(c.stats.bytesRead.Load()),
			"cursor_updates":    float64(c.stats.cursorUpdates.Load()),
			"journal_seeks":     float64(c.stats.journalSeeks.Load()),
			"read_errors":       float64(c.stats.readErrors.Load()),
			"events_per_second": c.getEventsPerSecond(),
		},
	}
}

// Statistics returns runtime statistics
func (c *collector) Statistics() core.Statistics {
	uptime := time.Since(c.startTime)

	return core.Statistics{
		StartTime:       c.startTime,
		EventsCollected: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		BytesRead:       c.stats.bytesRead.Load(),
		EntriesRead:     c.stats.entriesRead.Load(),
		CursorUpdates:   c.stats.cursorUpdates.Load(),
		JournalSeeks:    c.stats.journalSeeks.Load(),
		ReadErrors:      c.stats.readErrors.Load(),
		Custom: map[string]interface{}{
			"uptime_seconds":    uptime.Seconds(),
			"events_per_second": c.getEventsPerSecond(),
			"journal_open":      c.impl.IsOpen(),
			"current_cursor":    c.impl.CurrentCursor(),
			"boot_id":           c.impl.BootID(),
			"machine_id":        c.impl.MachineID(),
		},
	}
}

// Configure updates the collector configuration
func (c *collector) Configure(config core.Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	c.config = config
	return nil
}

// seekToPosition seeks the journal to the appropriate starting position
func (c *collector) seekToPosition() error {
	reader := c.impl.Reader()
	if reader == nil {
		return fmt.Errorf("no reader available")
	}

	// Try to load saved cursor first
	if c.config.PersistCursor && c.cursorManager.HasCursor() {
		cursor, err := c.cursorManager.LoadCursor()
		if err == nil && cursor != "" {
			if err := reader.SeekCursor(cursor); err == nil {
				c.stats.journalSeeks.Add(1)
				return nil
			}
		}
	}

	// Use initial cursor if provided
	if c.config.InitialCursor != "" {
		if err := reader.SeekCursor(c.config.InitialCursor); err == nil {
			c.stats.journalSeeks.Add(1)
			return nil
		}
	}

	// Use time-based seeking
	if !c.config.Since.IsZero() {
		if err := reader.SeekTime(c.config.Since); err == nil {
			c.stats.journalSeeks.Add(1)
			return nil
		}
	}

	// Default to end if in follow mode, beginning otherwise
	if c.config.SeekToEnd || c.config.FollowMode {
		// Seek to end - this is usually the default for journald
		return nil
	}

	return nil
}

// readLoop continuously reads journal entries
func (c *collector) readLoop() {
	defer c.wg.Done()

	reader := c.impl.Reader()
	if reader == nil {
		return
	}

	batchCount := 0
	for {
		select {
		case <-c.ctx.Done():
			return

		default:
			// Read next entry
			entry, err := reader.ReadEntry()
			if err != nil {
				c.stats.readErrors.Add(1)

				// Handle different error types
				if err == core.ErrNoMoreEntries {
					if c.config.FollowMode {
						// Wait for new entries
						reader.WaitForEntries(c.config.ReadTimeout)
						continue
					} else {
						// Batch mode - we're done
						return
					}
				}

				// Other errors - short delay and continue
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Update statistics
			c.stats.entriesRead.Add(1)
			if entry != nil {
				c.stats.bytesRead.Add(uint64(len(entry.Message)))

				// Update cursor
				if entry.Cursor != "" {
					c.lastCursor.Store(entry.Cursor)
					c.stats.cursorUpdates.Add(1)
				}

				// Process entry into event
				event, err := c.processor.ProcessEntry(c.ctx, entry)
				if err != nil {
					c.stats.readErrors.Add(1)
					continue
				}

				// Update stats
				c.stats.eventsCollected.Add(1)
				c.lastEventTime.Store(time.Now())

				// Try to send event
				select {
				case c.eventChan <- event:
					// Event sent successfully
				default:
					// Buffer full, drop event
					c.stats.eventsDropped.Add(1)
				}
			}

			// Batch processing for performance
			batchCount++
			if batchCount >= c.config.BatchSize {
				batchCount = 0
				// Small yield to prevent busy loop
				time.Sleep(1 * time.Millisecond)
			}
		}
	}
}

// cursorFlushLoop periodically saves cursor to persistent storage
func (c *collector) cursorFlushLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return

		case <-ticker.C:
			cursor := c.lastCursor.Load().(string)
			if cursor != "" {
				c.cursorManager.SaveCursor(cursor)
			}
		}
	}
}

// Helper methods

func (c *collector) getEventsPerSecond() float64 {
	uptime := time.Since(c.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(c.stats.eventsCollected.Load()) / uptime
}
