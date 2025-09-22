package link

import "time"

// Event types (must match BPF code)
const (
	EventSYNTimeout    = 1
	EventConnectionRST = 2
	EventARPTimeout    = 3
)

// LinkEvent represents a link failure event from BPF
type LinkEvent struct {
	Timestamp uint64
	PID       uint32
	SrcIP     uint32
	DstIP     uint32
	SrcPort   uint16
	DstPort   uint16
	EventType uint8
	Protocol  uint8
	Padding   uint16
	Comm      [16]byte
}

// Config defines link observer configuration
type Config struct {
	Enabled       bool          `yaml:"enabled"`
	BufferSize    int           `yaml:"buffer_size"`
	SampleRate    float64       `yaml:"sample_rate"`
	FlushInterval time.Duration `yaml:"flush_interval"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:       true,
		BufferSize:    10000,
		SampleRate:    1.0,
		FlushInterval: 30 * time.Second,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		c.BufferSize = 10000
	}
	if c.SampleRate < 0 || c.SampleRate > 1 {
		c.SampleRate = 1.0
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = 30 * time.Second
	}
	return nil
}

// FailureStats tracks failure statistics
type FailureStats struct {
	SYNTimeouts    uint64
	ConnectionRSTs uint64
	ARPTimeouts    uint64
	LastSeen       time.Time
}

// GetEventTypeName returns human-readable event type
func GetEventTypeName(eventType uint8) string {
	switch eventType {
	case EventSYNTimeout:
		return "syn_timeout"
	case EventConnectionRST:
		return "connection_reset"
	case EventARPTimeout:
		return "arp_timeout"
	default:
		return "unknown"
	}
}
