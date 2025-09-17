package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// K8sEnricher enriches events with Kubernetes metadata
type K8sEnricher struct {
	client kubernetes.Interface
	logger *zap.Logger
	cache  *k8sCache
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// k8sCache caches Kubernetes metadata
type k8sCache struct {
	mu   sync.RWMutex
	pods map[string]*podInfo // containerID -> podInfo
	ttl  time.Duration
}

// podInfo contains cached pod information
type podInfo struct {
	podName       string
	namespace     string
	containerName string
	serviceName   string
	deployment    string
	statefulSet   string
	daemonSet     string
	labels        map[string]string
	annotations   map[string]string
	lastUpdated   time.Time
}

// NewK8sEnricher creates a new Kubernetes enricher
func NewK8sEnricher(logger *zap.Logger) (*K8sEnricher, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Not running in cluster, K8s enrichment disabled
		return nil, fmt.Errorf("not running in Kubernetes cluster: %w", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating K8s client: %w", err)
	}

	enricher := &K8sEnricher{
		client: client,
		logger: logger,
		cache: &k8sCache{
			pods: make(map[string]*podInfo),
			ttl:  5 * time.Minute,
		},
		stopCh: make(chan struct{}),
	}

	// Start cache cleaner
	enricher.wg.Add(1)
	go enricher.cleanCache()

	return enricher, nil
}

// EnrichEvent adds Kubernetes metadata to an event
func (k *K8sEnricher) EnrichEvent(event *domain.CollectorEvent) {
	if event == nil || event.EventData.Process == nil {
		return
	}

	// Extract container ID from cgroup path
	containerID := k.extractContainerIDFromPath(event.EventData.Process.CgroupPath)
	if containerID == "" {
		return
	}

	// Check cache first
	if podInfo := k.getCachedPodInfo(containerID); podInfo != nil {
		k.enrichWithPodInfo(event, podInfo)
		return
	}

	// Query Kubernetes API
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	podInfo, err := k.queryPodInfo(ctx, containerID)
	if err != nil {
		k.logger.Debug("Failed to get pod info",
			zap.String("container_id", containerID),
			zap.Error(err))
		return
	}

	// Cache the result
	k.cachePodInfo(containerID, podInfo)

	// Enrich event
	k.enrichWithPodInfo(event, podInfo)
}

// extractContainerIDFromPath extracts container ID from cgroup path
func (k *K8sEnricher) extractContainerIDFromPath(cgroupPath string) string {
	if cgroupPath == "" {
		return ""
	}

	// Example cgroup paths:
	// /kubepods/besteffort/pod<pod-uid>/<container-id>
	// /docker/<container-id>
	// /containerd/<container-id>
	// systemd: /system.slice/docker-<container-id>.scope

	// Try docker pattern
	if idx := strings.Index(cgroupPath, "/docker/"); idx != -1 {
		containerID := cgroupPath[idx+8:] // Skip "/docker/"
		if slashIdx := strings.Index(containerID, "/"); slashIdx != -1 {
			containerID = containerID[:slashIdx]
		}
		if len(containerID) >= 12 {
			return containerID[:12] // Use first 12 chars of container ID
		}
	}

	// Try containerd pattern
	if idx := strings.Index(cgroupPath, "/containerd/"); idx != -1 {
		containerID := cgroupPath[idx+12:] // Skip "/containerd/"
		if slashIdx := strings.Index(containerID, "/"); slashIdx != -1 {
			containerID = containerID[:slashIdx]
		}
		if len(containerID) >= 12 {
			return containerID[:12]
		}
	}

	// Try systemd docker pattern
	if idx := strings.Index(cgroupPath, "docker-"); idx != -1 {
		containerID := cgroupPath[idx+7:] // Skip "docker-"
		if dotIdx := strings.Index(containerID, ".scope"); dotIdx != -1 {
			containerID = containerID[:dotIdx]
		}
		if len(containerID) >= 12 {
			return containerID[:12]
		}
	}

	// Try kubernetes pattern (last component after pod UID)
	if strings.Contains(cgroupPath, "/kubepods/") {
		parts := strings.Split(cgroupPath, "/")
		if len(parts) > 0 {
			lastPart := parts[len(parts)-1]
			if len(lastPart) >= 12 {
				return lastPart[:12]
			}
		}
	}

	return ""
}

// getCachedPodInfo retrieves pod info from cache
func (k *K8sEnricher) getCachedPodInfo(containerID string) *podInfo {
	k.cache.mu.RLock()
	defer k.cache.mu.RUnlock()

	info, exists := k.cache.pods[containerID]
	if !exists {
		return nil
	}

	// Check if cache entry is still valid
	if time.Since(info.lastUpdated) > k.cache.ttl {
		return nil
	}

	return info
}

// cachePodInfo stores pod info in cache
func (k *K8sEnricher) cachePodInfo(containerID string, info *podInfo) {
	k.cache.mu.Lock()
	defer k.cache.mu.Unlock()

	info.lastUpdated = time.Now()
	k.cache.pods[containerID] = info
}

// queryPodInfo queries Kubernetes API for pod information
func (k *K8sEnricher) queryPodInfo(ctx context.Context, containerID string) (*podInfo, error) {
	// List all pods and find the one with matching container
	pods, err := k.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Status.ContainerStatuses {
			if strings.Contains(container.ContainerID, containerID) {
				info := &podInfo{
					podName:       pod.Name,
					namespace:     pod.Namespace,
					containerName: container.Name,
					labels:        pod.Labels,
					annotations:   pod.Annotations,
				}

				// Extract ownership info
				for _, owner := range pod.OwnerReferences {
					switch owner.Kind {
					case "Deployment":
						info.deployment = owner.Name
					case "StatefulSet":
						info.statefulSet = owner.Name
					case "DaemonSet":
						info.daemonSet = owner.Name
					case "ReplicaSet":
						// Get deployment from ReplicaSet
						rs, err := k.client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
						if err == nil && len(rs.OwnerReferences) > 0 {
							for _, rsOwner := range rs.OwnerReferences {
								if rsOwner.Kind == "Deployment" {
									info.deployment = rsOwner.Name
									break
								}
							}
						}
					}
				}

				// Get service info
				services, err := k.client.CoreV1().Services(pod.Namespace).List(ctx, metav1.ListOptions{})
				if err == nil {
					for _, svc := range services.Items {
						if matchesSelector(pod.Labels, svc.Spec.Selector) {
							info.serviceName = svc.Name
							break
						}
					}
				}

				return info, nil
			}
		}
	}

	return nil, fmt.Errorf("pod not found for container %s", containerID)
}

// enrichWithPodInfo adds pod information to event
func (k *K8sEnricher) enrichWithPodInfo(event *domain.CollectorEvent, info *podInfo) {
	// Add container data if we have container info
	if event.EventData.Container == nil && info.containerName != "" {
		event.EventData.Container = &domain.ContainerData{
			ContainerID: info.containerName, // Use container name as ID for now
		}
	}

	// Add to metadata - this is where K8s-specific info belongs
	if event.Metadata.Labels == nil {
		event.Metadata.Labels = make(map[string]string)
	}

	event.Metadata.Labels["pod"] = info.podName
	event.Metadata.Labels["namespace"] = info.namespace
	event.Metadata.Labels["container"] = info.containerName

	if info.serviceName != "" {
		event.Metadata.Labels["service"] = info.serviceName
	}
	if info.deployment != "" {
		event.Metadata.Labels["deployment"] = info.deployment
	}
	if info.statefulSet != "" {
		event.Metadata.Labels["statefulset"] = info.statefulSet
	}
	if info.daemonSet != "" {
		event.Metadata.Labels["daemonset"] = info.daemonSet
	}

	// Add selected pod labels
	for k, v := range info.labels {
		if k == "app" || k == "version" || strings.HasPrefix(k, "app.kubernetes.io/") {
			event.Metadata.Labels["k8s."+k] = v
		}
	}
}

// matchesSelector checks if pod labels match service selector
func matchesSelector(podLabels, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}

	for k, v := range selector {
		if podLabels[k] != v {
			return false
		}
	}

	return true
}

// cleanCache periodically removes expired entries
func (k *K8sEnricher) cleanCache() {
	defer k.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-k.stopCh:
			return
		case <-ticker.C:
			k.cache.mu.Lock()
			now := time.Now()
			for id, info := range k.cache.pods {
				if now.Sub(info.lastUpdated) > k.cache.ttl {
					delete(k.cache.pods, id)
				}
			}
			k.cache.mu.Unlock()
		}
	}
}

// Close stops the enricher
func (k *K8sEnricher) Close() {
	close(k.stopCh)
	k.wg.Wait()
}
