package kernel

import "time"

// Buffer size constants
const (
	// DefaultEventBufferSize is the default size for event channels
	DefaultEventBufferSize = 10000

	// DefaultKernelBufferKB is the default kernel events buffer size in KB
	DefaultKernelBufferKB = 512

	// DefaultProcessBufferKB is the default process events buffer size in KB
	DefaultProcessBufferKB = 256

	// DefaultNetworkBufferKB is the default network events buffer size in KB
	DefaultNetworkBufferKB = 512

	// DefaultSecurityBufferKB is the default security events buffer size in KB
	DefaultSecurityBufferKB = 256

	// KBToBytes converts kilobytes to bytes
	KBToBytes = 1024
)

// Resource limit constants
const (
	// DefaultMaxMemoryMB is the default memory limit in MB
	DefaultMaxMemoryMB = 100

	// DefaultMaxCPUPercent is the default CPU usage limit percentage
	DefaultMaxCPUPercent = 25

	// DefaultMaxEventsPerSec is the default maximum events per second
	DefaultMaxEventsPerSec = 10000

	// DefaultEventQueueSize is the default event queue size
	DefaultEventQueueSize = 10000
)

// Backpressure constants
const (
	// DefaultHighWatermark is the default high watermark for backpressure (80%)
	DefaultHighWatermark = 0.8

	// DefaultLowWatermark is the default low watermark for backpressure (60%)
	DefaultLowWatermark = 0.6

	// DefaultDropThreshold is the default threshold to start dropping events (95%)
	DefaultDropThreshold = 0.95

	// DefaultSamplingReduction is the default sampling reduction factor (50%)
	DefaultSamplingReduction = 0.5

	// DefaultBackpressureMemoryMB is the default memory threshold for backpressure
	DefaultBackpressureMemoryMB = 80
)

// Timing constants
const (
	// DefaultBatchTimeout is the default batch processing timeout
	DefaultBatchTimeout = 100 * time.Millisecond

	// DefaultRecoveryDelay is the default delay before recovery attempts
	DefaultRecoveryDelay = 5 * time.Second

	// DefaultHealthCheckInterval is the default health check interval
	DefaultHealthCheckInterval = 30 * time.Second

	// DefaultMemoryCheckInterval is the default memory check interval
	DefaultMemoryCheckInterval = 10 * time.Second

	// ProcessFallbackInterval is the interval for process fallback monitoring
	ProcessFallbackInterval = 5 * time.Second

	// NetworkFallbackInterval is the interval for network fallback monitoring
	NetworkFallbackInterval = 10 * time.Second

	// MemoryFallbackInterval is the interval for memory fallback monitoring
	MemoryFallbackInterval = 15 * time.Second
)

// Health check constants
const (
	// DefaultMaxHealthFailures is the default maximum consecutive health check failures
	DefaultMaxHealthFailures = 3

	// RecentErrorThreshold is the time threshold to consider an error as recent
	RecentErrorThreshold = 30 * time.Second

	// FallbackHealthTimeout is the timeout to consider a fallback as unhealthy
	FallbackHealthTimeout = 2 * time.Minute
)

// Sampling constants
const (
	// DefaultSamplingRate is the default sampling rate (1 in 100 events)
	DefaultSamplingRate = 100
)

// Process monitoring constants
const (
	// MaxProcessScanLimit is the maximum number of processes to scan in fallback mode
	MaxProcessScanLimit = 1000

	// MaxConnectionScanLimit is the maximum number of connections to scan
	MaxConnectionScanLimit = 10000
)

// Retry constants
const (
	// DefaultMaxRetryAttempts is the default maximum retry attempts
	DefaultMaxRetryAttempts = 3

	// DefaultRetryInitialDelay is the default initial delay for retries
	DefaultRetryInitialDelay = 500 * time.Millisecond
)
