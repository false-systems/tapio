package network

import (
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// NetworkCollectorConfig configuration for network collector
type NetworkCollectorConfig struct {
	BufferSize         int           `json:"buffer_size"`
	FlushInterval      time.Duration `json:"flush_interval"`
	EnableIPv4         bool          `json:"enable_ipv4"`
	EnableTCP          bool          `json:"enable_tcp"`
	EnableUDP          bool          `json:"enable_udp"`
	EnableHTTP         bool          `json:"enable_http"`
	EnableHTTPS        bool          `json:"enable_https"`
	HTTPPorts          []int         `json:"http_ports"`
	HTTPSPorts         []int         `json:"https_ports"`
	MaxEventsPerSecond int           `json:"max_events_per_second"`
	SamplingRate       float64       `json:"sampling_rate"`
}

// EventProcessor interface for processing events
type EventProcessor interface {
	Process(ctx interface{}, event *domain.CollectorEvent) error
}

// Base Collector for embedding
type Collector struct {
	name           string
	logger         interface{} // Use interface{} to avoid import issues
	events         chan *domain.CollectorEvent
	ctx            interface{}
	wg             *sync.WaitGroup
	mutex          sync.RWMutex
	eventProcessor EventProcessor
}

// NewCollector creates a new base network collector
func NewCollector(name string, config *NetworkCollectorConfig, logger interface{}) (*Collector, error) {
	if config == nil {
		config = &NetworkCollectorConfig{
			BufferSize: 1000,
			EnableIPv4: true,
			EnableTCP:  true,
			EnableUDP:  true,
		}
	}

	return &Collector{
		name:   name,
		logger: logger,
		events: make(chan *domain.CollectorEvent, config.BufferSize),
		wg:     &sync.WaitGroup{},
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the collector
func (c *Collector) Start(ctx interface{}) error {
	c.ctx = ctx
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	if c.events != nil {
		close(c.events)
	}
	return nil
}

// Events returns the events channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return true
}
