package extraction

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
	corev1 "k8s.io/api/core/v1"
)

// NetworkContextExtractor enriches events with K8s network context
type NetworkContextExtractor struct {
	cache *K8sCache
}

// ExtractNetworkContext enriches network events with K8s network topology
func (e *NetworkContextExtractor) ExtractNetworkContext(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	// Only process network events
	if event.Network == nil {
		return nil
	}

	netData := event.Network
	k8sCtx := event.K8sContext
	if k8sCtx == nil {
		k8sCtx = &domain.K8sContext{}
		event.K8sContext = k8sCtx
	}

	// Initialize network context if needed
	if event.ResourceContext == nil {
		event.ResourceContext = &domain.ResourceContext{}
	}
	if event.ResourceContext.ActualState == nil {
		event.ResourceContext.ActualState = &domain.ResourceState{}
	}
	if event.ResourceContext.ActualState.NetworkState == nil {
		event.ResourceContext.ActualState.NetworkState = &domain.NetworkState{}
	}

	// Try to correlate network data to K8s resources
	if err := e.correlateByIP(ctx, event, cache); err != nil {
		// Log but don't fail
		_ = err
	}

	// Extract service topology
	if err := e.extractServiceTopology(ctx, event, cache); err != nil {
		_ = err
	}

	// Extract network policies
	if err := e.extractNetworkPolicies(ctx, event, cache); err != nil {
		_ = err
	}

	// Add network correlation hints
	e.addNetworkCorrelationHints(event)

	return nil
}

// correlateByIP finds K8s resources by IP addresses
func (e *NetworkContextExtractor) correlateByIP(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	netData := event.Network
	k8sCtx := event.K8sContext

	// Try to find source pod by IP
	if netData.SourceIP != "" && !isExternalIP(netData.SourceIP) {
		if pod, err := cache.GetPodByIP(netData.SourceIP); err == nil && pod != nil {
			// This is the source pod
			k8sCtx.Name = pod.Name
			k8sCtx.Namespace = pod.Namespace
			k8sCtx.UID = string(pod.UID)
			k8sCtx.NodeName = pod.Spec.NodeName
			k8sCtx.Labels = pod.Labels

			// Add source pod reference
			event.CorrelationHints = append(event.CorrelationHints,
				fmt.Sprintf("source_pod:%s/%s", pod.Namespace, pod.Name))
		}
	}

	// Try to find destination pod by IP
	if netData.DestIP != "" && !isExternalIP(netData.DestIP) {
		if pod, err := cache.GetPodByIP(netData.DestIP); err == nil && pod != nil {
			// Add destination context
			event.CorrelationHints = append(event.CorrelationHints,
				fmt.Sprintf("dest_pod:%s/%s", pod.Namespace, pod.Name))

			// If we don't have a source pod, use destination as primary
			if k8sCtx.Name == "" {
				k8sCtx.Name = pod.Name
				k8sCtx.Namespace = pod.Namespace
			}
		}
	}

	return nil
}

// extractServiceTopology finds services involved in the network flow
func (e *NetworkContextExtractor) extractServiceTopology(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	netData := event.Network
	k8sCtx := event.K8sContext

	// Check if destination matches any service
	if netData.DestIP != "" && netData.DestPort != 0 {
		// Look for services with this ClusterIP
		services := e.findServicesByIP(cache, netData.DestIP)
		for _, svc := range services {
			// Check if port matches
			for _, port := range svc.Spec.Ports {
				if port.Port == int32(netData.DestPort) {
					// This is a service call
					event.CorrelationHints = append(event.CorrelationHints,
						fmt.Sprintf("service:%s/%s:%d", svc.Namespace, svc.Name, port.Port))

					// Add service as consumer
					k8sCtx.Consumers = append(k8sCtx.Consumers, domain.K8sResourceRef{
						Kind:      "Service",
						Name:      svc.Name,
						Namespace: svc.Namespace,
					})

					// Find backend pods
					e.findServiceBackends(svc, event, cache)
				}
			}
		}
	}

	return nil
}

// findServicesByIP finds services with a specific ClusterIP
func (e *NetworkContextExtractor) findServicesByIP(cache *K8sCache, ip string) []*corev1.Service {
	var services []*corev1.Service

	// Iterate through all services in cache
	for _, obj := range cache.serviceInformer.GetStore().List() {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			continue
		}

		// Check ClusterIP
		if svc.Spec.ClusterIP == ip {
			services = append(services, svc)
		}

		// Check LoadBalancer IPs
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if ingress.IP == ip {
				services = append(services, svc)
			}
		}
	}

	return services
}

// findServiceBackends finds pods backing a service
func (e *NetworkContextExtractor) findServiceBackends(svc *corev1.Service, event *domain.UnifiedEvent, cache *K8sCache) {
	// Get endpoints for the service
	endpoints, err := cache.GetEndpointsForService(svc.Namespace, svc.Name)
	if err != nil || endpoints == nil {
		return
	}

	// Extract backend pod IPs
	var backendIPs []string
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			backendIPs = append(backendIPs, addr.IP)

			// Try to find the pod
			if pod, err := cache.GetPodByIP(addr.IP); err == nil && pod != nil {
				event.CorrelationHints = append(event.CorrelationHints,
					fmt.Sprintf("backend_pod:%s/%s", pod.Namespace, pod.Name))
			}
		}
	}

	// Store backend IPs for correlation
	if len(backendIPs) > 0 {
		event.CorrelationHints = append(event.CorrelationHints,
			fmt.Sprintf("backend_ips:%s", strings.Join(backendIPs, ",")))
	}
}

// extractNetworkPolicies checks for applicable network policies
func (e *NetworkContextExtractor) extractNetworkPolicies(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	k8sCtx := event.K8sContext

	// We need pod info to check policies
	if k8sCtx.Name == "" || k8sCtx.Namespace == "" {
		return nil
	}

	pod, err := cache.GetPod(k8sCtx.Namespace, k8sCtx.Name)
	if err != nil || pod == nil {
		return nil
	}

	// TODO: Check network policies that apply to this pod
	// This would require adding NetworkPolicy informer to cache
	// For now, we'll add a hint if the namespace has network policies

	event.CorrelationHints = append(event.CorrelationHints,
		fmt.Sprintf("network_ns:%s", k8sCtx.Namespace))

	return nil
}

// addNetworkCorrelationHints adds hints for network correlation
func (e *NetworkContextExtractor) addNetworkCorrelationHints(event *domain.UnifiedEvent) {
	netData := event.Network

	// Add protocol hint
	if netData.Protocol != "" {
		event.CorrelationHints = append(event.CorrelationHints,
			fmt.Sprintf("proto:%s", strings.ToLower(netData.Protocol)))
	}

	// Add direction hint
	if netData.Direction != "" {
		event.CorrelationHints = append(event.CorrelationHints,
			fmt.Sprintf("direction:%s", netData.Direction))
	}

	// Add port-based hints for well-known services
	hints := getWellKnownPortHints(netData.DestPort)
	event.CorrelationHints = append(event.CorrelationHints, hints...)

	// Add latency-based hints
	if netData.Latency > 100*1000000 { // > 100ms
		event.CorrelationHints = append(event.CorrelationHints, "high_latency")
	}

	// Add error hints
	if netData.StatusCode >= 500 {
		event.CorrelationHints = append(event.CorrelationHints, "server_error")
	} else if netData.StatusCode >= 400 {
		event.CorrelationHints = append(event.CorrelationHints, "client_error")
	}
}

// isExternalIP checks if an IP is external to the cluster
func isExternalIP(ip string) bool {
	// Parse IP
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check if it's a private IP (RFC1918)
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fd00::/8", // IPv6 ULA
	}

	for _, block := range privateBlocks {
		_, cidr, _ := net.ParseCIDR(block)
		if cidr != nil && cidr.Contains(parsedIP) {
			return false // It's internal
		}
	}

	// Check for special addresses
	if parsedIP.IsLoopback() || parsedIP.IsLinkLocalUnicast() {
		return false
	}

	// Assume external if not in private ranges
	return true
}

// getWellKnownPortHints returns hints based on well-known ports
func getWellKnownPortHints(port uint16) []string {
	var hints []string

	switch port {
	case 53:
		hints = append(hints, "dns")
	case 80, 8080:
		hints = append(hints, "http")
	case 443, 8443:
		hints = append(hints, "https")
	case 3306:
		hints = append(hints, "mysql")
	case 5432:
		hints = append(hints, "postgres")
	case 6379:
		hints = append(hints, "redis")
	case 9200:
		hints = append(hints, "elasticsearch")
	case 27017:
		hints = append(hints, "mongodb")
	case 5672:
		hints = append(hints, "rabbitmq")
	case 9092:
		hints = append(hints, "kafka")
	case 2379, 2380:
		hints = append(hints, "etcd")
	case 6443:
		hints = append(hints, "kube-apiserver")
	case 10250:
		hints = append(hints, "kubelet")
	case 9090:
		hints = append(hints, "prometheus")
	case 3000:
		hints = append(hints, "grafana")
	}

	return hints
}

// NetworkTopology represents the K8s network topology for an event
type NetworkTopology struct {
	// Source information
	SourcePod       *PodNetworkInfo     `json:"source_pod,omitempty"`
	SourceService   *ServiceNetworkInfo `json:"source_service,omitempty"`
	SourceNamespace string              `json:"source_namespace,omitempty"`
	SourceNode      string              `json:"source_node,omitempty"`

	// Destination information
	DestPod       *PodNetworkInfo     `json:"dest_pod,omitempty"`
	DestService   *ServiceNetworkInfo `json:"dest_service,omitempty"`
	DestNamespace string              `json:"dest_namespace,omitempty"`
	DestNode      string              `json:"dest_node,omitempty"`

	// Network path
	CrossNode      bool     `json:"cross_node"`
	CrossNamespace bool     `json:"cross_namespace"`
	CrossZone      bool     `json:"cross_zone"`
	ServiceMesh    bool     `json:"service_mesh"`
	Backends       []string `json:"backends,omitempty"`
}

// PodNetworkInfo contains network-relevant pod information
type PodNetworkInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	IP        string            `json:"ip"`
	Labels    map[string]string `json:"labels"`
	Node      string            `json:"node"`
	Zone      string            `json:"zone,omitempty"`
}

// ServiceNetworkInfo contains network-relevant service information
type ServiceNetworkInfo struct {
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	ClusterIP  string            `json:"cluster_ip"`
	Type       string            `json:"type"`
	Ports      []int32           `json:"ports"`
	Selector   map[string]string `json:"selector"`
	BackendIPs []string          `json:"backend_ips,omitempty"`
}
