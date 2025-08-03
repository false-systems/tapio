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

// Collector implements minimal etcd monitoring
type Collector struct {
	name      string
	config    Config
	client    *clientv3.Client
	events    chan collectors.RawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	mu        sync.RWMutex
	ebpfState interface{} // Platform-specific eBPF state
	stats     CollectorStats
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

	// Add enhanced K8s metadata - STANDARD for all collectors
	k8sMetadata := c.extractK8sMetadata(key)
	for k, v := range k8sMetadata {
		rawEvent.Metadata[k] = v
	}

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

// extractK8sMetadata extracts full K8s metadata from etcd key
func (c *Collector) extractK8sMetadata(key string) map[string]string {
	metadata := make(map[string]string)

	// Expected formats:
	// /registry/{resource}/{namespace}/{name} (namespaced)
	// /registry/{resource}/{name} (cluster-scoped)
	parts := strings.Split(key, "/")

	if len(parts) < 3 || parts[1] != "registry" {
		return metadata
	}

	// Extract resource type/kind
	resourceType := parts[2]
	metadata["k8s_kind"] = c.normalizeResourceType(resourceType)

	// Check if namespaced or cluster-scoped
	if len(parts) == 5 {
		// Namespaced resource: /registry/{resource}/{namespace}/{name}
		metadata["k8s_namespace"] = parts[3]
		metadata["k8s_name"] = parts[4]
	} else if len(parts) == 4 {
		// Cluster-scoped resource: /registry/{resource}/{name}
		metadata["k8s_name"] = parts[3]
	}

	// Note: Additional K8s metadata would need to be extracted from the value:
	// - k8s_uid: Parse from the stored K8s object
	// - k8s_labels: Extract from metadata.labels in the K8s object
	// - k8s_owner_refs: Extract from metadata.ownerReferences
	// This would require deserializing the etcd value as a K8s object

	return metadata
}

// normalizeResourceType converts etcd resource types to K8s kinds
func (c *Collector) normalizeResourceType(resourceType string) string {
	// Map common etcd resource types to K8s kinds
	switch resourceType {
	case "pods":
		return "Pod"
	case "services":
		return "Service"
	case "deployments":
		return "Deployment"
	case "replicasets":
		return "ReplicaSet"
	case "configmaps":
		return "ConfigMap"
	case "secrets":
		return "Secret"
	case "namespaces":
		return "Namespace"
	case "nodes":
		return "Node"
	case "persistentvolumes":
		return "PersistentVolume"
	case "persistentvolumeclaims":
		return "PersistentVolumeClaim"
	case "statefulsets":
		return "StatefulSet"
	case "daemonsets":
		return "DaemonSet"
	case "jobs":
		return "Job"
	case "cronjobs":
		return "CronJob"
	case "ingresses":
		return "Ingress"
	case "endpoints":
		return "Endpoints"
	case "events":
		return "Event"
	default:
		// Capitalize first letter as convention
		if len(resourceType) > 0 {
			return strings.ToUpper(resourceType[:1]) + resourceType[1:]
		}
		return resourceType
	}
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
