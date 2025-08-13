package dns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Config for DNS collector with comprehensive settings
type Config struct {
	// Basic settings
	Name         string
	BufferSize   int
	Interface    string
	EnableEBPF   bool
	EnableSocket bool

	// DNS specific
	DNSPort   uint16
	Protocols []string // ["udp", "tcp"]

	// Rate limiting
	RateLimitEnabled bool
	RateLimitRPS     float64
	RateLimitBurst   int

	// Cache settings
	CacheEnabled bool
	CacheSize    int
	CacheTTL     time.Duration

	// Performance
	WorkerCount        int
	BatchSize          int
	FlushInterval      time.Duration
	SlowQueryThreshold time.Duration

	// Logging
	Logger *zap.Logger
}

// DefaultConfig returns sensible defaults with production settings
func DefaultConfig() Config {
	return Config{
		Name:               "dns",
		BufferSize:         10000,
		Interface:          "eth0",
		EnableEBPF:         true,
		EnableSocket:       false, // socket filter needs special privileges
		DNSPort:            53,
		Protocols:          []string{"udp", "tcp"},
		RateLimitEnabled:   true,
		RateLimitRPS:       1000.0, // 1000 queries per second
		RateLimitBurst:     2000,
		CacheEnabled:       true,
		CacheSize:          10000,
		CacheTTL:           5 * time.Minute,
		WorkerCount:        4,
		BatchSize:          100,
		FlushInterval:      100 * time.Millisecond,
		SlowQueryThreshold: 100 * time.Millisecond,
	}
}

// IPv4Address represents IPv4 address for eBPF compatibility
type IPv4Address struct {
	Addr uint32
}

// IPv6Address represents IPv6 address for eBPF compatibility
type IPv6Address struct {
	Addr [4]uint32
}

// AddressUnion represents the union from eBPF struct
type AddressUnion struct {
	IPv4 IPv4Address
	IPv6 IPv6Address
}

// EnhancedDNSEvent represents the enhanced DNS event from eBPF - must match C struct exactly
type EnhancedDNSEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64
	EventType uint8
	Protocol  uint8
	IPVersion uint8
	Pad1      uint8
	SrcAddr   AddressUnion
	DstAddr   AddressUnion
	SrcPort   uint16
	DstPort   uint16
	DNSID     uint16
	DNSFlags  uint16
	DNSOpcode uint8
	DNSRcode  uint8
	DNSQtype  uint16
	DataLen   uint32
	LatencyNs uint32
	QueryName [128]byte // Increased size
	Data      [512]byte // Increased size
}

// DNSCache holds cached DNS responses with TTL
type DNSCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	maxSize int
	ttl     time.Duration
}

type CacheEntry struct {
	Value     interface{}
	Expires   time.Time
	HitCount  int64
	CreatedAt time.Time
}

// DNSStats holds collector statistics
type DNSStats struct {
	QueriesTotal   int64
	ResponsesTotal int64
	TimeoutsTotal  int64
	ErrorsTotal    int64
	CacheHits      int64
	CacheMisses    int64
	ActiveQueries  int64
	PacketsDropped int64
	LastQueryTime  time.Time
	LatencySum     int64 // in nanoseconds
	LatencyCount   int64
	SlowQueries    int64 // queries > threshold
}

// Collector implements DNS monitoring via eBPF with comprehensive observability
type Collector struct {
	// Core
	name       string
	logger     *zap.Logger
	config     Config
	ctx        context.Context
	cancel     context.CancelFunc
	healthy    bool
	stopped    bool
	mu         sync.RWMutex
	safeParser *collectors.SafeParser

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Event processing
	events   chan collectors.RawEvent
	workerWg sync.WaitGroup

	// Rate limiting
	rlimiter *rate.Limiter

	// Cache
	cache *DNSCache

	// Statistics
	stats DNSStats

	// Active queries tracking
	activeQueries sync.Map // queryID -> startTime

	// OpenTelemetry
	tracer             trace.Tracer
	meter              metric.Meter
	queriesTotal       metric.Int64Counter
	queryLatency       metric.Float64Histogram
	errorsTotal        metric.Int64Counter
	activeQueriesGauge metric.Int64UpDownCounter
	cacheHitsTotal     metric.Int64Counter
	cacheMissTotal     metric.Int64Counter
	slowQueriesTotal   metric.Int64Counter
	packetsDropped     metric.Int64Counter
}

// NewCollector creates a new DNS collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	// Initialize logger if not provided
	logger := cfg.Logger
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize OTEL components - MANDATORY pattern
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	queriesTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total DNS queries processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create queries counter", zap.Error(err))
	}

	queryLatency, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create latency histogram", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	activeQueriesGauge, err := meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_connections", name),
		metric.WithDescription(fmt.Sprintf("Active DNS queries in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create active queries gauge", zap.Error(err))
	}

	cacheHitsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_cache_hits_total", name),
		metric.WithDescription(fmt.Sprintf("Total cache hits in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create cache hits counter", zap.Error(err))
	}

	cacheMissTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_cache_misses_total", name),
		metric.WithDescription(fmt.Sprintf("Total cache misses in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create cache miss counter", zap.Error(err))
	}

	slowQueriesTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_slow_queries_total", name),
		metric.WithDescription(fmt.Sprintf("Total slow queries in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create slow queries counter", zap.Error(err))
	}

	packetsDropped, err := meter.Int64Counter(
		fmt.Sprintf("%s_packets_dropped_total", name),
		metric.WithDescription(fmt.Sprintf("Total packets dropped in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create packets dropped counter", zap.Error(err))
	}

	// Initialize rate limiter if enabled
	var rlimiter *rate.Limiter
	if cfg.RateLimitEnabled {
		rlimiter = rate.NewLimiter(rate.Limit(cfg.RateLimitRPS), cfg.RateLimitBurst)
	}

	// Initialize cache if enabled
	var cache *DNSCache
	if cfg.CacheEnabled {
		cache = &DNSCache{
			entries: make(map[string]*CacheEntry),
			maxSize: cfg.CacheSize,
			ttl:     cfg.CacheTTL,
		}
	}

	return &Collector{
		name:               name,
		logger:             logger,
		config:             cfg,
		events:             make(chan collectors.RawEvent, cfg.BufferSize),
		safeParser:         collectors.NewSafeParser(),
		rlimiter:           rlimiter,
		cache:              cache,
		tracer:             tracer,
		meter:              meter,
		queriesTotal:       queriesTotal,
		queryLatency:       queryLatency,
		errorsTotal:        errorsTotal,
		activeQueriesGauge: activeQueriesGauge,
		cacheHitsTotal:     cacheHitsTotal,
		cacheMissTotal:     cacheMissTotal,
		slowQueriesTotal:   slowQueriesTotal,
		packetsDropped:     packetsDropped,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "dns.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	if !c.config.EnableEBPF {
		c.healthy = true
		span.SetStatus(codes.Ok, "started without eBPF")
		return nil
	}

	// Start eBPF monitoring using platform-specific implementation
	if err := c.startEBPF(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_start_failed"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processing loop
	go c.readEBPFEvents()

	c.healthy = true
	span.SetStatus(codes.Ok, "DNS collector started successfully")
	c.logger.Info("DNS collector started",
		zap.String("name", c.name),
		zap.Bool("ebpf_enabled", c.config.EnableEBPF),
		zap.Int("buffer_size", c.config.BufferSize),
	)
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	// Prevent multiple stops
	if c.stopped {
		return nil
	}
	c.stopped = true

	if c.cancel != nil {
		c.cancel()
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Close events channel only once
	if c.events != nil {
		close(c.events)
	}
	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}




// NewDNSCache creates a new DNS cache with specified size and TTL
func NewDNSCache(maxSize int, ttl time.Duration) *DNSCache {
	return &DNSCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// CacheGet retrieves a value from the cache
func (c *Collector) CacheGet(key string) (interface{}, bool) {
	if c.cache == nil {
		return nil, false
	}

	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	entry, exists := c.cache.entries[key]
	if !exists {
		c.cacheMissTotal.Add(context.Background(), 1)
		return nil, false
	}

	// Check if expired
	if time.Now().After(entry.Expires) {
		c.cacheMissTotal.Add(context.Background(), 1)
		return nil, false
	}

	entry.HitCount++
	c.cacheHitsTotal.Add(context.Background(), 1)
	return entry.Value, true
}

// CacheSet stores a value in the cache with the specified TTL
func (c *Collector) CacheSet(key string, value interface{}, ttl time.Duration) {
	if c.cache == nil {
		return
	}

	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	// Check if we need to evict entries
	if len(c.cache.entries) >= c.cache.maxSize {
		// Simple eviction: remove the oldest entry
		var oldestKey string
		var oldestTime time.Time
		for k, v := range c.cache.entries {
			if oldestKey == "" || v.CreatedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.CreatedAt
			}
		}
		if oldestKey != "" {
			delete(c.cache.entries, oldestKey)
		}
	}

	c.cache.entries[key] = &CacheEntry{
		Value:     value,
		Expires:   time.Now().Add(ttl),
		HitCount:  0,
		CreatedAt: time.Now(),
	}
}
