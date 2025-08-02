package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Config holds etcd collector configuration
type Config struct {
	Endpoints []string   `json:"endpoints"`
	Username  string     `json:"username"`
	Password  string     `json:"password"`
	TLS       *TLSConfig `json:"tls"`
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"`
}

// Collector implements minimal etcd monitoring
type Collector struct {
	name        string
	config      Config
	client      *clientv3.Client
	events      chan collectors.RawEvent
	ctx         context.Context
	cancel      context.CancelFunc
	healthy     bool
	mu          sync.RWMutex
	ebpfState   interface{} // Platform-specific eBPF state
	stats       CollectorStats
}

// CollectorStats tracks collector statistics
type CollectorStats struct {
	EventsCollected uint64
	EventsDropped   uint64
	ErrorCount      uint64
	LastEventTime   time.Time
}

// NewCollector creates a new minimal etcd collector
func NewCollector(name string, config Config) (*Collector, error) {
	if len(config.Endpoints) == 0 {
		config.Endpoints = []string{"localhost:2379"}
	}

	return &Collector{
		name:    name,
		config:  config,
		events:  make(chan collectors.RawEvent, 10000), // Large buffer
		healthy: true,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Create etcd client
	clientConfig := clientv3.Config{
		Endpoints:   c.config.Endpoints,
		DialTimeout: 5 * time.Second,
	}

	if c.config.Username != "" {
		clientConfig.Username = c.config.Username
		clientConfig.Password = c.config.Password
	}

	// TODO: Add TLS configuration if needed

	client, err := clientv3.New(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}
	c.client = client

	// Test connection
	ctxTimeout, cancel := context.WithTimeout(c.ctx, 3*time.Second)
	_, err = c.client.Status(ctxTimeout, c.config.Endpoints[0])
	cancel()
	if err != nil {
		c.client.Close()
		c.client = nil
		return fmt.Errorf("failed to connect to etcd: %w", err)
	}

	// Ready to start processing

	// Start watching K8s registry
	go c.watchRegistry()

	// Start eBPF monitoring if available
	if err := c.startEBPF(); err != nil {
		// Log but don't fail - eBPF is optional
		// In minimal collector, we just collect what we can
	}

	c.healthy = true
	return nil
}

// Stop gracefully shuts down
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Close etcd client
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Cleanup complete

	// Close events channel
	if c.events != nil {
		close(c.events)
		c.events = nil
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

// watchRegistry watches the K8s registry prefix in etcd
func (c *Collector) watchRegistry() {
	// Watch K8s registry prefix
	watchChan := c.client.Watch(c.ctx, "/registry/", clientv3.WithPrefix())

	for {
		select {
		case <-c.ctx.Done():
			return
		case watchResp, ok := <-watchChan:
			if !ok {
				return
			}

			if watchResp.Err() != nil {
				c.mu.Lock()
				c.stats.ErrorCount++
				c.mu.Unlock()
				continue
			}

			for _, event := range watchResp.Events {
				c.processEtcdEvent(event)
			}
		}
	}
}

// processEtcdEvent processes an etcd watch event
func (c *Collector) processEtcdEvent(event *clientv3.Event) {
	// Extract resource type from key
	key := string(event.Kv.Key)
	resourceType := c.extractResourceType(key)

	// Create event data with raw etcd information
	eventData := map[string]interface{}{
		"key":             key,
		"value":           string(event.Kv.Value),
		"mod_revision":    event.Kv.ModRevision,
		"create_revision": event.Kv.CreateRevision,
		"version":         event.Kv.Version,
		"resource_type":   resourceType,
	}

	// Determine operation type
	var operation string
	switch event.Type {
	case clientv3.EventTypePut:
		operation = "PUT"
	case clientv3.EventTypeDelete:
		operation = "DELETE"
	default:
		operation = "UNKNOWN"
	}

	rawEvent := c.createEvent(operation, eventData)
	rawEvent.Metadata["resource_type"] = resourceType
	rawEvent.Metadata["operation"] = operation

	// Send event
	select {
	case c.events <- rawEvent:
		c.mu.Lock()
		c.stats.EventsCollected++
		c.stats.LastEventTime = time.Now()
		c.mu.Unlock()
	case <-c.ctx.Done():
		return
	default:
		// Buffer full, drop event
		c.mu.Lock()
		c.stats.EventsDropped++
		c.mu.Unlock()
	}
}

// extractResourceType extracts the K8s resource type from etcd key
func (c *Collector) extractResourceType(key string) string {
	// Expected format: /registry/{resource}/{namespace}/{name}
	// or /registry/{resource}/{name} for cluster-scoped resources
	parts := strings.Split(key, "/")
	if len(parts) >= 3 && parts[1] == "registry" {
		return parts[2]
	}
	return "unknown"
}

// Helper to create an etcd raw event
func (c *Collector) createEvent(eventType string, data interface{}) collectors.RawEvent {
	jsonData, _ := json.Marshal(data)

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "etcd",
		Data:      jsonData,
		Metadata: map[string]string{
			"collector": c.name,
			"event":     eventType,
		},
		// Generate new trace ID for etcd events
		// TODO: Extract from gRPC headers when frame parsing is implemented
		TraceID: collectors.GenerateTraceID(),
		SpanID:  collectors.GenerateSpanID(),
	}
}

// Health returns detailed health information
func (c *Collector) Health() (bool, map[string]interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	health := map[string]interface{}{
		"healthy":          c.healthy,
		"events_collected": c.stats.EventsCollected,
		"events_dropped":   c.stats.EventsDropped,
		"error_count":      c.stats.ErrorCount,
		"last_event":       c.stats.LastEventTime,
		"client_connected": c.client != nil,
	}

	return c.healthy, health
}

// Statistics returns collector statistics
func (c *Collector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"events_collected": c.stats.EventsCollected,
		"events_dropped":   c.stats.EventsDropped,
		"error_count":      c.stats.ErrorCount,
		"last_event_time":  c.stats.LastEventTime,
	}

	// Ready to return stats

	return stats
}
