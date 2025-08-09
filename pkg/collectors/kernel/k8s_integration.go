package kernel

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sIntegration handles Kubernetes API integration for map population
type K8sIntegration struct {
	client    kubernetes.Interface
	logger    *zap.Logger
	collector *ModularCollector
	ctx       context.Context
	cancel    context.CancelFunc
	watchers  []watch.Interface
	mu        sync.RWMutex
}

// NewK8sIntegration creates a new Kubernetes integration
func NewK8sIntegration(collector *ModularCollector, logger *zap.Logger) (*K8sIntegration, error) {
	// Try in-cluster config first, then local kubeconfig
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback to local kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load Kubernetes config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &K8sIntegration{
		client:    client,
		logger:    logger,
		collector: collector,
		watchers:  make([]watch.Interface, 0),
	}, nil
}

// Start starts the Kubernetes integration
func (k *K8sIntegration) Start(ctx context.Context) error {
	k.ctx, k.cancel = context.WithCancel(ctx)

	// Start watchers for different resource types
	if err := k.startPodWatcher(); err != nil {
		k.logger.Error("Failed to start pod watcher", zap.Error(err))
		return err
	}

	if err := k.startServiceWatcher(); err != nil {
		k.logger.Error("Failed to start service watcher", zap.Error(err))
		return err
	}

	if err := k.startConfigMapWatcher(); err != nil {
		k.logger.Error("Failed to start configmap watcher", zap.Error(err))
		return err
	}

	if err := k.startSecretWatcher(); err != nil {
		k.logger.Error("Failed to start secret watcher", zap.Error(err))
		return err
	}

	k.logger.Info("Kubernetes integration started")
	return nil
}

// Stop stops the Kubernetes integration
func (k *K8sIntegration) Stop() error {
	if k.cancel != nil {
		k.cancel()
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	for _, w := range k.watchers {
		w.Stop()
	}
	k.watchers = nil

	k.logger.Info("Kubernetes integration stopped")
	return nil
}

// startPodWatcher starts watching Pod resources
func (k *K8sIntegration) startPodWatcher() error {
	watcher, err := k.client.CoreV1().Pods("").Watch(k.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	k.mu.Lock()
	k.watchers = append(k.watchers, watcher)
	k.mu.Unlock()

	go func() {
		defer watcher.Stop()
		for {
			select {
			case event, ok := <-watcher.ResultChan():
				if !ok {
					k.logger.Debug("Pod watcher channel closed")
					return
				}
				k.handlePodEvent(event)
			case <-k.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// handlePodEvent processes Pod watch events
func (k *K8sIntegration) handlePodEvent(event watch.Event) {
	pod, ok := event.Object.(*v1.Pod)
	if !ok {
		return
	}

	switch event.Type {
	case watch.Added, watch.Modified:
		k.updatePodInfo(pod)
	case watch.Deleted:
		k.removePodInfo(pod)
	}
}

// updatePodInfo updates pod information in eBPF maps
func (k *K8sIntegration) updatePodInfo(pod *v1.Pod) {
	// Extract cgroup ID from pod status or annotations
	cgroupID := k.extractCgroupIDFromPod(pod)
	if cgroupID == 0 {
		return
	}

	// Update pod info map
	if err := k.collector.UpdatePodInfo(cgroupID, string(pod.UID), pod.Namespace, pod.Name); err != nil {
		k.logger.Error("Failed to update pod info", zap.Error(err),
			zap.String("pod", pod.Name), zap.String("namespace", pod.Namespace))
		return
	}

	// Update container info for each container
	for i, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.ContainerID == "" {
			continue
		}

		// Extract PID from container status (this is simplified - would need container runtime integration)
		pid := k.extractPIDFromContainer(pod, &containerStatus)
		if pid == 0 {
			continue
		}

		image := pod.Spec.Containers[i].Image
		if err := k.collector.UpdateContainerInfo(pid, containerStatus.ContainerID, string(pod.UID), image); err != nil {
			k.logger.Error("Failed to update container info", zap.Error(err))
		}
	}

	// Update mount information for ConfigMaps and Secrets
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap != nil {
			mountPath := k.findMountPath(pod, volume.Name)
			if mountPath != "" {
				if err := k.collector.UpdateMountInfo(mountPath, volume.ConfigMap.Name, pod.Namespace, false); err != nil {
					k.logger.Error("Failed to update configmap mount info", zap.Error(err))
				}
			}
		}
		if volume.Secret != nil {
			mountPath := k.findMountPath(pod, volume.Name)
			if mountPath != "" {
				if err := k.collector.UpdateMountInfo(mountPath, volume.Secret.SecretName, pod.Namespace, true); err != nil {
					k.logger.Error("Failed to update secret mount info", zap.Error(err))
				}
			}
		}
	}

	k.logger.Debug("Updated pod info in eBPF maps",
		zap.String("pod", pod.Name),
		zap.String("namespace", pod.Namespace),
		zap.Uint64("cgroup_id", cgroupID))
}

// removePodInfo removes pod information from eBPF maps
func (k *K8sIntegration) removePodInfo(pod *v1.Pod) {
	cgroupID := k.extractCgroupIDFromPod(pod)
	if cgroupID == 0 {
		return
	}

	if err := k.collector.RemovePodInfo(cgroupID); err != nil {
		k.logger.Error("Failed to remove pod info", zap.Error(err))
	}
}

// extractCgroupIDFromPod extracts cgroup ID from pod
// In a real implementation, this would integrate with container runtime
func (k *K8sIntegration) extractCgroupIDFromPod(pod *v1.Pod) uint64 {
	// This is a placeholder - real implementation would:
	// 1. Query container runtime (containerd/docker) for container info
	// 2. Extract cgroup path from container info
	// 3. Read cgroup inode number from filesystem
	// 4. Handle different container runtime formats

	// For now, use a hash of pod UID as placeholder
	return k.hashString(string(pod.UID))
}

// extractPIDFromContainer extracts main PID from container
func (k *K8sIntegration) extractPIDFromContainer(pod *v1.Pod, containerStatus *v1.ContainerStatus) uint32 {
	// This is a placeholder - real implementation would:
	// 1. Parse container ID to extract runtime and ID
	// 2. Query container runtime API for container inspect
	// 3. Extract main process PID from container state
	return 0
}

// findMountPath finds the mount path for a volume in the pod
func (k *K8sIntegration) findMountPath(pod *v1.Pod, volumeName string) string {
	for _, container := range pod.Spec.Containers {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == volumeName {
				return volumeMount.MountPath
			}
		}
	}
	return ""
}

// startServiceWatcher starts watching Service resources
func (k *K8sIntegration) startServiceWatcher() error {
	watcher, err := k.client.CoreV1().Services("").Watch(k.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	k.mu.Lock()
	k.watchers = append(k.watchers, watcher)
	k.mu.Unlock()

	go func() {
		defer watcher.Stop()
		for {
			select {
			case event, ok := <-watcher.ResultChan():
				if !ok {
					return
				}
				k.handleServiceEvent(event)
			case <-k.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// handleServiceEvent processes Service watch events
func (k *K8sIntegration) handleServiceEvent(event watch.Event) {
	service, ok := event.Object.(*v1.Service)
	if !ok {
		return
	}

	switch event.Type {
	case watch.Added, watch.Modified:
		k.updateServiceEndpoints(service)
	case watch.Deleted:
		k.removeServiceEndpoints(service)
	}
}

// updateServiceEndpoints updates service endpoint information
func (k *K8sIntegration) updateServiceEndpoints(service *v1.Service) {
	for _, port := range service.Spec.Ports {
		if err := k.collector.UpdateServiceEndpoint(
			service.Spec.ClusterIP,
			uint16(port.Port),
			service.Name,
			service.Namespace,
			service.Spec.ClusterIP,
		); err != nil {
			k.logger.Error("Failed to update service endpoint", zap.Error(err))
		}
	}
}

// removeServiceEndpoints removes service endpoint information
func (k *K8sIntegration) removeServiceEndpoints(service *v1.Service) {
	for _, port := range service.Spec.Ports {
		if err := k.collector.RemoveServiceEndpoint(service.Spec.ClusterIP, uint16(port.Port)); err != nil {
			k.logger.Error("Failed to remove service endpoint", zap.Error(err))
		}
	}
}

// startConfigMapWatcher starts watching ConfigMap resources
func (k *K8sIntegration) startConfigMapWatcher() error {
	watcher, err := k.client.CoreV1().ConfigMaps("").Watch(k.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	k.mu.Lock()
	k.watchers = append(k.watchers, watcher)
	k.mu.Unlock()

	go func() {
		defer watcher.Stop()
		for {
			select {
			case event, ok := <-watcher.ResultChan():
				if !ok {
					return
				}
				k.handleConfigMapEvent(event)
			case <-k.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// handleConfigMapEvent processes ConfigMap watch events
func (k *K8sIntegration) handleConfigMapEvent(event watch.Event) {
	configMap, ok := event.Object.(*v1.ConfigMap)
	if !ok {
		return
	}

	switch event.Type {
	case watch.Added, watch.Modified:
		// ConfigMap changes are handled through pod updates
		k.logger.Debug("ConfigMap updated",
			zap.String("configmap", configMap.Name),
			zap.String("namespace", configMap.Namespace))
	case watch.Deleted:
		k.logger.Debug("ConfigMap deleted",
			zap.String("configmap", configMap.Name),
			zap.String("namespace", configMap.Namespace))
	}
}

// startSecretWatcher starts watching Secret resources
func (k *K8sIntegration) startSecretWatcher() error {
	watcher, err := k.client.CoreV1().Secrets("").Watch(k.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	k.mu.Lock()
	k.watchers = append(k.watchers, watcher)
	k.mu.Unlock()

	go func() {
		defer watcher.Stop()
		for {
			select {
			case event, ok := <-watcher.ResultChan():
				if !ok {
					return
				}
				k.handleSecretEvent(event)
			case <-k.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// handleSecretEvent processes Secret watch events
func (k *K8sIntegration) handleSecretEvent(event watch.Event) {
	secret, ok := event.Object.(*v1.Secret)
	if !ok {
		return
	}

	switch event.Type {
	case watch.Added, watch.Modified:
		k.logger.Debug("Secret updated",
			zap.String("secret", secret.Name),
			zap.String("namespace", secret.Namespace))
	case watch.Deleted:
		k.logger.Debug("Secret deleted",
			zap.String("secret", secret.Name),
			zap.String("namespace", secret.Namespace))
	}
}

// hashString creates a simple hash of a string
func (k *K8sIntegration) hashString(s string) uint64 {
	hash := uint64(5381)
	for i := 0; i < len(s) && i < 64; i++ {
		hash = ((hash << 5) + hash) + uint64(s[i])
	}
	return hash + 0x100000000 // Add offset to avoid collision with PIDs
}
