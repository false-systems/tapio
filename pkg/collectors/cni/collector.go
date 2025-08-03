package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// PodInfo contains extracted K8s pod information
type PodInfo struct {
	Namespace string
	PodName   string
	PodUID    string
}

// Collector implements minimal CNI monitoring
type Collector struct {
	name      string
	events    chan collectors.RawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	ebpfState interface{} // Platform-specific eBPF state
}

// NewCollector creates a new minimal CNI collector
func NewCollector(name string) (*Collector, error) {
	return &Collector{
		name:    name,
		events:  make(chan collectors.RawEvent, 1000),
		healthy: true,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF monitoring if available
	if err := c.startEBPF(); err != nil {
		// Log but don't fail - eBPF is optional
		// In minimal collector, we just collect what we can
	}

	return nil
}

// Stop gracefully shuts down
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Close events channel
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

// Helper to create a CNI raw event
func (c *Collector) createEvent(eventType string, data interface{}) collectors.RawEvent {
	jsonData, _ := json.Marshal(data)

	metadata := map[string]string{
		"collector": c.name,
		"event":     eventType,
	}

	// Extract K8s metadata from CNI data
	if cniData, ok := data.(map[string]interface{}); ok {
		// Parse pod/namespace from netns path if available
		// K8s netns format: /var/run/netns/cni-<UUID> or contains pod info
		if netnsPath, ok := cniData["data"].(string); ok {
			if podInfo := c.parseK8sFromNetns(netnsPath); podInfo != nil {
				metadata["k8s_kind"] = "Pod"
				metadata["k8s_uid"] = podInfo.PodUID
				// Only set if we have values
				if podInfo.Namespace != "" {
					metadata["k8s_namespace"] = podInfo.Namespace
				}
				if podInfo.PodName != "" {
					metadata["k8s_name"] = podInfo.PodName
				}
				// Note: Full pod details would need to be resolved via K8s API
				// or from a shared pod info cache maintained by other collectors
			}
		}
	}

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "cni",
		Data:      jsonData,
		Metadata:  metadata,
		// Generate new trace ID for CNI events
		TraceID: collectors.GenerateTraceID(),
		SpanID:  collectors.GenerateSpanID(),
	}
}

// parseK8sFromNetns extracts K8s pod information from network namespace path
func (c *Collector) parseK8sFromNetns(netnsPath string) *PodInfo {
	// Common patterns for K8s network namespaces:
	// 1. /var/run/netns/cni-<uuid>
	// 2. /proc/<pid>/ns/net where pid belongs to a container
	// 3. May contain pod UID in the path
	
	// Try to extract pod UID from CNI netns naming
	cniPattern := regexp.MustCompile(`cni-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	if matches := cniPattern.FindStringSubmatch(netnsPath); len(matches) > 1 {
		return &PodInfo{
			PodUID: matches[1],
			// Namespace and pod name would need to be resolved via K8s API
			// or from additional context (e.g., eBPF maps)
		}
	}

	// Try to extract from containerd/docker paths
	if strings.Contains(netnsPath, "kubepods") {
		// Extract pod UID from cgroup path pattern
		// /kubepods/besteffort/pod<UID>/...
		podPattern := regexp.MustCompile(`pod([0-9a-f]{8}_[0-9a-f]{4}_[0-9a-f]{4}_[0-9a-f]{4}_[0-9a-f]{12})`)
		if matches := podPattern.FindStringSubmatch(netnsPath); len(matches) > 1 {
			// Convert underscore format to hyphen format
			podUID := strings.ReplaceAll(matches[1], "_", "-")
			return &PodInfo{
				PodUID: podUID,
			}
		}
	}

	// If we can't parse pod info, return nil
	return nil
}
