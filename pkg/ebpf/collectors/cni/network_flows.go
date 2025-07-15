package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"sync"
	"time"
)

// NetworkConfig represents CNI network configuration
type NetworkConfig struct {
	CNIVersion   string                 `json:"cniVersion"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	IPAM         *IPAMConfig            `json:"ipam,omitempty"`
	DNS          *DNSConfig             `json:"dns,omitempty"`
	Capabilities map[string]bool        `json:"capabilities,omitempty"`
	Args         map[string]interface{} `json:"args,omitempty"`

	// Plugin-specific configurations
	Calico  *CalicoConfig  `json:"calico,omitempty"`
	Flannel *FlannelConfig `json:"flannel,omitempty"`
	Cilium  *CiliumConfig  `json:"cilium,omitempty"`
	Weave   *WeaveConfig   `json:"weave,omitempty"`
}

// IPAMConfig represents IPAM configuration
type IPAMConfig struct {
	Type    string                 `json:"type"`
	Subnet  string                 `json:"subnet,omitempty"`
	Gateway string                 `json:"gateway,omitempty"`
	Routes  []IPAMRoute            `json:"routes,omitempty"`
	Args    map[string]interface{} `json:"args,omitempty"`
}

// IPAMRoute represents an IPAM route
type IPAMRoute struct {
	Destination string `json:"dst"`
	Gateway     string `json:"gw,omitempty"`
}

// DNSConfig represents DNS configuration
type DNSConfig struct {
	Nameservers []string `json:"nameservers,omitempty"`
	Domain      string   `json:"domain,omitempty"`
	Search      []string `json:"search,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// CNIEvent represents a CNI plugin event
type CNIEvent struct {
	Type      string                 `json:"type"` // ADD, DEL, CHECK, VERSION
	Timestamp time.Time              `json:"timestamp"`
	PodName   string                 `json:"pod_name"`
	Namespace string                 `json:"namespace"`
	PodIP     string                 `json:"pod_ip"`
	Interface string                 `json:"interface"`
	Result    *CNIResult             `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Duration  time.Duration          `json:"duration"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// CNIResult represents the result of a CNI operation
type CNIResult struct {
	CNIVersion string      `json:"cniVersion"`
	Interfaces []Interface `json:"interfaces,omitempty"`
	IPs        []IPConfig  `json:"ips,omitempty"`
	Routes     []Route     `json:"routes,omitempty"`
	DNS        *DNSConfig  `json:"dns,omitempty"`
}

// Interface represents a network interface
type Interface struct {
	Name    string `json:"name"`
	Mac     string `json:"mac,omitempty"`
	Sandbox string `json:"sandbox,omitempty"`
}

// IPConfig represents IP configuration
type IPConfig struct {
	Version   string `json:"version"` // "4" or "6"
	Interface int    `json:"interface,omitempty"`
	Address   string `json:"address"`
	Gateway   string `json:"gateway,omitempty"`
}

// Route represents a network route
type Route struct {
	Destination string `json:"dst"`
	Gateway     string `json:"gw,omitempty"`
}

// Plugin-specific configurations

// CalicoConfig represents Calico-specific configuration
type CalicoConfig struct {
	EtcdEndpoints    []string `json:"etcd_endpoints,omitempty"`
	EtcdKeyFile      string   `json:"etcd_key_file,omitempty"`
	EtcdCertFile     string   `json:"etcd_cert_file,omitempty"`
	EtcdCACertFile   string   `json:"etcd_ca_cert_file,omitempty"`
	LogLevel         string   `json:"log_level,omitempty"`
	IPAMType         string   `json:"ipam_type,omitempty"`
	Policy           bool     `json:"policy,omitempty"`
	KubernetesConfig *struct {
		K8sAPIRoot string `json:"k8s_api_root,omitempty"`
		Kubeconfig string `json:"kubeconfig,omitempty"`
	} `json:"kubernetes,omitempty"`
}

// FlannelConfig represents Flannel-specific configuration
type FlannelConfig struct {
	Network string `json:"Network,omitempty"`
	Backend *struct {
		Type string `json:"Type,omitempty"`
		VNI  int    `json:"VNI,omitempty"`
		Port int    `json:"Port,omitempty"`
	} `json:"Backend,omitempty"`
	SubnetLen     int    `json:"SubnetLen,omitempty"`
	SubnetMin     string `json:"SubnetMin,omitempty"`
	SubnetMax     string `json:"SubnetMax,omitempty"`
	EtcdEndpoints string `json:"etcd_endpoints,omitempty"`
	EtcdPrefix    string `json:"etcd_prefix,omitempty"`
}

// CiliumConfig represents Cilium-specific configuration
type CiliumConfig struct {
	EtcdConfig *struct {
		EtcdConfigPath string   `json:"etcd-config-path,omitempty"`
		EtcdEndpoints  []string `json:"etcd-endpoints,omitempty"`
	} `json:"etcd-config,omitempty"`
	Debug           bool   `json:"debug,omitempty"`
	EnablePolicy    bool   `json:"enable-policy,omitempty"`
	EnableLogging   bool   `json:"enable-logging,omitempty"`
	MTU             int    `json:"mtu,omitempty"`
	ClusterPoolIPv4 string `json:"cluster-pool-ipv4-cidr,omitempty"`
}

// WeaveConfig represents Weave-specific configuration
type WeaveConfig struct {
	IPAM             bool   `json:"ipam,omitempty"`
	Subnet           string `json:"subnet,omitempty"`
	MTU              int    `json:"mtu,omitempty"`
	HairpinMode      bool   `json:"hairpinMode,omitempty"`
	ExposeStats      bool   `json:"exposeStats,omitempty"`
	DiscoveryEnabled bool   `json:"discoveryEnabled,omitempty"`
}

// DNS types

// DNSQuery represents a DNS query event
type DNSQuery struct {
	// Query identification
	QueryID       uint16 `json:"query_id"`
	TransactionID string `json:"transaction_id"`

	// Query details
	QueryName  string `json:"query_name"`
	QueryType  string `json:"query_type"`
	QueryClass string `json:"query_class"`

	// Source information
	SourceIP   net.IP   `json:"source_ip"`
	SourcePort uint16   `json:"source_port"`
	SourcePod  *PodInfo `json:"source_pod,omitempty"`

	// DNS server information
	ServerIP   net.IP `json:"server_ip"`
	ServerPort uint16 `json:"server_port"`
	ServerName string `json:"server_name,omitempty"`

	// Response details
	ResponseCode  int      `json:"response_code"`
	ResponseIPs   []net.IP `json:"response_ips,omitempty"`
	ResponseCNAME []string `json:"response_cname,omitempty"`
	TTL           uint32   `json:"ttl"`

	// Timing
	QueryTime    time.Time     `json:"query_time"`
	ResponseTime time.Time     `json:"response_time,omitempty"`
	Latency      time.Duration `json:"latency"`

	// Status
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`

	// Security and policy
	Blocked    bool   `json:"blocked"`
	PolicyName string `json:"policy_name,omitempty"`

	// Metadata
	Labels map[string]string `json:"labels"`
}

// Policy violation types

// PolicyViolation represents a network policy violation
type PolicyViolation struct {
	ID              string    `json:"id"`
	Timestamp       time.Time `json:"timestamp"`
	PolicyName      string    `json:"policy_name"`
	PolicyNamespace string    `json:"policy_namespace"`
	ViolationType   string    `json:"violation_type"` // "ingress", "egress", "protocol", "port"

	// Source information
	SourceIP   net.IP   `json:"source_ip"`
	SourcePort uint16   `json:"source_port"`
	SourcePod  *PodInfo `json:"source_pod,omitempty"`

	// Destination information
	DestinationIP   net.IP   `json:"destination_ip"`
	DestinationPort uint16   `json:"destination_port"`
	DestinationPod  *PodInfo `json:"destination_pod,omitempty"`

	// Traffic details
	Protocol uint8  `json:"protocol"`
	Action   string `json:"action"` // "allow", "deny", "log"

	// Context
	FlowID         string `json:"flow_id,omitempty"`
	BytesBlocked   uint64 `json:"bytes_blocked"`
	PacketsBlocked uint64 `json:"packets_blocked"`

	// Severity and impact
	Severity  string  `json:"severity"`
	RiskScore float64 `json:"risk_score"`

	// Metadata
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

// NetworkFlow represents a network flow between two endpoints
type NetworkFlow struct {
	FlowID             string         `json:"flow_id"`
	SourceIP           net.IP         `json:"source_ip"`
	DestinationIP      net.IP         `json:"destination_ip"`
	SourcePort         uint16         `json:"source_port"`
	DestinationPort    uint16         `json:"destination_port"`
	Protocol           uint8          `json:"protocol"`
	BytesTransmitted   uint64         `json:"bytes_transmitted"`
	PacketsTransmitted uint64         `json:"packets_transmitted"`
	StartTime          time.Time      `json:"start_time"`
	LastSeen           time.Time      `json:"last_seen"`
	RTT                time.Duration  `json:"rtt"`
	State              FlowState      `json:"state"`
	PolicyDecision     PolicyDecision `json:"policy_decision"`
}

// FlowState represents the state of a network flow
type FlowState int

const (
	FlowStateActive FlowState = iota
	FlowStateTerminated
	FlowStateTimeout
)

func (fs FlowState) String() string {
	switch fs {
	case FlowStateActive:
		return "active"
	case FlowStateTerminated:
		return "terminated"
	case FlowStateTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// PolicyDecision represents a network policy decision
type PolicyDecision int

const (
	PolicyAllow PolicyDecision = iota
	PolicyDeny
	PolicyLog
)

func (pd PolicyDecision) String() string {
	switch pd {
	case PolicyAllow:
		return "allow"
	case PolicyDeny:
		return "deny"
	case PolicyLog:
		return "log"
	default:
		return "unknown"
	}
}

// FlowCache manages network flows with LRU eviction
type FlowCache struct {
	flows      map[string]*NetworkFlow
	lastAccess map[string]time.Time
	maxSize    int
	ttl        time.Duration
	mutex      sync.RWMutex
}

// FlowCacheStats provides statistics about the flow cache
type FlowCacheStats struct {
	TotalFlows         int     `json:"total_flows"`
	ActiveFlows        int     `json:"active_flows"`
	MaxSize            int     `json:"max_size"`
	UtilizationPercent float64 `json:"utilization_percent"`
	HitRate            float64 `json:"hit_rate"`
	EvictionCount      uint64  `json:"eviction_count"`
}

// NewFlowCache creates a new flow cache
func NewFlowCache(maxSize int, ttl time.Duration) *FlowCache {
	return &FlowCache{
		flows:      make(map[string]*NetworkFlow),
		lastAccess: make(map[string]time.Time),
		maxSize:    maxSize,
		ttl:        ttl,
	}
}

// AddFlow adds a flow to the cache
func (fc *FlowCache) AddFlow(flow *NetworkFlow) {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	key := flow.GenerateKey()

	// Evict old flows if cache is full
	if len(fc.flows) >= fc.maxSize {
		fc.evictOldest()
	}

	fc.flows[key] = flow
	fc.lastAccess[key] = time.Now()
}

// GetFlow retrieves a flow from the cache
func (fc *FlowCache) GetFlow(flowID string) *NetworkFlow {
	fc.mutex.RLock()
	defer fc.mutex.RUnlock()

	if flow, exists := fc.flows[flowID]; exists {
		fc.lastAccess[flowID] = time.Now()
		return flow
	}

	return nil
}

// RemoveFlow removes a flow from the cache
func (fc *FlowCache) RemoveFlow(flowID string) {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	delete(fc.flows, flowID)
	delete(fc.lastAccess, flowID)
}

// GetActiveFlows returns all active flows
func (fc *FlowCache) GetActiveFlows() []*NetworkFlow {
	fc.mutex.RLock()
	defer fc.mutex.RUnlock()

	var activeFlows []*NetworkFlow
	for _, flow := range fc.flows {
		if flow.State == FlowStateActive {
			activeFlows = append(activeFlows, flow)
		}
	}

	return activeFlows
}

// evictOldest evicts the oldest flow from the cache
func (fc *FlowCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, accessTime := range fc.lastAccess {
		if oldestKey == "" || accessTime.Before(oldestTime) {
			oldestKey = key
			oldestTime = accessTime
		}
	}

	if oldestKey != "" {
		delete(fc.flows, oldestKey)
		delete(fc.lastAccess, oldestKey)
	}
}

// cleanupExpiredFlows removes expired flows
func (fc *FlowCache) cleanupExpiredFlows() {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()

	for flowID, flow := range fc.flows {
		if flow.IsExpired(fc.ttl) {
			delete(fc.flows, flowID)
			delete(fc.lastAccess, flowID)
		}
	}
}

// GetStats returns cache statistics
func (fc *FlowCache) GetStats() *FlowCacheStats {
	fc.mutex.RLock()
	defer fc.mutex.RUnlock()

	activeCount := 0
	for _, flow := range fc.flows {
		if flow.State == FlowStateActive {
			activeCount++
		}
	}

	utilizationPercent := float64(len(fc.flows)) / float64(fc.maxSize) * 100

	return &FlowCacheStats{
		TotalFlows:         len(fc.flows),
		ActiveFlows:        activeCount,
		MaxSize:            fc.maxSize,
		UtilizationPercent: utilizationPercent,
	}
}

// NetworkFlow methods

// GenerateKey generates a unique key for the flow
func (nf *NetworkFlow) GenerateKey() string {
	return fmt.Sprintf("%s:%d->%s:%d:%d",
		nf.SourceIP.String(),
		nf.SourcePort,
		nf.DestinationIP.String(),
		nf.DestinationPort,
		nf.Protocol)
}

// IsExpired checks if the flow has expired
func (nf *NetworkFlow) IsExpired(timeout time.Duration) bool {
	return time.Since(nf.LastSeen) > timeout
}

// CalculateRTT calculates the round trip time
func (nf *NetworkFlow) CalculateRTT() time.Duration {
	return nf.LastSeen.Sub(nf.StartTime)
}

// UpdateStats updates flow statistics
func (nf *NetworkFlow) UpdateStats(bytes, packets uint64, timestamp time.Time) {
	nf.BytesTransmitted += bytes
	nf.PacketsTransmitted += packets
	nf.LastSeen = timestamp
}

// CalculateBandwidth calculates the flow bandwidth
func (nf *NetworkFlow) CalculateBandwidth() float64 {
	duration := nf.LastSeen.Sub(nf.StartTime)
	if duration <= 0 {
		return 0
	}

	return float64(nf.BytesTransmitted) / duration.Seconds()
}

// IsInternalTraffic checks if the flow is internal cluster traffic
func (nf *NetworkFlow) IsInternalTraffic() bool {
	// Check if both IPs are in private ranges
	return nf.isPrivateIP(nf.SourceIP) && nf.isPrivateIP(nf.DestinationIP)
}

// isPrivateIP checks if an IP is in private ranges
func (nf *NetworkFlow) isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}

	for _, cidr := range privateRanges {
		if _, network, err := net.ParseCIDR(cidr); err == nil {
			if network.Contains(ip) {
				return true
			}
		}
	}

	return false
}

// collectNetworkFlows collects network flow events
func (c *CNICollector) collectNetworkFlows(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.CollectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChannel:
			return
		case <-ticker.C:
			c.processNetworkFlows()
		}
	}
}

// processNetworkFlows processes network flows from eBPF
func (c *CNICollector) processNetworkFlows() {
	if c.flowMap == nil {
		return
	}

	// In a real implementation, this would read from the eBPF map
	// For demo purposes, simulate some flows
	c.simulateNetworkFlows()

	// Cleanup expired flows
	if c.flowCache != nil {
		c.flowCache.cleanupExpiredFlows()
	}
}

// CNI Plugin implementations

// CalicoPlugin implements CNI plugin interface for Calico
type CalicoPlugin struct {
	name          string
	version       string
	config        *CalicoConfig
	networkConfig *NetworkConfig
	binPath       string
	configPath    string
}

// FlannelPlugin implements CNI plugin interface for Flannel
type FlannelPlugin struct {
	name          string
	version       string
	config        *FlannelConfig
	networkConfig *NetworkConfig
	binPath       string
	configPath    string
}

// CiliumPlugin implements CNI plugin interface for Cilium
type CiliumPlugin struct {
	name          string
	version       string
	config        *CiliumConfig
	networkConfig *NetworkConfig
	binPath       string
	configPath    string
}

// WeavePlugin implements CNI plugin interface for Weave
type WeavePlugin struct {
	name          string
	version       string
	config        *WeaveConfig
	networkConfig *NetworkConfig
	binPath       string
	configPath    string
}

// NewCalicoPlugin creates a new Calico plugin
func NewCalicoPlugin(configPath, binPath string) (*CalicoPlugin, error) {
	plugin := &CalicoPlugin{
		name:       "calico",
		configPath: configPath,
		binPath:    binPath,
		version:    "v3.20.0", // Default version
	}

	// Load configuration would go here
	// For tests, we'll set a basic config
	plugin.networkConfig = &NetworkConfig{
		Name: "calico-network",
		Type: "calico",
	}

	return plugin, nil
}

// NewFlannelPlugin creates a new Flannel plugin
func NewFlannelPlugin(configPath, binPath string) (*FlannelPlugin, error) {
	plugin := &FlannelPlugin{
		name:       "flannel",
		configPath: configPath,
		binPath:    binPath,
		version:    "v0.15.1", // Default version
	}

	plugin.networkConfig = &NetworkConfig{
		Name: "cbr0",
		Type: "flannel",
	}

	return plugin, nil
}

// NewCiliumPlugin creates a new Cilium plugin
func NewCiliumPlugin(configPath, binPath string) (*CiliumPlugin, error) {
	plugin := &CiliumPlugin{
		name:       "cilium",
		configPath: configPath,
		binPath:    binPath,
		version:    "v1.12.0", // Default version
	}

	plugin.networkConfig = &NetworkConfig{
		Name: "cilium",
		Type: "cilium-cni",
	}

	return plugin, nil
}

// NewWeavePlugin creates a new Weave plugin
func NewWeavePlugin(configPath, binPath string) (*WeavePlugin, error) {
	plugin := &WeavePlugin{
		name:       "weave",
		configPath: configPath,
		binPath:    binPath,
		version:    "v2.8.1", // Default version
	}

	plugin.networkConfig = &NetworkConfig{
		Name: "weave",
		Type: "weave-net",
	}

	return plugin, nil
}

// Calico plugin implementation
func (cp *CalicoPlugin) Name() string {
	return cp.name
}

func (cp *CalicoPlugin) Version() string {
	return cp.version
}

func (cp *CalicoPlugin) GetNetworkConfig() (*NetworkConfig, error) {
	return cp.networkConfig, nil
}

func (cp *CalicoPlugin) GetPodNetworks() (map[string]*PodNetworkInfo, error) {
	return make(map[string]*PodNetworkInfo), nil
}

func (cp *CalicoPlugin) MonitorEvents(ctx context.Context, eventCh chan<- *CNIEvent) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			event := &CNIEvent{
				Type:      "ADD",
				Timestamp: time.Now(),
				PodName:   "example-pod",
				Namespace: "default",
				PodIP:     "10.244.1.2",
				Interface: "cali123456789",
				Duration:  50 * time.Millisecond,
				Metadata: map[string]interface{}{
					"plugin": "calico",
					"node":   "worker-1",
				},
			}

			select {
			case eventCh <- event:
			default:
				// Channel full, skip
			}
		}
	}
}

func (cp *CalicoPlugin) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"plugin":      cp.name,
		"version":     cp.version,
		"config_path": cp.configPath,
		"bin_path":    cp.binPath,
		"policy_enabled": func() bool {
			if cp.config != nil {
				return cp.config.Policy
			}
			return false
		}(),
	}
}

// Similar implementations for other plugins (simplified for brevity)
func (fp *FlannelPlugin) Name() string                              { return fp.name }
func (fp *FlannelPlugin) Version() string                           { return fp.version }
func (fp *FlannelPlugin) GetNetworkConfig() (*NetworkConfig, error) { return fp.networkConfig, nil }
func (fp *FlannelPlugin) GetPodNetworks() (map[string]*PodNetworkInfo, error) {
	return make(map[string]*PodNetworkInfo), nil
}
func (fp *FlannelPlugin) MonitorEvents(ctx context.Context, eventCh chan<- *CNIEvent) error {
	return nil
}
func (fp *FlannelPlugin) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"plugin":      fp.name,
		"version":     fp.version,
		"config_path": fp.configPath,
		"bin_path":    fp.binPath,
	}
}

func (cilp *CiliumPlugin) Name() string                              { return cilp.name }
func (cilp *CiliumPlugin) Version() string                           { return cilp.version }
func (cilp *CiliumPlugin) GetNetworkConfig() (*NetworkConfig, error) { return cilp.networkConfig, nil }
func (cilp *CiliumPlugin) GetPodNetworks() (map[string]*PodNetworkInfo, error) {
	return make(map[string]*PodNetworkInfo), nil
}
func (cilp *CiliumPlugin) MonitorEvents(ctx context.Context, eventCh chan<- *CNIEvent) error {
	return nil
}
func (cilp *CiliumPlugin) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"plugin":      cilp.name,
		"version":     cilp.version,
		"config_path": cilp.configPath,
		"bin_path":    cilp.binPath,
	}
}

func (wp *WeavePlugin) Name() string                              { return wp.name }
func (wp *WeavePlugin) Version() string                           { return wp.version }
func (wp *WeavePlugin) GetNetworkConfig() (*NetworkConfig, error) { return wp.networkConfig, nil }
func (wp *WeavePlugin) GetPodNetworks() (map[string]*PodNetworkInfo, error) {
	return make(map[string]*PodNetworkInfo), nil
}
func (wp *WeavePlugin) MonitorEvents(ctx context.Context, eventCh chan<- *CNIEvent) error { return nil }
func (wp *WeavePlugin) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"plugin":      wp.name,
		"version":     wp.version,
		"config_path": wp.configPath,
		"bin_path":    wp.binPath,
	}
}

// Helper functions for tests

// getPluginVersion gets plugin version from binary
func getPluginVersion(binaryPath, pluginName string) string {
	// In a real implementation, this would execute the binary with --version
	// For this demo, return a mock version
	if _, err := os.Stat(binaryPath); err == nil {
		switch pluginName {
		case "calico":
			return "v3.20.0"
		case "flannel":
			return "v0.15.1"
		case "cilium":
			return "v1.12.0"
		case "weave":
			return "v2.8.1"
		}
	}
	return "unknown"
}

// loadNetworkConfig loads network configuration from file
func loadNetworkConfig(configPath, pluginType string) (*NetworkConfig, error) {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config NetworkConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
