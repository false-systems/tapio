package systemdapi

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Collector implements systemd journal monitoring for Kubernetes environments
type Collector struct {
	// Core configuration
	name   string
	config Config
	logger *zap.Logger

	// Journal reader
	journal *sdjournal.Journal
	mu      sync.RWMutex

	// Event processing
	events chan *domain.CollectorEvent
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Health and lifecycle
	healthy   bool
	startTime time.Time
	lastRead  time.Time

	// Rate limiting
	rateLimiter *rate.Limiter

	// Statistics
	stats *CollectorStats

	// OpenTelemetry instrumentation - MANDATORY pattern from CLAUDE.md
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// systemd-api specific metrics
	journalPosition   metric.Int64Gauge
	journalConnected  metric.Int64Gauge
	connectionRetries metric.Int64Counter
}

// NewCollector creates a new systemd-api collector with full OTEL instrumentation
func NewCollector(name string, config Config) (*Collector, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize production logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OpenTelemetry components following CLAUDE.md standards
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total journal events processed by %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors encountered by %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Journal event processing duration in milliseconds for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total journal events dropped due to buffer overflow by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current event buffer usage for %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	journalPosition, err := meter.Int64Gauge(
		fmt.Sprintf("%s_journal_position", name),
		metric.WithDescription(fmt.Sprintf("Current journal position for %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create journal position gauge", zap.Error(err))
	}

	journalConnected, err := meter.Int64Gauge(
		fmt.Sprintf("%s_journal_connected", name),
		metric.WithDescription(fmt.Sprintf("Journal connection status for %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create journal connected gauge", zap.Error(err))
	}

	connectionRetries, err := meter.Int64Counter(
		fmt.Sprintf("%s_connection_retries_total", name),
		metric.WithDescription(fmt.Sprintf("Total journal connection retries for %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create connection retries counter", zap.Error(err))
	}

	// Initialize rate limiter
	var rateLimiter *rate.Limiter
	if config.EventRate > 0 {
		rateLimiter = rate.NewLimiter(rate.Limit(config.EventRate), config.BurstSize)
	}

	// Initialize statistics
	stats := &CollectorStats{
		EntriesProcessed:  0,
		EntriesDropped:    0,
		ErrorsTotal:       0,
		JournalPosition:   0,
		JournalConnected:  false,
		BufferUtilization: 0.0,
		ProcessingLatency: 0.0,
		LastActivity:      time.Now(),
		UptimeSeconds:     0,
		ConnectionRetries: 0,
	}

	return &Collector{
		name:              name,
		config:            config,
		logger:            logger.Named(name),
		events:            make(chan *domain.CollectorEvent, config.BufferSize),
		rateLimiter:       rateLimiter,
		stats:             stats,
		tracer:            tracer,
		eventsProcessed:   eventsProcessed,
		errorsTotal:       errorsTotal,
		processingTime:    processingTime,
		droppedEvents:     droppedEvents,
		bufferUsage:       bufferUsage,
		journalPosition:   journalPosition,
		journalConnected:  journalConnected,
		connectionRetries: connectionRetries,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins journal monitoring
func (c *Collector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "systemd-api.start")
	defer span.End()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		span.SetAttributes(attribute.String("error", "collector_already_started"))
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.startTime = time.Now()

	// Initialize journal connection
	if err := c.initJournal(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "journal_init_failed"),
			))
		}
		span.SetAttributes(attribute.String("error", err.Error()))
		return fmt.Errorf("failed to initialize journal: %w", err)
	}

	// Start journal reading goroutine
	c.wg.Add(1)
	go c.readJournalLoop()

	// Start health monitoring
	c.wg.Add(1)
	go c.healthMonitorLoop()

	c.healthy = true
	c.logger.Info("Systemd-api collector started",
		zap.String("name", c.name),
		zap.Int("buffer_size", c.config.BufferSize),
		zap.Strings("units", c.config.Units),
		zap.Int("priority", int(c.config.Priority)),
	)

	span.SetAttributes(
		attribute.String("collector", c.name),
		attribute.Int("buffer_size", c.config.BufferSize),
		attribute.StringSlice("units", c.config.Units),
	)

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Wait for goroutines to finish
	c.wg.Wait()

	// Close journal
	if c.journal != nil {
		if err := c.journal.Close(); err != nil {
			c.logger.Warn("Failed to close journal", zap.Error(err))
		}
	}

	// Close events channel
	close(c.events)
	c.healthy = false

	c.logger.Info("Systemd-api collector stopped")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy && c.journal != nil
}

// initJournal initializes the systemd journal connection
func (c *Collector) initJournal() error {
	var err error

	// Open journal
	if c.config.JournalPath != "" {
		c.journal, err = sdjournal.NewJournalFromDir(c.config.JournalPath)
	} else {
		c.journal, err = sdjournal.NewJournal()
	}

	if err != nil {
		return fmt.Errorf("failed to open journal: %w", err)
	}

	// Apply filters
	if err := c.applyFilters(); err != nil {
		c.journal.Close()
		return fmt.Errorf("failed to apply filters: %w", err)
	}

	// Seek to appropriate position
	if err := c.seekJournal(); err != nil {
		c.journal.Close()
		return fmt.Errorf("failed to seek journal: %w", err)
	}

	c.stats.JournalConnected = true
	if c.journalConnected != nil {
		c.journalConnected.Record(context.Background(), 1)
	}

	c.logger.Info("Journal initialized successfully",
		zap.String("path", c.config.JournalPath),
		zap.Bool("follow_mode", c.config.FollowMode),
		zap.Int("filters", len(c.config.GetJournalMatches())),
	)

	return nil
}

// applyFilters applies journal filtering configuration
func (c *Collector) applyFilters() error {
	// Clear existing matches
	c.journal.FlushMatches()

	// Add unit filters
	matches := c.config.GetJournalMatches()
	for _, match := range matches {
		if err := c.journal.AddMatch(match.String()); err != nil {
			return fmt.Errorf("failed to add match %s: %w", match.String(), err)
		}
	}

	c.logger.Debug("Applied journal filters",
		zap.Int("match_count", len(matches)),
		zap.Strings("units", c.config.Units),
	)

	return nil
}

// seekJournal positions the journal cursor appropriately
func (c *Collector) seekJournal() error {
	if c.config.FollowMode {
		// Start from the end (tail mode)
		if err := c.journal.SeekTail(); err != nil {
			return fmt.Errorf("failed to seek to tail: %w", err)
		}
		c.logger.Debug("Seeking to journal tail")
	} else {
		// Start from a specific time or beginning
		if seekTime, shouldSeek := c.config.GetSeekPosition(); shouldSeek {
			if err := c.journal.SeekRealtimeUsec(uint64(seekTime.Unix() * 1000000)); err != nil {
				return fmt.Errorf("failed to seek to time %v: %w", seekTime, err)
			}
			c.logger.Debug("Seeking to specific time", zap.Time("time", seekTime))
		} else {
			if err := c.journal.SeekHead(); err != nil {
				return fmt.Errorf("failed to seek to head: %w", err)
			}
			c.logger.Debug("Seeking to journal head")
		}
	}

	return nil
}

// readJournalLoop continuously reads from the journal
func (c *Collector) readJournalLoop() {
	defer c.wg.Done()

	c.logger.Info("Starting journal read loop")

	retryCount := 0
	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Journal read loop stopped")
			return
		default:
		}

		// Read next journal entry
		n, err := c.journal.Next()
		if err != nil {
			c.handleJournalError(err, &retryCount)
			continue
		}

		if n == 0 {
			// No new entries, wait a bit
			time.Sleep(time.Millisecond * 100)
			continue
		}

		// Reset retry count on successful read
		retryCount = 0

		// Process the journal entry
		if err := c.processJournalEntry(); err != nil {
			c.logger.Warn("Failed to process journal entry", zap.Error(err))
			if c.errorsTotal != nil {
				c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "entry_processing_failed"),
				))
			}
			c.stats.ErrorsTotal++
		}
	}
}

// processJournalEntry processes a single journal entry
func (c *Collector) processJournalEntry() error {
	ctx, span := c.tracer.Start(c.ctx, "systemd-api.process_entry")
	defer span.End()

	start := time.Now()

	// Extract journal entry data
	entry, err := c.extractJournalEntry()
	if err != nil {
		span.SetAttributes(attribute.String("error", "entry_extraction_failed"))
		return fmt.Errorf("failed to extract journal entry: %w", err)
	}

	// Apply priority filtering
	if !c.config.ShouldIncludePriority(sdjournal.Priority(entry.Priority)) {
		span.SetAttributes(attribute.String("skipped", "priority_filtered"))
		return nil
	}

	// Apply rate limiting
	if c.rateLimiter != nil && !c.rateLimiter.Allow() {
		span.SetAttributes(attribute.String("skipped", "rate_limited"))
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "rate_limited"),
			))
		}
		c.stats.EntriesDropped++
		return nil
	}

	// Create systemd event data
	eventData := &SystemdEventData{
		EventType:    entry.GetEventType(),
		Source:       "journal",
		JournalEntry: entry,
		UnitName:     entry.Unit,
	}

	// Convert to CollectorEvent
	collectorEvent := eventData.ToCollectorEvent(c.name)

	// Set span attributes
	span.SetAttributes(
		attribute.String("unit", entry.Unit),
		attribute.String("message", entry.Message),
		attribute.Int("priority", entry.Priority),
		attribute.String("event_type", string(eventData.EventType)),
	)

	// Send event
	select {
	case c.events <- collectorEvent:
		// Record success metrics
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("unit", entry.Unit),
				attribute.String("event_type", string(eventData.EventType)),
			))
		}

		// Record processing time
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if c.processingTime != nil {
			c.processingTime.Record(ctx, duration, metric.WithAttributes(
				attribute.String("unit", entry.Unit),
			))
		}

		// Update statistics
		c.stats.EntriesProcessed++
		c.stats.LastActivity = time.Now()
		c.stats.ProcessingLatency = duration

	case <-c.ctx.Done():
		return nil

	default:
		// Buffer full - drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "buffer_full"),
			))
		}
		span.SetAttributes(attribute.String("dropped", "buffer_full"))
		c.stats.EntriesDropped++
		c.logger.Warn("Event buffer full, dropping journal entry",
			zap.String("unit", entry.Unit),
			zap.String("message", entry.Message))
	}

	return nil
}

// extractJournalEntry extracts data from the current journal entry
func (c *Collector) extractJournalEntry() (*JournalEntry, error) {
	entry := &JournalEntry{
		Fields:    make(map[string]string),
		ExtraData: make(map[string]string),
	}

	// Get all journal data
	data, err := c.journal.GetData()
	if err != nil {
		return nil, fmt.Errorf("failed to get journal data: %w", err)
	}

	// Extract configured fields
	for _, field := range c.config.Fields {
		if value, exists := data[field]; exists {
			stringValue := fmt.Sprintf("%v", value)
			entry.Fields[field] = stringValue
			entry.ExtraData[field] = stringValue

			// Map to structured fields
			switch field {
			case "MESSAGE":
				entry.Message = stringValue
			case "PRIORITY":
				if priority, err := strconv.Atoi(stringValue); err == nil {
					entry.Priority = priority
				}
			case "_PID":
				if pid, err := strconv.ParseInt(stringValue, 10, 32); err == nil {
					entry.PID = int32(pid)
				}
			case "_COMM":
				entry.Command = stringValue
			case "_SYSTEMD_UNIT":
				entry.Unit = stringValue
			case "_HOSTNAME":
				entry.Hostname = stringValue
			case "_MACHINE_ID":
				entry.MachineID = stringValue
			case "_BOOT_ID":
				entry.BootID = stringValue
			case "_TRANSPORT":
				entry.Transport = stringValue
			case "SYSLOG_IDENTIFIER":
				entry.SyslogID = stringValue
			case "_SYSTEMD_CGROUP":
				entry.CgroupPath = stringValue
			case "_SOURCE_REALTIME_TIMESTAMP":
				if timestamp, err := strconv.ParseInt(stringValue, 10, 64); err == nil {
					entry.Timestamp = time.Unix(0, timestamp*1000) // Convert microseconds to nanoseconds
				}
			}
		}
	}

	// Set timestamp if not already set
	if entry.Timestamp.IsZero() {
		timestamp, err := c.journal.GetRealtimeUsec()
		if err == nil {
			entry.Timestamp = time.Unix(0, int64(timestamp)*1000)
		} else {
			entry.Timestamp = time.Now()
		}
	}

	// Extract container ID from cgroup if available
	if entry.CgroupPath != "" {
		if containerID := extractContainerIDFromCgroup(entry.CgroupPath); containerID != "" {
			entry.ContainerID = containerID
		}
	}

	return entry, nil
}

// handleJournalError handles journal reading errors with retry logic
func (c *Collector) handleJournalError(err error, retryCount *int) {
	*retryCount++

	if c.errorsTotal != nil {
		c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "journal_read_failed"),
		))
	}

	if c.connectionRetries != nil {
		c.connectionRetries.Add(c.ctx, 1)
	}

	c.stats.ErrorsTotal++
	c.stats.ConnectionRetries++

	c.logger.Error("Journal read error",
		zap.Error(err),
		zap.Int("retry_count", *retryCount),
		zap.Int("max_retries", c.config.MaxRetries),
	)

	if *retryCount >= c.config.MaxRetries {
		c.logger.Error("Max retries exceeded, marking unhealthy")
		c.mu.Lock()
		c.healthy = false
		c.mu.Unlock()
		return
	}

	// Wait before retry
	time.Sleep(c.config.RetryDelay)

	// Try to reinitialize journal
	if c.journal != nil {
		c.journal.Close()
	}

	if err := c.initJournal(); err != nil {
		c.logger.Error("Failed to reinitialize journal", zap.Error(err))
	} else {
		c.logger.Info("Journal reinitialized successfully")
	}
}

// healthMonitorLoop monitors collector health and updates metrics
func (c *Collector) healthMonitorLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.updateHealthMetrics()
		}
	}
}

// updateHealthMetrics updates health and performance metrics
func (c *Collector) updateHealthMetrics() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Update buffer utilization
	bufferUtil := float64(len(c.events)) / float64(cap(c.events))
	c.stats.BufferUtilization = bufferUtil

	if c.bufferUsage != nil {
		c.bufferUsage.Record(context.Background(), int64(len(c.events)))
	}

	// Update uptime
	c.stats.UptimeSeconds = int64(time.Since(c.startTime).Seconds())

	// Update journal position if available
	if c.journal != nil {
		if cursor, err := c.journal.GetCursor(); err == nil {
			// Use cursor hash as position approximation
			position := int64(hashString(cursor))
			c.stats.JournalPosition = uint64(position)
			if c.journalPosition != nil {
				c.journalPosition.Record(context.Background(), position)
			}
		}
	}

	// Update connection status
	connected := c.healthy && c.journal != nil
	if c.journalConnected != nil {
		var connectedValue int64
		if connected {
			connectedValue = 1
		}
		c.journalConnected.Record(context.Background(), connectedValue)
	}
}

// Helper function to hash strings for position tracking
func hashString(s string) uint32 {
	hash := uint32(5381)
	for _, c := range s {
		hash = ((hash << 5) + hash) + uint32(c)
	}
	return hash
}

// Statistics returns collector statistics
func (c *Collector) Statistics() *CollectorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create a copy to avoid race conditions
	statsCopy := *c.stats
	return &statsCopy
}
