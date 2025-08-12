package nats

import "time"

// Connection and timeout constants
const (
	DefaultConnectTimeout = 10 * time.Second
	DefaultReconnectWait  = 2 * time.Second
	DefaultMaxReconnects  = 60
)

// Publishing configuration constants
const (
	DefaultMaxPending        = 256
	DefaultStreamMaxBytes    = 1024 * 1024 * 1024 // 1GB
	DefaultStreamMaxMessages = 10000000
	DefaultStreamMaxAge      = 7 * 24 * time.Hour
	DefaultStreamReplicas    = 1
)

// Processing and retry constants
const (
	MaxConsecutiveErrors  = 10
	BaseBackoffDelay      = time.Second
	MaxBackoffDelay       = 30 * time.Second
	BackoffMultiplier     = 2.0
	ProcessingTimeout     = 20 * time.Second
	MaxBatchSize          = 1000
	CleanupTimeout        = 30 * time.Second
	AsyncCompleteTimeout  = 5 * time.Second
	MetricsReportInterval = 1 * time.Minute
	RetryShortDelay       = time.Second
)

// Default stream and subject names
const (
	DefaultStreamName   = "TAPIO_EVENTS"
	DefaultEventsPrefix = "events"
	DefaultTracesPrefix = "traces"
)
