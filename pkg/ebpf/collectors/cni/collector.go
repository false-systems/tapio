package cni

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

// CNICollector collects network events from CNI plugins using eBPF
type CNICollector struct {
	name      string
	config    *CNICollectorConfig
	metrics   *CNIMetrics
	flowCache *FlowCache

	// eBPF programs
	networkFlowProgram     *ebpf.Program
	podTrafficProgram      *ebpf.Program
	dnsMonitorProgram      *ebpf.Program
	policyViolationProgram *ebpf.Program

	// eBPF maps
	flowMap            *ebpf.Map
	podTrafficMap      *ebpf.Map
	dnsQueriesMap      *ebpf.Map
	policyViolationMap *ebpf.Map

	// CNI plugins
	cniPlugins map[string]CNIPlugin

	// Pod tracking
	podIndex map[string]*PodInfo

	// Service mesh
	detectedMesh ServiceMeshType
	meshAnalyzer *MeshTrafficAnalyzer

	// Control
	wg          sync.WaitGroup
	stopChannel chan struct{}
	eventChan   chan interface{}

	// Thread safety
	mutex sync.RWMutex
}

// CNICollectorConfig configures the CNI collector
type CNICollectorConfig struct {
	CollectionInterval     time.Duration `json:"collection_interval"`
	CNIConfigPath          string        `json:"cni_config_path"`
	CNIBinPath             string        `json:"cni_bin_path"`
	SupportedCNIPlugins    []string      `json:"supported_cni_plugins"`
	EnableNetworkFlows     bool          `json:"enable_network_flows"`
	EnableDNSMonitoring    bool          `json:"enable_dns_monitoring"`
	EnablePolicyMonitoring bool          `json:"enable_policy_monitoring"`
	FlowCacheSize          int           `json:"flow_cache_size"`
	DNSCacheSize           int           `json:"dns_cache_size"`
	MaxConcurrentFlows     int           `json:"max_concurrent_flows"`
}

// CNIMetrics tracks CNI collector metrics
type CNIMetrics struct {
	EventsCollected    uint64 `json:"events_collected"`
	FlowsTracked       uint64 `json:"flows_tracked"`
	DNSQueries         uint64 `json:"dns_queries"`
	DNSFailures        uint64 `json:"dns_failures"`
	PolicyViolations   uint64 `json:"policy_violations"`
	ActiveFlows        uint64 `json:"active_flows"`
	BlockedConnections uint64 `json:"blocked_connections"`
	AllowedConnections uint64 `json:"allowed_connections"`
	CollectionErrors   uint64 `json:"collection_errors"`

	// Performance metrics
	FlowsPerSecond  float64       `json:"flows_per_second"`
	DNSFailureRate  float64       `json:"dns_failure_rate"`
	DNSLatencyP95   time.Duration `json:"dns_latency_p95"`
	CPUUsagePercent float64       `json:"cpu_usage_percent"`
	MemoryUsageMB   float64       `json:"memory_usage_mb"`

	// eBPF metrics
	eBPFMapUtilization map[string]float64 `json:"ebpf_map_utilization"`

	LastUpdate time.Time     `json:"last_update"`
	Uptime     time.Duration `json:"uptime"`

	mutex sync.RWMutex
}

// PodInfo represents information about a pod
type PodInfo struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	IP              net.IP            `json:"ip"`
	Labels          map[string]string `json:"labels"`
	Annotations     map[string]string `json:"annotations"`
	ServiceMeshInfo *ServiceMeshInfo  `json:"service_mesh_info,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

// ServiceMeshInfo contains service mesh related information
type ServiceMeshInfo struct {
	Injected     bool            `json:"injected"`
	MeshType     ServiceMeshType `json:"mesh_type"`
	ProxyImage   string          `json:"proxy_image"`
	ProxyVersion string          `json:"proxy_version"`
	TLSEnabled   bool            `json:"tls_enabled"`
	Policies     []string        `json:"policies"`
}

// ServiceMeshType represents different service mesh types
type ServiceMeshType string

const (
	ServiceMeshNone    ServiceMeshType = "none"
	ServiceMeshIstio   ServiceMeshType = "istio"
	ServiceMeshLinkerd ServiceMeshType = "linkerd"
	ServiceMeshConsul  ServiceMeshType = "consul"
	ServiceMeshCilium  ServiceMeshType = "cilium"
)

// CNIPlugin interface for different CNI plugin implementations
type CNIPlugin interface {
	Name() string
	Version() string
	GetNetworkConfig() (*NetworkConfig, error)
	GetPodNetworks() (map[string]*PodNetworkInfo, error)
	MonitorEvents(ctx context.Context, eventCh chan<- *CNIEvent) error
	GetMetrics() map[string]interface{}
}

// PodNetworkInfo represents pod network information
type PodNetworkInfo struct {
	PodName     string    `json:"pod_name"`
	Namespace   string    `json:"namespace"`
	IP          net.IP    `json:"ip"`
	Interface   string    `json:"interface"`
	NetworkName string    `json:"network_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// NewCNICollector creates a new CNI collector
func NewCNICollector(config *CNICollectorConfig) (*CNICollector, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if config.CollectionInterval <= 0 {
		return nil, fmt.Errorf("collection interval must be positive")
	}

	collector := &CNICollector{
		name:        "cni-collector",
		config:      config,
		metrics:     &CNIMetrics{},
		cniPlugins:  make(map[string]CNIPlugin),
		podIndex:    make(map[string]*PodInfo),
		stopChannel: make(chan struct{}),
		eventChan:   make(chan interface{}, 100),
	}

	// Initialize flow cache
	if config.FlowCacheSize > 0 {
		collector.flowCache = NewFlowCache(config.FlowCacheSize, 5*time.Minute)
	}

	// Initialize mesh analyzer
	collector.meshAnalyzer = NewMeshTrafficAnalyzer([]string{"istio", "linkerd", "consul", "cilium"})

	return collector, nil
}

// Start starts the CNI collector
func (c *CNICollector) Start(ctx context.Context) error {
	// Discover CNI plugins
	if err := c.discoverCNIPlugins(); err != nil {
		return fmt.Errorf("failed to discover CNI plugins: %w", err)
	}

	// Detect service mesh
	if meshType, err := c.detectServiceMesh(); err == nil {
		c.detectedMesh = meshType
	}

	// Start collection goroutines
	if c.config.EnableNetworkFlows {
		c.wg.Add(1)
		go c.collectNetworkFlows(ctx)
	}

	if c.config.EnableDNSMonitoring {
		c.wg.Add(1)
		go c.collectDNSQueries(ctx)
	}

	if c.config.EnablePolicyMonitoring {
		c.wg.Add(1)
		go c.collectPolicyViolations(ctx)
	}

	// Start metrics update
	c.wg.Add(1)
	go c.updateMetrics(ctx)

	// Start CNI plugin monitoring
	for _, plugin := range c.cniPlugins {
		c.wg.Add(1)
		go c.monitorCNIPlugin(ctx, plugin)
	}

	return nil
}

// Stop stops the CNI collector
func (c *CNICollector) Stop() error {
	close(c.stopChannel)
	c.wg.Wait()
	return nil
}

// GetMetrics returns collector metrics
func (c *CNICollector) GetMetrics() map[string]interface{} {
	c.metrics.mutex.RLock()
	defer c.metrics.mutex.RUnlock()

	uptime := time.Since(time.Now().Add(-c.metrics.Uptime))

	return map[string]interface{}{
		"events_collected":    c.metrics.EventsCollected,
		"flows_tracked":       c.metrics.FlowsTracked,
		"dns_queries":         c.metrics.DNSQueries,
		"dns_failures":        c.metrics.DNSFailures,
		"policy_violations":   c.metrics.PolicyViolations,
		"active_flows":        c.metrics.ActiveFlows,
		"blocked_connections": c.metrics.BlockedConnections,
		"allowed_connections": c.metrics.AllowedConnections,
		"collection_errors":   c.metrics.CollectionErrors,
		"flows_per_second":    c.metrics.FlowsPerSecond,
		"dns_failure_rate":    c.metrics.DNSFailureRate,
		"dns_latency_p95_ms":  c.metrics.DNSLatencyP95.Milliseconds(),
		"cpu_usage_percent":   c.metrics.CPUUsagePercent,
		"memory_usage_mb":     c.metrics.MemoryUsageMB,
		"uptime_seconds":      uptime.Seconds(),
		"last_update":         c.metrics.LastUpdate,
	}
}

// sendEvent sends an event to the event channel
func (c *CNICollector) sendEvent(event *opinionated.OpinionatedEvent) {
	select {
	case c.eventChan <- event:
	default:
		// Channel full, increment error counter
		c.metrics.mutex.Lock()
		c.metrics.CollectionErrors++
		c.metrics.mutex.Unlock()
	}
}

// findPodByIP finds a pod by its IP address
func (c *CNICollector) findPodByIP(ip net.IP) *PodInfo {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	ipStr := ip.String()
	if pod, exists := c.podIndex[ipStr]; exists {
		return pod
	}

	return nil
}

// protocolToString converts protocol number to string
func (c *CNICollector) protocolToString(protocol uint8) string {
	switch protocol {
	case 1:
		return "icmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 132:
		return "sctp"
	default:
		return fmt.Sprintf("proto-%d", protocol)
	}
}

// simulateNetworkFlows simulates network flows for demonstration
func (c *CNICollector) simulateNetworkFlows() {
	// This would be replaced with actual eBPF data reading
	flows := []*NetworkFlow{
		{
			FlowID:             "flow-001",
			SourceIP:           net.ParseIP("10.244.1.10"),
			DestinationIP:      net.ParseIP("10.244.2.20"),
			SourcePort:         45123,
			DestinationPort:    80,
			Protocol:           6, // TCP
			BytesTransmitted:   1024,
			PacketsTransmitted: 10,
			StartTime:          time.Now().Add(-1 * time.Minute),
			LastSeen:           time.Now(),
			State:              FlowStateActive,
			PolicyDecision:     PolicyAllow,
		},
	}

	for _, flow := range flows {
		if c.flowCache != nil {
			c.flowCache.AddFlow(flow)
		}

		// Update metrics
		c.metrics.mutex.Lock()
		c.metrics.FlowsTracked++
		c.metrics.mutex.Unlock()
	}
}

// Placeholder implementations for required methods
func (c *CNICollector) collectDNSQueries(ctx context.Context) {
	defer c.wg.Done()
	// Implementation would be in dns_monitor.go
}

func (c *CNICollector) collectPolicyViolations(ctx context.Context) {
	defer c.wg.Done()
	// Implementation would be in policy_monitor.go
}

func (c *CNICollector) updateMetrics(ctx context.Context) {
	defer c.wg.Done()
	// Implementation would calculate and update metrics
}

func (c *CNICollector) monitorCNIPlugin(ctx context.Context, plugin CNIPlugin) {
	defer c.wg.Done()
	// Implementation would be in cni_plugins.go
}

func (c *CNICollector) discoverCNIPlugins() error {
	// Implementation would be in cni_plugins.go
	return nil
}

func (c *CNICollector) detectServiceMesh() (ServiceMeshType, error) {
	// Implementation would be in dns_monitor.go
	return ServiceMeshNone, nil
}

// MeshTrafficAnalyzer analyzes service mesh traffic patterns
type MeshTrafficAnalyzer struct {
	supportedMeshes []string
	detectedMesh    ServiceMeshType
	trafficPatterns map[string]*MeshTrafficPattern
	mutex           sync.RWMutex
}

// MeshTrafficPattern represents traffic patterns in service mesh
type MeshTrafficPattern struct {
	SourceService      string        `json:"source_service"`
	DestinationService string        `json:"destination_service"`
	Protocol           string        `json:"protocol"`
	RequestRate        float64       `json:"request_rate"`
	SuccessRate        float64       `json:"success_rate"`
	P50Latency         time.Duration `json:"p50_latency"`
	P95Latency         time.Duration `json:"p95_latency"`
	P99Latency         time.Duration `json:"p99_latency"`
	ErrorRate          float64       `json:"error_rate"`
	TLSEnabled         bool          `json:"tls_enabled"`
	PolicyViolations   uint64        `json:"policy_violations"`
	LastUpdate         time.Time     `json:"last_update"`
}

// NewMeshTrafficAnalyzer creates a new mesh traffic analyzer
func NewMeshTrafficAnalyzer(supportedMeshes []string) *MeshTrafficAnalyzer {
	return &MeshTrafficAnalyzer{
		supportedMeshes: supportedMeshes,
		trafficPatterns: make(map[string]*MeshTrafficPattern),
	}
}

// processCNIEvent processes a CNI event and generates opinionated events
func (c *CNICollector) processCNIEvent(plugin CNIPlugin, cniEvent *CNIEvent) {
	// Create opinionated event
	severity := opinionated.SeverityInfo
	if cniEvent.Error != "" {
		severity = opinionated.SeverityHigh
	} else if cniEvent.Duration > 100*time.Millisecond {
		severity = opinionated.SeverityMedium
	}

	event := &opinionated.OpinionatedEvent{
		ID:         fmt.Sprintf("cni-%s-%s-%d", plugin.Name(), cniEvent.Type, time.Now().UnixNano()),
		Timestamp:  cniEvent.Timestamp,
		Category:   opinionated.CategoryNetworkHealth,
		Severity:   severity,
		Confidence: 0.95,
		Source: opinionated.EventSource{
			Collector: "cni-collector",
			Component: plugin.Name(),
			Node:      "current-node", // Would be actual node name
		},
		Context: opinionated.OpinionatedContext{
			Namespace: cniEvent.Namespace,
			Pod:       cniEvent.PodName,
		},
		Data: map[string]interface{}{
			"message":   fmt.Sprintf("CNI %s operation for pod %s/%s", cniEvent.Type, cniEvent.Namespace, cniEvent.PodName),
			"operation": cniEvent.Type,
		},
		Attributes: map[string]interface{}{
			"cni.plugin":         plugin.Name(),
			"cni.plugin_version": plugin.Version(),
			"cni.operation":      cniEvent.Type,
			"cni.pod_ip":         cniEvent.PodIP,
			"cni.interface":      cniEvent.Interface,
			"cni.duration_ms":    cniEvent.Duration.Milliseconds(),
		},
		CorrelationHints: []string{
			fmt.Sprintf("pod:%s", cniEvent.PodName),
			fmt.Sprintf("namespace:%s", cniEvent.Namespace),
			fmt.Sprintf("cni:%s", plugin.Name()),
		},
	}

	// Add error information if present
	if cniEvent.Error != "" {
		event.Attributes["cni.error"] = cniEvent.Error
		event.Data["error"] = cniEvent.Error
	}

	// Add plugin-specific metadata
	for k, v := range cniEvent.Metadata {
		event.Attributes[fmt.Sprintf("cni.%s", k)] = v
	}

	// Add CNI result information if available
	if cniEvent.Result != nil {
		event.Attributes["cni.result.cni_version"] = cniEvent.Result.CNIVersion
		event.Attributes["cni.result.interface_count"] = len(cniEvent.Result.Interfaces)
		event.Attributes["cni.result.ip_count"] = len(cniEvent.Result.IPs)
		event.Attributes["cni.result.route_count"] = len(cniEvent.Result.Routes)
	}

	c.sendEvent(event)

	// Update metrics
	c.metrics.mutex.Lock()
	c.metrics.EventsCollected++
	c.metrics.LastUpdate = time.Now()
	c.metrics.mutex.Unlock()
}

// isValidInterfaceName checks if an interface name follows expected patterns
func (c *CNICollector) isValidInterfaceName(ifaceName string) bool {
	// Common CNI interface patterns
	validPatterns := []string{
		"eth", "veth", "cali", "flannel", "cilium", "weave", "cbr", "docker", "cni",
	}

	for _, pattern := range validPatterns {
		if strings.HasPrefix(ifaceName, pattern) {
			return true
		}
	}

	return false
}
