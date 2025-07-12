package diagnostics

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/yairfalse/tapio/pkg/ebpf"
)

// HealthChecker performs comprehensive health checks on all dependencies
type HealthChecker struct {
	kubeClient  kubernetes.Interface
	kubeConfig  *rest.Config
	ebpfMonitor ebpf.Monitor

	mu        sync.RWMutex
	lastCheck time.Time
	results   map[string]*ComponentHealth
}

// ComponentHealth represents the health status of a component
type ComponentHealth struct {
	Name            string
	Healthy         bool
	Message         string
	LastChecked     time.Time
	ResponseTime    time.Duration
	Details         map[string]interface{}
	Recommendations []string
}

// HealthReport contains the overall system health status
type HealthReport struct {
	Timestamp     time.Time
	OverallHealth string // "healthy", "degraded", "unhealthy"
	Components    map[string]*ComponentHealth
	Diagnostics   []string
	Actions       []string
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(kubeClient kubernetes.Interface, kubeConfig *rest.Config, ebpfMonitor ebpf.Monitor) *HealthChecker {
	return &HealthChecker{
		kubeClient:  kubeClient,
		kubeConfig:  kubeConfig,
		ebpfMonitor: ebpfMonitor,
		results:     make(map[string]*ComponentHealth),
	}
}

// RunHealthCheck performs a comprehensive health check
func (hc *HealthChecker) RunHealthCheck(ctx context.Context) (*HealthReport, error) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	report := &HealthReport{
		Timestamp:   time.Now(),
		Components:  make(map[string]*ComponentHealth),
		Diagnostics: make([]string, 0),
		Actions:     make([]string, 0),
	}

	// Check Kubernetes API connectivity
	kubeHealth := hc.checkKubernetesAPI(ctx)
	report.Components["kubernetes-api"] = kubeHealth

	// Check cluster nodes
	nodesHealth := hc.checkClusterNodes(ctx)
	report.Components["cluster-nodes"] = nodesHealth

	// Check eBPF availability
	ebpfHealth := hc.checkEBPF()
	report.Components["ebpf-monitoring"] = ebpfHealth

	// Check network connectivity
	networkHealth := hc.checkNetworkConnectivity(ctx)
	report.Components["network"] = networkHealth

	// Check DNS resolution
	dnsHealth := hc.checkDNS(ctx)
	report.Components["dns"] = dnsHealth

	// Determine overall health
	report.OverallHealth = hc.calculateOverallHealth(report.Components)

	// Generate diagnostics and recommendations
	hc.generateDiagnostics(report)

	// Cache results
	hc.results = report.Components
	hc.lastCheck = time.Now()

	return report, nil
}

// checkKubernetesAPI checks Kubernetes API server connectivity
func (hc *HealthChecker) checkKubernetesAPI(ctx context.Context) *ComponentHealth {
	health := &ComponentHealth{
		Name:            "Kubernetes API",
		LastChecked:     time.Now(),
		Details:         make(map[string]interface{}),
		Recommendations: make([]string, 0),
	}

	start := time.Now()

	// Try to get API server version
	version, err := hc.kubeClient.Discovery().ServerVersion()
	health.ResponseTime = time.Since(start)

	if err != nil {
		health.Healthy = false
		health.Message = fmt.Sprintf("Cannot connect to Kubernetes API: %v", err)
		health.Details["error"] = err.Error()

		// Provide specific recommendations based on error
		if isConnectionRefused(err) {
			health.Recommendations = append(health.Recommendations,
				"Ensure Kubernetes cluster is running",
				"Check if kubectl works: kubectl cluster-info",
				"Verify kubeconfig is correct: kubectl config view")
		} else if isTimeout(err) {
			health.Recommendations = append(health.Recommendations,
				"Check network connectivity to cluster",
				"Verify firewall rules allow API server access",
				"Increase timeout values if cluster is slow")
		}
		return health
	}

	health.Healthy = true
	health.Message = fmt.Sprintf("Connected to Kubernetes %s", version.GitVersion)
	health.Details["version"] = version.GitVersion
	health.Details["platform"] = version.Platform

	// Check response time
	if health.ResponseTime > 5*time.Second {
		health.Message += " (slow response)"
		health.Recommendations = append(health.Recommendations,
			"API server response is slow, check cluster load",
			"Consider scaling API server replicas")
	}

	return health
}

// checkClusterNodes checks the health of cluster nodes
func (hc *HealthChecker) checkClusterNodes(ctx context.Context) *ComponentHealth {
	health := &ComponentHealth{
		Name:            "Cluster Nodes",
		LastChecked:     time.Now(),
		Details:         make(map[string]interface{}),
		Recommendations: make([]string, 0),
	}

	if hc.kubeClient == nil {
		health.Healthy = false
		health.Message = "Cannot check nodes: Kubernetes client not available"
		return health
	}

	nodeList, err := hc.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		health.Healthy = false
		health.Message = fmt.Sprintf("Failed to list nodes: %v", err)
		return health
	}

	totalNodes := len(nodeList.Items)
	readyNodes := 0
	nodeStatuses := make(map[string]string)

	for _, node := range nodeList.Items {
		nodeReady := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				nodeReady = true
				readyNodes++
				break
			}
		}

		status := "NotReady"
		if nodeReady {
			status = "Ready"
		}
		nodeStatuses[node.Name] = status
	}

	health.Details["total_nodes"] = totalNodes
	health.Details["ready_nodes"] = readyNodes
	health.Details["node_statuses"] = nodeStatuses

	if readyNodes == 0 {
		health.Healthy = false
		health.Message = "No nodes are ready"
		health.Recommendations = append(health.Recommendations,
			"Check node status with: kubectl get nodes",
			"Review node logs for errors",
			"Ensure nodes have sufficient resources")
	} else if readyNodes < totalNodes {
		health.Healthy = true // Degraded but functional
		health.Message = fmt.Sprintf("%d of %d nodes are ready", readyNodes, totalNodes)
		health.Recommendations = append(health.Recommendations,
			"Some nodes are not ready, check with: kubectl describe node <node-name>",
			"Review events: kubectl get events --all-namespaces")
	} else {
		health.Healthy = true
		health.Message = fmt.Sprintf("All %d nodes are ready", totalNodes)
	}

	return health
}

// checkEBPF checks eBPF monitoring availability
func (hc *HealthChecker) checkEBPF() *ComponentHealth {
	health := &ComponentHealth{
		Name:            "eBPF Monitoring",
		LastChecked:     time.Now(),
		Details:         make(map[string]interface{}),
		Recommendations: make([]string, 0),
	}

	if hc.ebpfMonitor == nil {
		health.Healthy = false
		health.Message = "eBPF monitor not initialized"
		return health
	}

	// Get detailed eBPF status
	ebpfStatus := ebpf.GetDetailedStatus()
	for k, v := range ebpfStatus {
		health.Details[k] = v
	}

	if hc.ebpfMonitor.IsAvailable() {
		health.Healthy = true
		health.Message = "eBPF monitoring is available"

		// Check if actually running
		if err := hc.ebpfMonitor.GetLastError(); err != nil {
			health.Message += fmt.Sprintf(" (last error: %v)", err)
		}
	} else {
		health.Healthy = false
		health.Message = ebpf.GetAvailabilityStatus()

		// Add recommendations from eBPF availability check
		if recs, ok := ebpfStatus["recommendations"].([]string); ok {
			health.Recommendations = append(health.Recommendations, recs...)
		}
	}

	return health
}

// checkNetworkConnectivity checks basic network connectivity
func (hc *HealthChecker) checkNetworkConnectivity(ctx context.Context) *ComponentHealth {
	health := &ComponentHealth{
		Name:            "Network Connectivity",
		LastChecked:     time.Now(),
		Details:         make(map[string]interface{}),
		Recommendations: make([]string, 0),
	}

	// Check if we can reach common endpoints
	endpoints := []string{
		"8.8.8.8:53", // Google DNS
		"1.1.1.1:53", // Cloudflare DNS
	}

	reachable := 0
	for _, endpoint := range endpoints {
		conn, err := net.DialTimeout("tcp", endpoint, 3*time.Second)
		if err == nil {
			conn.Close()
			reachable++
		}
	}

	health.Details["endpoints_checked"] = len(endpoints)
	health.Details["endpoints_reachable"] = reachable

	if reachable == 0 {
		health.Healthy = false
		health.Message = "No external network connectivity"
		health.Recommendations = append(health.Recommendations,
			"Check network configuration",
			"Verify firewall rules",
			"Check if running in air-gapped environment")
	} else if reachable < len(endpoints) {
		health.Healthy = true
		health.Message = "Partial network connectivity"
		health.Recommendations = append(health.Recommendations,
			"Some endpoints unreachable, check network policies")
	} else {
		health.Healthy = true
		health.Message = "Network connectivity is good"
	}

	return health
}

// checkDNS checks DNS resolution
func (hc *HealthChecker) checkDNS(ctx context.Context) *ComponentHealth {
	health := &ComponentHealth{
		Name:            "DNS Resolution",
		LastChecked:     time.Now(),
		Details:         make(map[string]interface{}),
		Recommendations: make([]string, 0),
	}

	// Try to resolve some common domains
	domains := []string{
		"kubernetes.default.svc.cluster.local",
		"google.com",
	}

	resolved := 0
	for _, domain := range domains {
		ips, err := net.LookupHost(domain)
		if err == nil && len(ips) > 0 {
			resolved++
			health.Details[domain] = ips[0]
		} else {
			health.Details[domain] = "failed"
		}
	}

	health.Details["domains_checked"] = len(domains)
	health.Details["domains_resolved"] = resolved

	if resolved == 0 {
		health.Healthy = false
		health.Message = "DNS resolution is not working"
		health.Recommendations = append(health.Recommendations,
			"Check DNS configuration in /etc/resolv.conf",
			"Verify CoreDNS is running: kubectl get pods -n kube-system | grep coredns",
			"Check DNS service: kubectl get svc -n kube-system | grep dns")
	} else if resolved < len(domains) {
		health.Healthy = true
		health.Message = "DNS partially working"
		if health.Details["kubernetes.default.svc.cluster.local"] == "failed" {
			health.Recommendations = append(health.Recommendations,
				"Cluster DNS not working, check CoreDNS logs")
		}
	} else {
		health.Healthy = true
		health.Message = "DNS resolution is working"
	}

	return health
}

// calculateOverallHealth determines the overall system health
func (hc *HealthChecker) calculateOverallHealth(components map[string]*ComponentHealth) string {
	unhealthy := 0
	total := len(components)

	for _, comp := range components {
		if !comp.Healthy {
			unhealthy++
		}
	}

	if unhealthy == 0 {
		return "healthy"
	} else if unhealthy < total/2 {
		return "degraded"
	}
	return "unhealthy"
}

// generateDiagnostics creates diagnostic messages and recommended actions
func (hc *HealthChecker) generateDiagnostics(report *HealthReport) {
	// Check critical components
	if kubeHealth, ok := report.Components["kubernetes-api"]; ok && !kubeHealth.Healthy {
		report.Diagnostics = append(report.Diagnostics,
			"❌ Kubernetes API is not accessible - this is critical for operation")
		report.Actions = append(report.Actions,
			"Fix Kubernetes connectivity before proceeding")
	}

	// Check for degraded components
	degradedCount := 0
	for name, comp := range report.Components {
		if !comp.Healthy {
			degradedCount++
			report.Diagnostics = append(report.Diagnostics,
				fmt.Sprintf("⚠️  %s is unhealthy: %s", name, comp.Message))
		}
	}

	// Overall status message
	switch report.OverallHealth {
	case "healthy":
		report.Diagnostics = append(report.Diagnostics,
			"✅ All systems are operational")
	case "degraded":
		report.Diagnostics = append(report.Diagnostics,
			fmt.Sprintf("⚠️  System is degraded with %d components unhealthy", degradedCount))
		report.Actions = append(report.Actions,
			"Review component recommendations for remediation steps")
	case "unhealthy":
		report.Diagnostics = append(report.Diagnostics,
			fmt.Sprintf("❌ System is unhealthy with %d components down", degradedCount))
		report.Actions = append(report.Actions,
			"Critical issues detected - immediate action required")
	}
}

// GetQuickDiagnostics returns a quick health summary
func (hc *HealthChecker) GetQuickDiagnostics() map[string]interface{} {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	summary := map[string]interface{}{
		"last_check": hc.lastCheck,
		"components": make(map[string]bool),
	}

	for name, health := range hc.results {
		summary["components"].(map[string]bool)[name] = health.Healthy
	}

	return summary
}

// Helper functions
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connect: connection refused")
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "context deadline exceeded")
}
