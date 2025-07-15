package cni

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf"
	"github.com/yairfalse/tapio/pkg/collectors/unified"
	"github.com/yairfalse/tapio/pkg/logging"
)

// Collector implements unified.Collector for CNI network monitoring
type Collector struct {
	// Configuration
	config unified.CollectorConfig
	logger *logging.Logger

	// eBPF programs and maps
	networkFlowProgram     *ebpf.Program
	podTrafficProgram      *ebpf.Program
	dnsMonitorProgram      *ebpf.Program
	policyViolationProgram *ebpf.Program

	flowMap            *ebpf.Map
	podTrafficMap      *ebpf.Map
	dnsQueriesMap      *ebpf.Map
	policyViolationMap *ebpf.Map

	// CNI plugin detection and management
	cniPlugins   map[string]CNIPlugin
	detectedMesh ServiceMeshType
	meshAnalyzer *MeshTrafficAnalyzer

	// Pod tracking
	podIndex  map[string]*PodInfo
	flowCache *FlowCache

	// Event channel
	eventChan chan *unified.Event

	// State management
	started atomic.Bool
	stopped atomic.Bool
	enabled atomic.Bool

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Statistics
	stats struct {
		eventsCollected  atomic.Uint64
		flowsTracked     atomic.Uint64
		dnsQueries       atomic.Uint64
		dnsFailures      atomic.Uint64
		policyViolations atomic.Uint64
		activeFlows      atomic.Uint64
		collectionErrors atomic.Uint64
	}

	// Performance tracking
	lastEventTime atomic.Value // time.Time
	startTime     time.Time
	mutex         sync.RWMutex
}

// CNICollectorConfig extends the base config with CNI-specific options
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

// PodInfo represents information about a pod
type PodInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	IP        net.IP            `json:"ip"`
	Labels    map[string]string `json:"labels"`
	CNIPlugin string            `json:"cni_plugin"`
	MeshType  ServiceMeshType   `json:"mesh_type"`
	CreatedAt time.Time         `json:"created_at"`
}

// ServiceMeshType represents detected service mesh
type ServiceMeshType string

const (
	MeshTypeNone    ServiceMeshType = "none"
	MeshTypeIstio   ServiceMeshType = "istio"
	MeshTypeLinkerd ServiceMeshType = "linkerd"
	MeshTypeConsul  ServiceMeshType = "consul"
	MeshTypeCilium  ServiceMeshType = "cilium"
)

// CNIPlugin represents a CNI plugin interface
type CNIPlugin interface {
	Name() string
	Version() string
	Capabilities() []string
	IsHealthy() bool
	GetPodNetworkInfo(podName, namespace string) (*PodNetworkInfo, error)
}

// PodNetworkInfo contains network information for a pod
type PodNetworkInfo struct {
	IP        net.IP          `json:"ip"`
	Gateway   net.IP          `json:"gateway"`
	Subnet    *net.IPNet      `json:"subnet"`
	Interface string          `json:"interface"`
	Plugin    string          `json:"plugin"`
	Policies  []NetworkPolicy `json:"policies"`
}

// NetworkPolicy represents a network policy
type NetworkPolicy struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Type      string   `json:"type"`   // ingress, egress, both
	Action    string   `json:"action"` // allow, deny
	Rules     []string `json:"rules"`
}

// NewCNICollector creates a new CNI collector
func NewCNICollector(config unified.CollectorConfig) (*Collector, error) {
	logger := logging.Development.WithComponent("cni-collector")

	// Extract CNI-specific configuration
	cniConfig, err := extractCNIConfig(config.Extra)
	if err != nil {
		return nil, fmt.Errorf("invalid CNI configuration: %w", err)
	}

	c := &Collector{
		config:     config,
		logger:     logger,
		eventChan:  make(chan *unified.Event, config.EventBufferSize),
		cniPlugins: make(map[string]CNIPlugin),
		podIndex:   make(map[string]*PodInfo),
		flowCache:  NewFlowCache(cniConfig.FlowCacheSize),
		enabled:    atomic.Bool{},
		startTime:  time.Now(),
	}

	// Initialize as enabled based on config
	c.enabled.Store(config.Enabled)
	c.lastEventTime.Store(time.Now())

	// Initialize mesh analyzer
	c.meshAnalyzer = NewMeshTrafficAnalyzer()

	// Detect and initialize CNI plugins
	if err := c.initializeCNIPlugins(cniConfig); err != nil {
		return nil, fmt.Errorf("failed to initialize CNI plugins: %w", err)
	}

	return c, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.config.Name
}

// Type returns the collector type
func (c *Collector) Type() string {
	return "cni"
}

// Start begins collecting CNI events
func (c *Collector) Start(ctx context.Context) error {
	if !c.enabled.Load() {
		return fmt.Errorf("CNI collector is disabled")
	}

	if c.started.Load() {
		return fmt.Errorf("CNI collector already started")
	}

	c.logger.Info("Starting CNI collector",
		"cni_plugins", len(c.cniPlugins),
		"detected_mesh", c.detectedMesh,
	)

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load and attach eBPF programs
	if err := c.loadeBPFPrograms(); err != nil {
		return fmt.Errorf("failed to load eBPF programs: %w", err)
	}

	// Mark as started
	c.started.Store(true)

	// Start collection routines
	c.wg.Add(4)
	go c.collectNetworkFlows()
	go c.collectDNSEvents()
	go c.collectPolicyViolations()
	go c.collectPodTraffic()

	// Start statistics reporting
	c.wg.Add(1)
	go c.reportStatistics()

	c.logger.Info("CNI collector started successfully")
	return nil
}

// Stop halts the collector
func (c *Collector) Stop() error {
	if !c.started.Load() {
		return fmt.Errorf("CNI collector not started")
	}

	if c.stopped.Load() {
		return fmt.Errorf("CNI collector already stopped")
	}

	c.logger.Info("Stopping CNI collector")

	// Mark as stopping
	c.stopped.Store(true)

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Clean up eBPF resources
	c.cleanupeBPFPrograms()

	// Wait for goroutines
	c.wg.Wait()

	// Close event channel
	close(c.eventChan)

	c.logger.Info("CNI collector stopped",
		"flows_tracked", c.stats.flowsTracked.Load(),
		"dns_queries", c.stats.dnsQueries.Load(),
		"policy_violations", c.stats.policyViolations.Load(),
	)

	return nil
}

// IsEnabled returns whether the collector is enabled
func (c *Collector) IsEnabled() bool {
	return c.enabled.Load()
}

// Events returns the event channel
func (c *Collector) Events() <-chan *unified.Event {
	return c.eventChan
}

// Health returns the collector health status
func (c *Collector) Health() *unified.Health {
	status := unified.HealthStatusHealthy
	message := "CNI collector is healthy"

	if !c.started.Load() {
		status = unified.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = unified.HealthStatusUnhealthy
		message = "Collector stopped"
	} else if c.stats.collectionErrors.Load() > 100 {
		status = unified.HealthStatusDegraded
		message = fmt.Sprintf("High error count: %d", c.stats.collectionErrors.Load())
	}

	lastEvent := c.lastEventTime.Load().(time.Time)
	if time.Since(lastEvent) > 5*time.Minute && c.started.Load() {
		status = unified.HealthStatusDegraded
		message = "No events received in 5 minutes"
	}

	return &unified.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   lastEvent,
		EventsProcessed: c.stats.eventsCollected.Load(),
		ErrorCount:      c.stats.collectionErrors.Load(),
		Metrics: map[string]interface{}{
			"flows_tracked":     c.stats.flowsTracked.Load(),
			"dns_queries":       c.stats.dnsQueries.Load(),
			"dns_failures":      c.stats.dnsFailures.Load(),
			"policy_violations": c.stats.policyViolations.Load(),
			"active_flows":      c.stats.activeFlows.Load(),
			"detected_mesh":     string(c.detectedMesh),
			"cni_plugins_count": len(c.cniPlugins),
		},
	}
}

// GetStats returns collector statistics
func (c *Collector) GetStats() *unified.Stats {
	uptime := time.Since(c.startTime)
	eventsCollected := c.stats.eventsCollected.Load()

	return &unified.Stats{
		EventsCollected: eventsCollected,
		ErrorCount:      c.stats.collectionErrors.Load(),
		StartTime:       c.startTime,
		LastEventTime:   c.lastEventTime.Load().(time.Time),
		Custom: map[string]interface{}{
			"flows_tracked":     c.stats.flowsTracked.Load(),
			"dns_queries":       c.stats.dnsQueries.Load(),
			"dns_failures":      c.stats.dnsFailures.Load(),
			"policy_violations": c.stats.policyViolations.Load(),
			"active_flows":      c.stats.activeFlows.Load(),
			"events_per_second": float64(eventsCollected) / uptime.Seconds(),
			"uptime_seconds":    uptime.Seconds(),
			"detected_mesh":     string(c.detectedMesh),
			"cni_plugins_count": len(c.cniPlugins),
		},
	}
}

// Configure updates the collector configuration
func (c *Collector) Configure(config unified.CollectorConfig) error {
	c.config = config
	c.enabled.Store(config.Enabled)

	c.logger.Info("Updated CNI collector configuration",
		"enabled", config.Enabled,
		"buffer_size", config.EventBufferSize,
	)

	return nil
}

// Collection methods (implement the actual eBPF data collection)

func (c *Collector) collectNetworkFlows() {
	defer c.wg.Done()
	// Implementation for network flow collection from eBPF maps
	// This would read from flowMap and create unified.Event objects
}

func (c *Collector) collectDNSEvents() {
	defer c.wg.Done()
	// Implementation for DNS event collection from eBPF maps
	// This would read from dnsQueriesMap and create unified.Event objects
}

func (c *Collector) collectPolicyViolations() {
	defer c.wg.Done()
	// Implementation for policy violation collection from eBPF maps
	// This would read from policyViolationMap and create unified.Event objects
}

func (c *Collector) collectPodTraffic() {
	defer c.wg.Done()
	// Implementation for pod traffic collection from eBPF maps
	// This would read from podTrafficMap and create unified.Event objects
}

func (c *Collector) reportStatistics() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.logger.Info("CNI collector statistics",
				"events_collected", c.stats.eventsCollected.Load(),
				"flows_tracked", c.stats.flowsTracked.Load(),
				"dns_queries", c.stats.dnsQueries.Load(),
				"policy_violations", c.stats.policyViolations.Load(),
				"active_flows", c.stats.activeFlows.Load(),
			)
		}
	}
}

// Helper methods

func (c *Collector) loadeBPFPrograms() error {
	// Implementation to load eBPF programs
	// This would load the actual eBPF bytecode and attach to kernel
	return nil
}

func (c *Collector) cleanupeBPFPrograms() {
	// Implementation to clean up eBPF resources
	if c.networkFlowProgram != nil {
		c.networkFlowProgram.Close()
	}
	// Close other programs and maps...
}

func (c *Collector) initializeCNIPlugins(config *CNICollectorConfig) error {
	// Implementation to detect and initialize CNI plugins
	// This would scan for available CNI plugins and create plugin instances
	c.detectedMesh = c.detectServiceMesh()
	return nil
}

func (c *Collector) detectServiceMesh() ServiceMeshType {
	// Implementation to detect service mesh type
	// This would examine pod labels, annotations, and network configuration
	return MeshTypeNone
}

func extractCNIConfig(extra map[string]interface{}) (*CNICollectorConfig, error) {
	config := &CNICollectorConfig{
		// Defaults
		CollectionInterval:     time.Second,
		CNIConfigPath:          "/etc/cni/net.d",
		CNIBinPath:             "/opt/cni/bin",
		SupportedCNIPlugins:    []string{"calico", "flannel", "cilium", "weave"},
		EnableNetworkFlows:     true,
		EnableDNSMonitoring:    true,
		EnablePolicyMonitoring: true,
		FlowCacheSize:          10000,
		DNSCacheSize:           1000,
		MaxConcurrentFlows:     50000,
	}

	// Override with provided config
	if extra != nil {
		// Implementation would parse extra config
	}

	return config, nil
}

// FlowCache and MeshTrafficAnalyzer stubs
type FlowCache struct {
	// Implementation for flow caching
}

func NewFlowCache(size int) *FlowCache {
	return &FlowCache{}
}

type MeshTrafficAnalyzer struct {
	// Implementation for service mesh traffic analysis
}

func NewMeshTrafficAnalyzer() *MeshTrafficAnalyzer {
	return &MeshTrafficAnalyzer{}
}
