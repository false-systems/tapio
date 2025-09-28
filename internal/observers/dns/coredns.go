package dns

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ProcessInfo represents a process for CoreDNS detection
type ProcessInfo struct {
	PID     uint32
	Name    string
	Cmdline string
}

// CoreDNSProcessDetector detects CoreDNS processes
type CoreDNSProcessDetector struct {
	knownPIDs map[uint32]bool
}

// NewCoreDNSProcessDetector creates a new CoreDNS detector
func NewCoreDNSProcessDetector() *CoreDNSProcessDetector {
	return &CoreDNSProcessDetector{
		knownPIDs: make(map[uint32]bool),
	}
}

// IsCoreDNS checks if a process is CoreDNS
func (d *CoreDNSProcessDetector) IsCoreDNS(process ProcessInfo) bool {
	// Check process name
	if strings.Contains(strings.ToLower(process.Name), "coredns") {
		return true
	}
	if strings.Contains(strings.ToLower(process.Name), "kube-dns") {
		return true
	}
	// Check command line
	if strings.Contains(strings.ToLower(process.Cmdline), "coredns") {
		return true
	}
	return false
}

// RegisterPID registers a CoreDNS process ID
func (d *CoreDNSProcessDetector) RegisterPID(pid uint32) {
	d.knownPIDs[pid] = true
}

// IsPIDRegistered checks if a PID is registered as CoreDNS
func (d *CoreDNSProcessDetector) IsPIDRegistered(pid uint32) bool {
	return d.knownPIDs[pid]
}

// CoreDNSMetrics represents parsed CoreDNS metrics
type CoreDNSMetrics struct {
	QueryCount     map[string]uint64
	CacheHits      uint64
	CacheMisses    uint64
	PluginsEnabled map[string]bool
}

// CoreDNSMetricsParser parses CoreDNS Prometheus metrics
type CoreDNSMetricsParser struct{}

// NewCoreDNSMetricsParser creates a new metrics parser
func NewCoreDNSMetricsParser() *CoreDNSMetricsParser {
	return &CoreDNSMetricsParser{}
}

// Parse parses CoreDNS metrics text
func (p *CoreDNSMetricsParser) Parse(metricsText string) (*CoreDNSMetrics, error) {
	metrics := &CoreDNSMetrics{
		QueryCount:     make(map[string]uint64),
		PluginsEnabled: make(map[string]bool),
	}

	lines := strings.Split(metricsText, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse query counts
		if strings.HasPrefix(line, "coredns_dns_request_count_total") {
			if strings.Contains(line, "type=\"A\"") {
				metrics.QueryCount["A"] = parseMetricValue(line)
			} else if strings.Contains(line, "type=\"AAAA\"") {
				metrics.QueryCount["AAAA"] = parseMetricValue(line)
			}
		}

		// Parse cache metrics
		if strings.HasPrefix(line, "coredns_cache_hits_total") {
			if strings.Contains(line, "type=\"success\"") {
				metrics.CacheHits = parseMetricValue(line)
			}
		}
		if strings.HasPrefix(line, "coredns_cache_misses_total") {
			metrics.CacheMisses = parseMetricValue(line)
		}

		// Parse plugin status
		if strings.HasPrefix(line, "coredns_plugin_enabled") {
			if strings.Contains(line, "name=\"cache\"") && strings.HasSuffix(line, " 1") {
				metrics.PluginsEnabled["cache"] = true
			}
			if strings.Contains(line, "name=\"kubernetes\"") && strings.HasSuffix(line, " 1") {
				metrics.PluginsEnabled["kubernetes"] = true
			}
			if strings.Contains(line, "name=\"forward\"") && strings.HasSuffix(line, " 1") {
				metrics.PluginsEnabled["forward"] = true
			}
		}
	}

	return metrics, nil
}

func parseMetricValue(line string) uint64 {
	parts := strings.Fields(line)
	if len(parts) > 0 {
		val, _ := strconv.ParseUint(parts[len(parts)-1], 10, 64)
		return val
	}
	return 0
}

// K8sServiceInfo represents Kubernetes service information
type K8sServiceInfo struct {
	Service         string
	Namespace       string
	Type            string // svc, pod, short
	PodIP           string
	PodName         string
	Port            string
	Protocol        string
	IsSystemService bool
}

// K8sServiceDetector detects Kubernetes service queries
type K8sServiceDetector struct{}

// NewK8sServiceDetector creates a new K8s detector
func NewK8sServiceDetector() *K8sServiceDetector {
	return &K8sServiceDetector{}
}

// Detect detects Kubernetes service patterns
func (d *K8sServiceDetector) Detect(queryName string) (K8sServiceInfo, bool) {
	info := K8sServiceInfo{}
	queryName = strings.ToLower(queryName)

	// Check for cluster.local domains
	if strings.HasSuffix(queryName, ".cluster.local") {
		parts := strings.Split(queryName, ".")

		// Check for SRV records: _http._tcp.service.namespace.svc.cluster.local
		if len(parts) >= 6 && parts[4] == "svc" && strings.HasPrefix(parts[0], "_") && strings.HasPrefix(parts[1], "_") {
			info.Port = strings.TrimPrefix(parts[0], "_")
			info.Protocol = strings.TrimPrefix(parts[1], "_")
			info.Service = parts[2]
			info.Namespace = parts[3]
			info.Type = "svc"
			return info, true
		}

		// Check for StatefulSet pods: pod-0.service.namespace.svc.cluster.local
		if len(parts) >= 6 && parts[3] == "svc" && strings.Contains(parts[0], "-") {
			info.PodName = parts[0]
			info.Service = parts[1]
			info.Namespace = parts[2]
			info.Type = "svc"
			return info, true
		}

		// Standard service: service.namespace.svc.cluster.local
		if len(parts) >= 5 && parts[2] == "svc" {
			info.Service = parts[0]
			info.Namespace = parts[1]
			info.Type = "svc"

			// Check if system service
			if info.Service == "kubernetes" || info.Namespace == "kube-system" {
				info.IsSystemService = true
			}

			return info, true
		}

		// Pod IP: 10-244-0-5.namespace.pod.cluster.local
		if len(parts) >= 5 && parts[2] == "pod" {
			ipStr := strings.ReplaceAll(parts[0], "-", ".")
			info.PodIP = ipStr
			info.Namespace = parts[1]
			info.Type = "pod"
			return info, true
		}
	}

	// Short format: service.namespace.svc
	if strings.HasSuffix(queryName, ".svc") {
		parts := strings.Split(queryName, ".")
		if len(parts) >= 3 {
			info.Service = parts[0]
			info.Namespace = parts[1]
			info.Type = "svc"
			return info, true
		}
	}

	// Very short format: service.namespace
	parts := strings.Split(queryName, ".")
	if len(parts) == 2 && !strings.Contains(queryName, "com") && !strings.Contains(queryName, "org") {
		info.Service = parts[0]
		info.Namespace = parts[1]
		info.Type = "short"
		return info, true
	}

	return info, false
}

// CoreDNSInfo wraps K8sServiceInfo with CoreDNS flag
type CoreDNSInfo struct {
	K8sServiceInfo
	IsCoreDNS bool
}

// CoreDNSDetector detects CoreDNS queries
type CoreDNSDetector struct {
	k8sDetector *K8sServiceDetector
}

// NewCoreDNSDetector creates a new CoreDNS detector
func NewCoreDNSDetector() *CoreDNSDetector {
	return &CoreDNSDetector{
		k8sDetector: NewK8sServiceDetector(),
	}
}

// Detect detects if a query is CoreDNS related
func (d *CoreDNSDetector) Detect(queryName string) CoreDNSInfo {
	info, isK8s := d.k8sDetector.Detect(queryName)
	return CoreDNSInfo{
		K8sServiceInfo: info,
		IsCoreDNS:      isK8s,
	}
}

// CoreDNSPlugin represents a CoreDNS plugin
type CoreDNSPlugin struct {
	Name  string
	Order int
}

// CoreDNSPluginChain simulates CoreDNS plugin chain
type CoreDNSPluginChain struct {
	plugins []CoreDNSPlugin
}

// NewCoreDNSPluginChain creates a new plugin chain
func NewCoreDNSPluginChain(plugins []CoreDNSPlugin) *CoreDNSPluginChain {
	return &CoreDNSPluginChain{
		plugins: plugins,
	}
}

// ProcessQuery simulates processing a query through the plugin chain
func (c *CoreDNSPluginChain) ProcessQuery(query DNSQuery) []PluginEvent {
	var events []PluginEvent

	// Check if it's a Kubernetes query
	detector := NewK8sServiceDetector()
	if info, isK8s := detector.Detect(query.Name); isK8s {
		// Kubernetes plugin would handle this
		events = append(events, PluginEvent{
			Plugin:       "kubernetes",
			K8sService:   info.Service,
			K8sNamespace: info.Namespace,
		})
	}

	return events
}

// PluginEvent represents an event from a plugin
type PluginEvent struct {
	Plugin       string
	K8sService   string
	K8sNamespace string
}

// CoreDNSCache simulates CoreDNS cache
type CoreDNSCache struct {
	hits   uint64
	misses uint64
}

// NewCoreDNSCache creates a new cache simulator
func NewCoreDNSCache() *CoreDNSCache {
	return &CoreDNSCache{}
}

// RecordHit records a cache hit
func (c *CoreDNSCache) RecordHit(name string) {
	c.hits++
}

// RecordMiss records a cache miss
func (c *CoreDNSCache) RecordMiss(name string) {
	c.misses++
}

// GetStats returns cache statistics
func (c *CoreDNSCache) GetStats() CacheStats {
	total := c.hits + c.misses
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(c.hits) / float64(total) * 100
	}
	return CacheStats{
		Hits:    c.hits,
		Misses:  c.misses,
		HitRate: hitRate,
	}
}

// CacheStats represents cache statistics
type CacheStats struct {
	Hits    uint64
	Misses  uint64
	HitRate float64
}

// CoreDNSForwarder simulates CoreDNS forwarding
type CoreDNSForwarder struct {
	upstreams []string
	unhealthy map[string]bool
}

// NewCoreDNSForwarder creates a new forwarder
func NewCoreDNSForwarder() *CoreDNSForwarder {
	return &CoreDNSForwarder{
		upstreams: make([]string, 0),
		unhealthy: make(map[string]bool),
	}
}

// SetUpstreams sets upstream DNS servers
func (f *CoreDNSForwarder) SetUpstreams(upstreams []string) {
	f.upstreams = upstreams
}

// SelectUpstream selects an upstream server
func (f *CoreDNSForwarder) SelectUpstream(queryName string) string {
	for _, upstream := range f.upstreams {
		if !f.unhealthy[upstream] {
			return upstream
		}
	}
	// All unhealthy, return first anyway
	if len(f.upstreams) > 0 {
		return f.upstreams[0]
	}
	return ""
}

// MarkUnhealthy marks an upstream as unhealthy
func (f *CoreDNSForwarder) MarkUnhealthy(upstream string) {
	f.unhealthy[upstream] = true
}

// CoreDNSHealth tracks CoreDNS health
type CoreDNSHealth struct {
	successCount uint64
	failureCount uint64
}

// NewCoreDNSHealth creates a new health tracker
func NewCoreDNSHealth() *CoreDNSHealth {
	return &CoreDNSHealth{}
}

// IsHealthy checks if CoreDNS is healthy
func (h *CoreDNSHealth) IsHealthy() bool {
	// Need at least 3 successes to be considered healthy
	return h.successCount >= 3 && h.failureCount < h.successCount
}

// RecordCheck records a health check result
func (h *CoreDNSHealth) RecordCheck(success bool) {
	if success {
		h.successCount++
	} else {
		h.failureCount++
	}
}

// GetMetrics returns health metrics
func (h *CoreDNSHealth) GetMetrics() HealthMetrics {
	return HealthMetrics{
		TotalChecks:  h.successCount + h.failureCount,
		FailedChecks: h.failureCount,
	}
}

// HealthMetrics represents health check metrics
type HealthMetrics struct {
	TotalChecks  uint64
	FailedChecks uint64
}

// CoreDNSConfig represents CoreDNS configuration
type CoreDNSConfig struct {
	Zones   []string
	Plugins map[string]bool
}

// CoreDNSReloader handles CoreDNS config reloading
type CoreDNSReloader struct {
	currentConfig *CoreDNSConfig
}

// NewCoreDNSReloader creates a new reloader
func NewCoreDNSReloader() *CoreDNSReloader {
	return &CoreDNSReloader{}
}

// LoadConfig loads a configuration
func (r *CoreDNSReloader) LoadConfig(config CoreDNSConfig) error {
	r.currentConfig = &config
	return nil
}

// HasChanged checks if config has changed
func (r *CoreDNSReloader) HasChanged(config CoreDNSConfig) (bool, error) {
	if r.currentConfig == nil {
		return true, nil
	}

	// Check zones
	if len(config.Zones) != len(r.currentConfig.Zones) {
		return true, nil
	}
	for i, zone := range config.Zones {
		if zone != r.currentConfig.Zones[i] {
			return true, nil
		}
	}

	// Check plugins
	if len(config.Plugins) != len(r.currentConfig.Plugins) {
		return true, nil
	}
	for plugin, enabled := range config.Plugins {
		if r.currentConfig.Plugins[plugin] != enabled {
			return true, nil
		}
	}

	return false, nil
}

// Reload reloads with new config
func (r *CoreDNSReloader) Reload(config CoreDNSConfig) error {
	r.currentConfig = &config
	return nil
}

// CoreDNSLoopDetector detects DNS query loops
type CoreDNSLoopDetector struct {
	queryHistory map[string][]DNSQuery
}

// NewCoreDNSLoopDetector creates a new loop detector
func NewCoreDNSLoopDetector() *CoreDNSLoopDetector {
	return &CoreDNSLoopDetector{
		queryHistory: make(map[string][]DNSQuery),
	}
}

// DetectLoop detects if a query is looping
func (d *CoreDNSLoopDetector) DetectLoop(query DNSQuery) bool {
	key := fmt.Sprintf("%s-%d", query.Name, query.ID)
	history := d.queryHistory[key]

	// Check if we've seen this query from the original source again
	for _, prev := range history {
		if prev.Source == query.Source {
			return true // Loop detected
		}
	}

	d.queryHistory[key] = append(history, query)
	return false
}

// CoreDNSLoadBalancer provides DNS load balancing
type CoreDNSLoadBalancer struct {
	backends []string
	current  int
	down     map[string]bool
}

// NewCoreDNSLoadBalancer creates a new load balancer
func NewCoreDNSLoadBalancer() *CoreDNSLoadBalancer {
	return &CoreDNSLoadBalancer{
		backends: make([]string, 0),
		down:     make(map[string]bool),
	}
}

// AddBackend adds a backend server
func (lb *CoreDNSLoadBalancer) AddBackend(backend string) {
	lb.backends = append(lb.backends, backend)
}

// NextBackend returns the next available backend
func (lb *CoreDNSLoadBalancer) NextBackend() string {
	if len(lb.backends) == 0 {
		return ""
	}

	// Round-robin through available backends
	for i := 0; i < len(lb.backends); i++ {
		backend := lb.backends[lb.current]
		lb.current = (lb.current + 1) % len(lb.backends)

		if !lb.down[backend] {
			return backend
		}
	}

	// All down, return first anyway
	return lb.backends[0]
}

// MarkDown marks a backend as down
func (lb *CoreDNSLoadBalancer) MarkDown(backend string) {
	lb.down[backend] = true
}

// CoreDNSZoneTransfer handles zone transfers
type CoreDNSZoneTransfer struct {
	allowedNetworks []string
}

// NewCoreDNSZoneTransfer creates a new zone transfer handler
func NewCoreDNSZoneTransfer() *CoreDNSZoneTransfer {
	return &CoreDNSZoneTransfer{
		allowedNetworks: make([]string, 0),
	}
}

// IsZoneTransfer checks if query is a zone transfer
func (zt *CoreDNSZoneTransfer) IsZoneTransfer(qtype uint16) bool {
	return qtype == DNSTypeAXFR || qtype == DNSTypeIXFR
}

// SetAllowedNetworks sets allowed networks for zone transfers
func (zt *CoreDNSZoneTransfer) SetAllowedNetworks(networks []string) {
	zt.allowedNetworks = networks
}

// IsAllowed checks if an IP is allowed to do zone transfers
func (zt *CoreDNSZoneTransfer) IsAllowed(ip string) bool {
	if len(zt.allowedNetworks) == 0 {
		return false
	}

	clientIP := net.ParseIP(ip)
	if clientIP == nil {
		return false
	}

	for _, network := range zt.allowedNetworks {
		_, cidr, err := net.ParseCIDR(network)
		if err != nil {
			continue
		}
		if cidr.Contains(clientIP) {
			return true
		}
	}
	return false
}
