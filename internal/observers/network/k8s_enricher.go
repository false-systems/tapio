package network

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// K8sEnricher enriches network events with Kubernetes metadata
type K8sEnricher struct {
	logger *zap.Logger

	// Pod metadata cache
	podCache      map[string]*PodMetadata
	podCacheMutex sync.RWMutex

	// Service discovery cache
	serviceCache      map[string]*ServiceMetadata
	serviceCacheMutex sync.RWMutex

	// Cgroup to pod mapping
	cgroupCache      map[uint64]*PodMetadata
	cgroupCacheMutex sync.RWMutex

	// Stats
	enrichmentRate float64
	cacheHits      int64
	cacheMisses    int64

	// Cleanup
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// PodMetadata contains pod information
type PodMetadata struct {
	Name         string
	Namespace    string
	UID          string
	Labels       map[string]string
	Annotations  map[string]string
	ServiceName  string
	NodeName     string
	ContainerID  string
	WorkloadKind string
	WorkloadName string
	LastUpdated  time.Time
}

// ServiceMetadata contains service information
type ServiceMetadata struct {
	Name        string
	Namespace   string
	ClusterIP   string
	Ports       []ServicePort
	Selector    map[string]string
	Type        string
	LastUpdated time.Time
}

// ServicePort represents a service port
type ServicePort struct {
	Name       string
	Port       int32
	TargetPort int32
	Protocol   string
}

// NewK8sEnricher creates a new Kubernetes enricher
func NewK8sEnricher(logger *zap.Logger) (*K8sEnricher, error) {
	// Check if we're running in Kubernetes
	if _, err := os.Stat("/var/run/secrets/kubernetes.io"); os.IsNotExist(err) {
		return nil, fmt.Errorf("not running in Kubernetes cluster")
	}

	enricher := &K8sEnricher{
		logger:       logger,
		podCache:     make(map[string]*PodMetadata),
		serviceCache: make(map[string]*ServiceMetadata),
		cgroupCache:  make(map[uint64]*PodMetadata),
		stopChan:     make(chan struct{}),
	}

	// Start cache refresh goroutine
	enricher.wg.Add(1)
	go enricher.refreshCache()

	// Start cache cleanup goroutine
	enricher.wg.Add(1)
	go enricher.cleanupCache()

	return enricher, nil
}

// EnrichEvent enriches a domain event with K8s metadata
func (e *K8sEnricher) EnrichEvent(event *domain.CollectorEvent) {
	if event == nil || event.EventData.Network == nil {
		return
	}

	// Try to get pod metadata from pod UID if available
	var podMeta *PodMetadata
	if event.Metadata.Labels != nil && event.Metadata.Labels["pod_uid"] != "" {
		podMeta = e.getPodByUID(event.Metadata.Labels["pod_uid"])
	}

	// Enrich with pod metadata
	if podMeta != nil {
		e.enrichWithPodMetadata(event, podMeta)
		e.cacheHits++
	} else {
		e.cacheMisses++
	}

	// Try to enrich with service metadata
	e.enrichWithServiceMetadata(event)

	// Update enrichment rate
	e.updateEnrichmentRate()
}

// enrichWithPodMetadata adds pod metadata to event
func (e *K8sEnricher) enrichWithPodMetadata(event *domain.CollectorEvent, pod *PodMetadata) {
	if event.Metadata.Labels == nil {
		event.Metadata.Labels = make(map[string]string)
	}

	// Add pod information
	event.Metadata.Labels["k8s.pod.name"] = pod.Name
	event.Metadata.Labels["k8s.namespace"] = pod.Namespace
	event.Metadata.Labels["k8s.pod.uid"] = pod.UID
	event.Metadata.Labels["k8s.node"] = pod.NodeName

	// Add workload information
	if pod.WorkloadKind != "" {
		event.Metadata.Labels["k8s.workload.kind"] = pod.WorkloadKind
		event.Metadata.Labels["k8s.workload.name"] = pod.WorkloadName
	}

	// Add service if known
	if pod.ServiceName != "" {
		event.Metadata.Labels["k8s.service"] = pod.ServiceName
	}

	// Add selected pod labels with prefix
	for k, v := range pod.Labels {
		// Only add important labels to avoid bloat
		if isImportantLabel(k) {
			event.Metadata.Labels["k8s.label."+k] = v
		}
	}
}

// enrichWithServiceMetadata adds service metadata to event
func (e *K8sEnricher) enrichWithServiceMetadata(event *domain.CollectorEvent) {
	// Try to match destination IP to a service
	if event.EventData.Network.DstIP != "" {
		service := e.getServiceByIP(event.EventData.Network.DstIP)
		if service != nil {
			if event.Metadata.Labels == nil {
				event.Metadata.Labels = make(map[string]string)
			}
			event.Metadata.Labels["k8s.dest.service"] = service.Name
			event.Metadata.Labels["k8s.dest.namespace"] = service.Namespace
			event.Metadata.Labels["k8s.dest.service.type"] = service.Type
		}
	}
}

// getPodByCgroup retrieves pod metadata by cgroup ID
func (e *K8sEnricher) getPodByCgroup(cgroupID uint64) *PodMetadata {
	e.cgroupCacheMutex.RLock()
	defer e.cgroupCacheMutex.RUnlock()
	return e.cgroupCache[cgroupID]
}

// getPodByUID retrieves pod metadata by UID
func (e *K8sEnricher) getPodByUID(uid string) *PodMetadata {
	e.podCacheMutex.RLock()
	defer e.podCacheMutex.RUnlock()
	return e.podCache[uid]
}

// getServiceByIP retrieves service metadata by cluster IP
func (e *K8sEnricher) getServiceByIP(ip string) *ServiceMetadata {
	e.serviceCacheMutex.RLock()
	defer e.serviceCacheMutex.RUnlock()
	return e.serviceCache[ip]
}

// refreshCache periodically refreshes K8s metadata cache
func (e *K8sEnricher) refreshCache() {
	defer e.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial load
	e.loadPodMetadata()
	e.loadServiceMetadata()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.loadPodMetadata()
			e.loadServiceMetadata()
		}
	}
}

// loadPodMetadata loads pod metadata from K8s API
func (e *K8sEnricher) loadPodMetadata() {
	// In production, this would use the K8s client-go library
	// For now, we'll read from procfs and other sources

	// Read cgroup information from /proc to map processes to pods
	e.loadCgroupMappings()

	e.logger.Debug("Pod metadata cache refreshed",
		zap.Int("pods", len(e.podCache)))
}

// loadServiceMetadata loads service metadata from K8s API
func (e *K8sEnricher) loadServiceMetadata() {
	// In production, this would use the K8s client-go library
	// For now, this is a placeholder

	e.logger.Debug("Service metadata cache refreshed",
		zap.Int("services", len(e.serviceCache)))
}

// loadCgroupMappings loads cgroup to pod mappings
func (e *K8sEnricher) loadCgroupMappings() {
	// Parse /proc/*/cgroup files to extract pod information
	// This is a simplified version - production would be more robust

	// Example: /proc/1234/cgroup contains:
	// 12:memory:/kubepods/besteffort/pod-uid/container-id

	e.cgroupCacheMutex.Lock()
	defer e.cgroupCacheMutex.Unlock()

	// Clear old mappings
	e.cgroupCache = make(map[uint64]*PodMetadata)

	// In production, iterate through /proc/*/cgroup files
	// Extract pod UID and map cgroup ID to pod metadata
}

// cleanupCache removes stale entries from cache
func (e *K8sEnricher) cleanupCache() {
	defer e.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.cleanupStaleEntries()
		}
	}
}

// cleanupStaleEntries removes entries older than threshold
func (e *K8sEnricher) cleanupStaleEntries() {
	now := time.Now()
	staleThreshold := 10 * time.Minute

	// Clean pod cache
	e.podCacheMutex.Lock()
	for uid, pod := range e.podCache {
		if now.Sub(pod.LastUpdated) > staleThreshold {
			delete(e.podCache, uid)
		}
	}
	e.podCacheMutex.Unlock()

	// Clean service cache
	e.serviceCacheMutex.Lock()
	for ip, svc := range e.serviceCache {
		if now.Sub(svc.LastUpdated) > staleThreshold {
			delete(e.serviceCache, ip)
		}
	}
	e.serviceCacheMutex.Unlock()
}

// updateEnrichmentRate calculates cache hit rate
func (e *K8sEnricher) updateEnrichmentRate() {
	total := e.cacheHits + e.cacheMisses
	if total > 0 {
		e.enrichmentRate = float64(e.cacheHits) / float64(total)
	}
}

// GetEnrichmentRate returns the current enrichment rate
func (e *K8sEnricher) GetEnrichmentRate() float64 {
	return e.enrichmentRate
}

// Close shuts down the enricher
func (e *K8sEnricher) Close() {
	close(e.stopChan)
	e.wg.Wait()
	e.logger.Info("K8s enricher closed")
}

// isImportantLabel checks if a label should be included
func isImportantLabel(key string) bool {
	importantPrefixes := []string{
		"app",
		"version",
		"component",
		"tier",
		"environment",
		"team",
	}

	for _, prefix := range importantPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// ParsePodUIDFromCgroup extracts pod UID from cgroup path
func ParsePodUIDFromCgroup(cgroupPath string) string {
	// Example: /kubepods/besteffort/pod1234-5678-90ab-cdef/container-id
	parts := strings.Split(cgroupPath, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "pod") && i > 0 {
			// Extract UID from pod prefix
			uid := strings.TrimPrefix(part, "pod")
			// Remove any suffix after the UID
			if idx := strings.Index(uid, "."); idx > 0 {
				uid = uid[:idx]
			}
			return uid
		}
	}
	return ""
}

// GetContainerIDFromCgroup extracts container ID from cgroup path
func GetContainerIDFromCgroup(cgroupPath string) string {
	parts := strings.Split(cgroupPath, "/")
	if len(parts) > 0 {
		// Last part is usually the container ID
		containerID := parts[len(parts)-1]
		// Remove any prefix (docker://, containerd://)
		if idx := strings.Index(containerID, "://"); idx > 0 {
			containerID = containerID[idx+3:]
		}
		return containerID
	}
	return ""
}
