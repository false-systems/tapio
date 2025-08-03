package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// NATSConfig holds all NATS-related configuration
type NATSConfig struct {
	// Connection
	URL               string
	Name              string
	MaxReconnects     int
	ReconnectWait     time.Duration
	ConnectionTimeout time.Duration

	// JetStream
	JetStreamEnabled bool

	// Streams
	TracesStreamName    string
	TracesSubjects      []string
	RawEventsStreamName string
	RawEventsSubjects   []string

	// Stream settings
	MaxAge          time.Duration
	MaxBytes        int64
	Storage         string
	Replicas        int
	DuplicateWindow time.Duration

	// Consumer settings
	ConsumerName  string
	DeliverPolicy string
	AckPolicy     string
	AckWait       time.Duration
	MaxDeliver    int
	ReplayPolicy  string

	// Subscription settings
	QueueGroup   string
	MaxPending   int
	WorkerCount  int
	BatchSize    int
	FetchTimeout time.Duration
}

// DefaultNATSConfig returns production-ready defaults
func DefaultNATSConfig() *NATSConfig {
	return &NATSConfig{
		// Connection defaults
		URL:               getEnv("NATS_URL", "nats://localhost:4222"),
		Name:              getEnv("NATS_CLIENT_NAME", "tapio-client"),
		MaxReconnects:     getEnvInt("NATS_MAX_RECONNECTS", 10),
		ReconnectWait:     getEnvDuration("NATS_RECONNECT_WAIT", "1s"),
		ConnectionTimeout: getEnvDuration("NATS_CONNECTION_TIMEOUT", "5s"),

		// JetStream
		JetStreamEnabled: getEnvBool("NATS_JETSTREAM_ENABLED", true),

		// Streams
		TracesStreamName:    getEnv("NATS_TRACES_STREAM", "TRACES"),
		TracesSubjects:      []string{getEnv("NATS_TRACES_SUBJECT", "traces.>")},
		RawEventsStreamName: getEnv("NATS_RAW_STREAM", "RAW_EVENTS"),
		RawEventsSubjects:   []string{getEnv("NATS_RAW_SUBJECT", "raw.>")},

		// Stream settings
		MaxAge:          getEnvDuration("NATS_STREAM_MAX_AGE", "24h"),
		MaxBytes:        getEnvInt64("NATS_STREAM_MAX_BYTES", 10*1024*1024*1024), // 10GB
		Storage:         getEnv("NATS_STREAM_STORAGE", "file"),
		Replicas:        getEnvInt("NATS_STREAM_REPLICAS", 1),
		DuplicateWindow: getEnvDuration("NATS_DUPLICATE_WINDOW", "2m"),

		// Consumer defaults
		ConsumerName:  getEnv("NATS_CONSUMER_NAME", "tapio-consumer"),
		DeliverPolicy: getEnv("NATS_DELIVER_POLICY", "all"),
		AckPolicy:     getEnv("NATS_ACK_POLICY", "explicit"),
		AckWait:       getEnvDuration("NATS_ACK_WAIT", "30s"),
		MaxDeliver:    getEnvInt("NATS_MAX_DELIVER", 3),
		ReplayPolicy:  getEnv("NATS_REPLAY_POLICY", "instant"),

		// Subscription defaults
		QueueGroup:   getEnv("NATS_QUEUE_GROUP", "tapio"),
		MaxPending:   getEnvInt("NATS_MAX_PENDING", 1000),
		WorkerCount:  getEnvInt("NATS_WORKER_COUNT", 10),
		BatchSize:    getEnvInt("NATS_BATCH_SIZE", 10),
		FetchTimeout: getEnvDuration("NATS_FETCH_TIMEOUT", "1s"),
	}
}

// InClusterNATSConfig returns configuration for in-cluster deployment
func InClusterNATSConfig() *NATSConfig {
	config := DefaultNATSConfig()
	config.URL = getEnv("NATS_URL", "nats://nats.tapio.svc.cluster.local:4222")
	config.Replicas = getEnvInt("NATS_STREAM_REPLICAS", 3)
	return config
}

// GetTracesSubject returns the primary traces subject
func (c *NATSConfig) GetTracesSubject() string {
	if len(c.TracesSubjects) > 0 {
		return c.TracesSubjects[0]
	}
	return "traces.>"
}

// GetRawEventsSubject returns the primary raw events subject
func (c *NATSConfig) GetRawEventsSubject() string {
	if len(c.RawEventsSubjects) > 0 {
		return c.RawEventsSubjects[0]
	}
	return "raw.>"
}

// Validate checks if the configuration is valid
func (c *NATSConfig) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("NATS URL cannot be empty")
	}
	if c.TracesStreamName == "" {
		return fmt.Errorf("traces stream name cannot be empty")
	}
	if len(c.TracesSubjects) == 0 {
		return fmt.Errorf("traces subjects cannot be empty")
	}
	if c.MaxAge <= 0 {
		return fmt.Errorf("max age must be positive")
	}
	if c.MaxBytes <= 0 {
		return fmt.Errorf("max bytes must be positive")
	}
	return nil
}

// Helper functions for environment variable parsing
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue string) time.Duration {
	value := getEnv(key, defaultValue)
	if duration, err := time.ParseDuration(value); err == nil {
		return duration
	}
	// If parsing fails, parse the default
	if duration, err := time.ParseDuration(defaultValue); err == nil {
		return duration
	}
	return time.Second // Fallback
}
