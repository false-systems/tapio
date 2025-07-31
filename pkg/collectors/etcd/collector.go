package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Collector implements minimal etcd collection
type Collector struct {
	config collectors.CollectorConfig
	events chan collectors.RawEvent

	// etcd client and monitoring
	etcdClient *clientv3.Client
	watcher    clientv3.Watcher

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	healthy bool
}

// NewCollector creates a new etcd collector
func NewCollector(config collectors.CollectorConfig) (collectors.Collector, error) {
	// Try eBPF collector first (Linux only)
	if ebpfCollector, err := NewEBPFCollector(config); err == nil {
		return ebpfCollector, nil
	}

	// Fallback to basic etcd client collector
	return NewBasicCollector(config)
}

// NewBasicCollector creates a new basic etcd collector
func NewBasicCollector(config collectors.CollectorConfig) (*Collector, error) {
	// Create etcd client
	etcdConfig := clientv3.Config{
		Endpoints:   []string{"localhost:2379"}, // Default etcd endpoint
		DialTimeout: 5 * time.Second,
		Username:    config.Labels["etcd_username"],
		Password:    config.Labels["etcd_password"],
	}

	// Override endpoints if specified in config
	if endpoints, ok := config.Labels["etcd_endpoints"]; ok {
		etcdConfig.Endpoints = []string{endpoints}
	}

	cli, err := clientv3.New(etcdConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return &Collector{
		config:     config,
		events:     make(chan collectors.RawEvent, config.BufferSize),
		etcdClient: cli,
		healthy:    true,
	}, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return "etcd"
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Initialize source connection
	// Start collection goroutine
	c.wg.Add(1)
	go c.collect()

	return nil
}

// Stop gracefully shuts down
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Close watcher
	if c.watcher != nil {
		c.watcher.Close()
	}

	// Close etcd client
	if c.etcdClient != nil {
		c.etcdClient.Close()
	}

	c.wg.Wait()
	close(c.events)

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// collect is the main collection loop
func (c *Collector) collect() {
	defer c.wg.Done()

	// Start watching all keys for changes
	c.watcher = clientv3.NewWatcher(c.etcdClient)
	watchChan := c.watcher.Watch(c.ctx, "", clientv3.WithPrefix())

	// Also collect cluster status periodically
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return

		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				c.mu.Lock()
				c.healthy = false
				c.mu.Unlock()
				continue
			}

			// Process watch events
			for _, event := range watchResp.Events {
				c.processWatchEvent(event)
			}

		case <-ticker.C:
			// Collect cluster status
			c.collectClusterStatus()
		}
	}
}

// processWatchEvent processes an etcd watch event
func (c *Collector) processWatchEvent(event *clientv3.Event) {
	data := map[string]interface{}{
		"type":            event.Type.String(),
		"key":             string(event.Kv.Key),
		"value":           string(event.Kv.Value),
		"create_revision": event.Kv.CreateRevision,
		"mod_revision":    event.Kv.ModRevision,
		"version":         event.Kv.Version,
		"lease":           event.Kv.Lease,
	}

	// Add previous key-value if this is an update
	if event.PrevKv != nil {
		data["prev_value"] = string(event.PrevKv.Value)
		data["prev_mod_revision"] = event.PrevKv.ModRevision
	}

	jsonData, _ := json.Marshal(data)

	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      fmt.Sprintf("etcd.%s", event.Type.String()),
		Data:      jsonData,
		Metadata:  c.createEventMetadata("watch", string(event.Kv.Key)),
	}

	select {
	case c.events <- rawEvent:
	case <-c.ctx.Done():
		return
	default:
		// Buffer full, drop event
	}
}

// collectClusterStatus collects etcd cluster status
func (c *Collector) collectClusterStatus() {
	// Get cluster status
	status, err := c.etcdClient.Status(c.ctx, c.etcdClient.Endpoints()[0])
	if err != nil {
		c.mu.Lock()
		c.healthy = false
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	c.healthy = true
	c.mu.Unlock()

	data := map[string]interface{}{
		"version":            status.Version,
		"db_size":            status.DbSize,
		"leader":             status.Leader,
		"raft_index":         status.RaftIndex,
		"raft_term":          status.RaftTerm,
		"raft_applied_index": status.RaftAppliedIndex,
	}

	jsonData, _ := json.Marshal(data)

	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "etcd.status",
		Data:      jsonData,
		Metadata:  c.createEventMetadata("status", "cluster"),
	}

	select {
	case c.events <- rawEvent:
	case <-c.ctx.Done():
		return
	default:
		// Buffer full, drop event
	}
}

// createEventMetadata creates event metadata
func (c *Collector) createEventMetadata(source, key string) map[string]string {
	metadata := map[string]string{
		"collector": "etcd",
		"source":    source,
		"key":       key,
	}

	// Add config labels
	for k, v := range c.config.Labels {
		metadata[k] = v
	}

	return metadata
}
