package dns

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectCoreDNSProcess(t *testing.T) {
	tests := []struct {
		name      string
		process   ProcessInfo
		isCoreDNS bool
	}{
		{
			name: "coredns process",
			process: ProcessInfo{
				PID:     1234,
				Name:    "coredns",
				Cmdline: "/usr/bin/coredns -conf /etc/coredns/Corefile",
			},
			isCoreDNS: true,
		},
		{
			name: "kube-dns process",
			process: ProcessInfo{
				PID:     5678,
				Name:    "kube-dns",
				Cmdline: "/kube-dns",
			},
			isCoreDNS: true,
		},
		{
			name: "systemd-resolved",
			process: ProcessInfo{
				PID:     999,
				Name:    "systemd-resolved",
				Cmdline: "/lib/systemd/systemd-resolved",
			},
			isCoreDNS: false,
		},
		{
			name: "dnsmasq",
			process: ProcessInfo{
				PID:     888,
				Name:    "dnsmasq",
				Cmdline: "dnsmasq --conf-file=/etc/dnsmasq.conf",
			},
			isCoreDNS: false,
		},
	}

	detector := NewCoreDNSProcessDetector()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.IsCoreDNS(tt.process)
			assert.Equal(t, tt.isCoreDNS, result)

			if tt.isCoreDNS {
				detector.RegisterPID(tt.process.PID)
				assert.True(t, detector.IsPIDRegistered(tt.process.PID))
			}
		})
	}
}

func TestParseCoreDNSMetrics(t *testing.T) {
	metricsText := `
# HELP coredns_dns_request_count_total total query count.
# TYPE coredns_dns_request_count_total counter
coredns_dns_request_count_total{server="dns://:53",type="A",zone="cluster.local."} 42
coredns_dns_request_count_total{server="dns://:53",type="AAAA",zone="cluster.local."} 28

# HELP coredns_dns_request_duration_seconds duration to process each query.
# TYPE coredns_dns_request_duration_seconds histogram
coredns_dns_request_duration_seconds_bucket{server="dns://:53",type="A",zone="cluster.local.",le="0.00025"} 35
coredns_dns_request_duration_seconds_bucket{server="dns://:53",type="A",zone="cluster.local.",le="0.0005"} 40
coredns_dns_request_duration_seconds_sum{server="dns://:53",type="A",zone="cluster.local."} 0.123
coredns_dns_request_duration_seconds_count{server="dns://:53",type="A",zone="cluster.local."} 42

# HELP coredns_cache_hits_total The count of cache hits.
# TYPE coredns_cache_hits_total counter
coredns_cache_hits_total{server="dns://:53",type="success"} 100
coredns_cache_hits_total{server="dns://:53",type="denial"} 10

# HELP coredns_cache_misses_total The count of cache misses.
# TYPE coredns_cache_misses_total counter
coredns_cache_misses_total{server="dns://:53"} 50

# HELP coredns_plugin_enabled indicates whether a plugin is enabled on per server basis.
# TYPE coredns_plugin_enabled gauge
coredns_plugin_enabled{name="cache",server="dns://:53"} 1
coredns_plugin_enabled{name="kubernetes",server="dns://:53"} 1
coredns_plugin_enabled{name="forward",server="dns://:53"} 1
`

	parser := NewCoreDNSMetricsParser()
	metrics, err := parser.Parse(metricsText)
	require.NoError(t, err)

	assert.Equal(t, uint64(42), metrics.QueryCount["A"])
	assert.Equal(t, uint64(28), metrics.QueryCount["AAAA"])
	assert.Equal(t, uint64(100), metrics.CacheHits)
	assert.Equal(t, uint64(50), metrics.CacheMisses)
	assert.True(t, metrics.PluginsEnabled["cache"])
	assert.True(t, metrics.PluginsEnabled["kubernetes"])
	assert.True(t, metrics.PluginsEnabled["forward"])
}

func TestDetectK8sServiceQuery(t *testing.T) {
	tests := []struct {
		name       string
		queryName  string
		expected   K8sServiceInfo
		isK8sQuery bool
	}{
		{
			name:      "standard service query",
			queryName: "nginx-service.production.svc.cluster.local",
			expected: K8sServiceInfo{
				Service:   "nginx-service",
				Namespace: "production",
				Type:      "svc",
			},
			isK8sQuery: true,
		},
		{
			name:      "pod IP query",
			queryName: "10-244-1-5.production.pod.cluster.local",
			expected: K8sServiceInfo{
				PodIP:     "10.244.1.5",
				Namespace: "production",
				Type:      "pod",
			},
			isK8sQuery: true,
		},
		{
			name:      "headless service with port",
			queryName: "_http._tcp.nginx.default.svc.cluster.local",
			expected: K8sServiceInfo{
				Service:   "nginx",
				Namespace: "default",
				Port:      "http",
				Protocol:  "tcp",
				Type:      "svc",
			},
			isK8sQuery: true,
		},
		{
			name:      "statefulset pod",
			queryName: "web-0.nginx.default.svc.cluster.local",
			expected: K8sServiceInfo{
				Service:   "nginx",
				Namespace: "default",
				PodName:   "web-0",
				Type:      "svc",
			},
			isK8sQuery: true,
		},
		{
			name:      "short service name",
			queryName: "nginx.default",
			expected: K8sServiceInfo{
				Service:   "nginx",
				Namespace: "default",
				Type:      "short",
			},
			isK8sQuery: true,
		},
		{
			name:       "external domain",
			queryName:  "google.com",
			expected:   K8sServiceInfo{},
			isK8sQuery: false,
		},
		{
			name:      "kubernetes API service",
			queryName: "kubernetes.default.svc.cluster.local",
			expected: K8sServiceInfo{
				Service:         "kubernetes",
				Namespace:       "default",
				Type:            "svc",
				IsSystemService: true,
			},
			isK8sQuery: true,
		},
	}

	detector := NewK8sServiceDetector()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, isK8s := detector.Detect(tt.queryName)
			assert.Equal(t, tt.isK8sQuery, isK8s)

			if isK8s {
				assert.Equal(t, tt.expected.Service, result.Service)
				assert.Equal(t, tt.expected.Namespace, result.Namespace)
				assert.Equal(t, tt.expected.Type, result.Type)

				if tt.expected.PodIP != "" {
					assert.Equal(t, tt.expected.PodIP, result.PodIP)
				}
				if tt.expected.Port != "" {
					assert.Equal(t, tt.expected.Port, result.Port)
					assert.Equal(t, tt.expected.Protocol, result.Protocol)
				}
			}
		})
	}
}

func TestCoreDNSPluginChain(t *testing.T) {
	// Test simulating CoreDNS plugin chain execution
	plugins := []CoreDNSPlugin{
		{Name: "errors", Order: 1},
		{Name: "health", Order: 2},
		{Name: "kubernetes", Order: 3},
		{Name: "prometheus", Order: 4},
		{Name: "forward", Order: 5},
		{Name: "cache", Order: 6},
		{Name: "loop", Order: 7},
		{Name: "reload", Order: 8},
		{Name: "loadbalance", Order: 9},
	}

	chain := NewCoreDNSPluginChain(plugins)

	// Test query flow through plugins
	query := DNSQuery{
		Name: "service.default.svc.cluster.local",
		Type: DNSTypeA,
	}

	events := chain.ProcessQuery(query)

	// Should generate events for each plugin that processes the query
	assert.Greater(t, len(events), 0)

	// Check kubernetes plugin handled it
	var kubernetesProcessed bool
	for _, event := range events {
		if event.Plugin == "kubernetes" {
			kubernetesProcessed = true
			assert.Equal(t, "service", event.K8sService)
			assert.Equal(t, "default", event.K8sNamespace)
		}
	}
	assert.True(t, kubernetesProcessed)
}

func TestCoreDNSCacheMetrics(t *testing.T) {
	cache := NewCoreDNSCache()

	// Simulate cache operations
	queries := []struct {
		name   string
		cached bool
	}{
		{"cached.example.com", true},
		{"new.example.com", false},
		{"cached.example.com", true}, // Hit
		{"another.example.com", false},
		{"cached.example.com", true}, // Hit again
	}

	for _, q := range queries {
		if q.cached {
			cache.RecordHit(q.name)
		} else {
			cache.RecordMiss(q.name)
		}
	}

	stats := cache.GetStats()
	assert.Equal(t, uint64(3), stats.Hits)
	assert.Equal(t, uint64(2), stats.Misses)
	assert.InDelta(t, 60.0, stats.HitRate, 1.0) // ~60% hit rate
}

func TestCoreDNSForwarding(t *testing.T) {
	forwarder := NewCoreDNSForwarder()

	// Configure upstream servers
	upstreams := []string{
		"8.8.8.8:53",
		"8.8.4.4:53",
		"1.1.1.1:53",
	}

	forwarder.SetUpstreams(upstreams)

	// Test upstream selection
	tests := []struct {
		queryName string
		expected  string
	}{
		{"external.com", "8.8.8.8:53"},   // First upstream
		{"google.com", "8.8.8.8:53"},     // Same if healthy
		{"cloudflare.com", "8.8.8.8:53"}, // Consistent selection
	}

	for _, tt := range tests {
		t.Run(tt.queryName, func(t *testing.T) {
			upstream := forwarder.SelectUpstream(tt.queryName)
			assert.Contains(t, upstreams, upstream)
		})
	}

	// Simulate upstream failure
	forwarder.MarkUnhealthy("8.8.8.8:53")
	upstream := forwarder.SelectUpstream("test.com")
	assert.NotEqual(t, "8.8.8.8:53", upstream)
}

func TestCoreDNSHealthCheck(t *testing.T) {
	health := NewCoreDNSHealth()

	// Should start unhealthy
	assert.False(t, health.IsHealthy())

	// Simulate successful health checks
	for i := 0; i < 3; i++ {
		health.RecordCheck(true)
		time.Sleep(10 * time.Millisecond)
	}

	assert.True(t, health.IsHealthy())

	// Simulate failures
	for i := 0; i < 5; i++ {
		health.RecordCheck(false)
	}

	assert.False(t, health.IsHealthy())

	// Get health metrics
	metrics := health.GetMetrics()
	assert.Greater(t, metrics.TotalChecks, uint64(0))
	assert.Greater(t, metrics.FailedChecks, uint64(0))
}

func TestCoreDNSReload(t *testing.T) {
	reloader := NewCoreDNSReloader()

	// Initial config
	config := CoreDNSConfig{
		Zones: []string{"cluster.local", "example.com"},
		Plugins: map[string]bool{
			"kubernetes": true,
			"forward":    true,
			"cache":      true,
		},
	}

	err := reloader.LoadConfig(config)
	require.NoError(t, err)

	// Modify config
	config.Zones = append(config.Zones, "new.zone")
	config.Plugins["log"] = true

	// Should detect changes
	changed, err := reloader.HasChanged(config)
	require.NoError(t, err)
	assert.True(t, changed)

	// Reload
	err = reloader.Reload(config)
	require.NoError(t, err)

	// Should not detect changes now
	changed, err = reloader.HasChanged(config)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestCoreDNSLoop(t *testing.T) {
	detector := NewCoreDNSLoopDetector()

	// Simulate query loop
	queries := []DNSQuery{
		{Name: "loop.example.com", ID: 1, Source: "10.0.0.1"},
		{Name: "loop.example.com", ID: 1, Source: "10.0.0.2"}, // Forwarded
		{Name: "loop.example.com", ID: 1, Source: "10.0.0.3"}, // Forwarded again
		{Name: "loop.example.com", ID: 1, Source: "10.0.0.1"}, // Back to original
	}

	var loopDetected bool
	for _, q := range queries {
		if detector.DetectLoop(q) {
			loopDetected = true
			break
		}
	}

	assert.True(t, loopDetected, "Should detect query loop")
}

func TestCoreDNSLoadBalance(t *testing.T) {
	lb := NewCoreDNSLoadBalancer()

	// Add backends
	backends := []string{
		"backend1:53",
		"backend2:53",
		"backend3:53",
	}

	for _, b := range backends {
		lb.AddBackend(b)
	}

	// Test round-robin distribution
	distribution := make(map[string]int)
	for i := 0; i < 300; i++ {
		backend := lb.NextBackend()
		distribution[backend]++
	}

	// Should be roughly equal distribution
	for _, count := range distribution {
		assert.InDelta(t, 100, count, 20, "Should have roughly equal distribution")
	}

	// Test with backend failure
	lb.MarkDown("backend1:53")

	// Should not select down backend
	for i := 0; i < 100; i++ {
		backend := lb.NextBackend()
		assert.NotEqual(t, "backend1:53", backend)
	}
}

func TestCoreDNSZoneTransfer(t *testing.T) {
	// Test AXFR/IXFR handling
	transfer := NewCoreDNSZoneTransfer()

	// Should detect zone transfer queries
	tests := []struct {
		qtype      uint16
		isTransfer bool
	}{
		{DNSTypeAXFR, true},
		{DNSTypeIXFR, true},
		{DNSTypeA, false},
		{DNSTypeAAAA, false},
	}

	for _, tt := range tests {
		result := transfer.IsZoneTransfer(tt.qtype)
		assert.Equal(t, tt.isTransfer, result)
	}

	// Should block unauthorized transfers
	allowed := []string{"10.0.0.0/24", "192.168.1.0/24"}
	transfer.SetAllowedNetworks(allowed)

	assert.True(t, transfer.IsAllowed("10.0.0.5"))
	assert.True(t, transfer.IsAllowed("192.168.1.100"))
	assert.False(t, transfer.IsAllowed("1.2.3.4"))
}
