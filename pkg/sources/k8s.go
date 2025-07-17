package sources

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"

	"github.com/yairfalse/tapio/pkg/collector"
)

// K8sSource implements Kubernetes API-based data collection
type K8sSource struct {
	name       string
	client     kubernetes.Interface
	config     *rest.Config
	started    bool
	namespaces []string
}

// NewK8sSource creates a new Kubernetes API data source
func NewK8sSource() *K8sSource {
	return &K8sSource{
		name:       "kubernetes",
		started:    false,
		namespaces: []string{},
	}
}

// Name returns the name of the data source
func (s *K8sSource) Name() string {
	return s.name
}

// IsAvailable checks if Kubernetes API is available
func (s *K8sSource) IsAvailable(ctx context.Context) bool {
	if s.client == nil {
		return s.initializeClient() == nil
	}

	// Test connectivity
	_, err := s.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	return err == nil
}

// Start begins Kubernetes API data collection
func (s *K8sSource) Start(ctx context.Context) error {
	if s.started {
		return fmt.Errorf("Kubernetes source already started")
	}

	if err := s.initializeClient(); err != nil {
		return fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	// Load available namespaces
	if err := s.loadNamespaces(ctx); err != nil {
		return fmt.Errorf("failed to load namespaces: %w", err)
	}

	s.started = true
	return nil
}

// Stop stops Kubernetes API data collection
func (s *K8sSource) Stop(ctx context.Context) error {
	s.started = false
	return nil
}

// Collect gathers data from Kubernetes API
func (s *K8sSource) Collect(ctx context.Context, targets []collectors.Target) (collectors.DataSet, error) {
	if !s.started {
		return collectors.DataSet{}, fmt.Errorf("Kubernetes source not started")
	}

	dataset := collectors.DataSet{
		Timestamp: time.Now(),
		Source:    s.name,
		Metrics:   []collectors.Metric{},
		Events:    []collectors.Event{},
		Errors:    []error{},
	}

	for _, target := range targets {
		if s.SupportsTarget(target) {
			metrics, events, err := s.collectForTarget(ctx, target)
			if err != nil {
				dataset.Errors = append(dataset.Errors, fmt.Errorf("failed to collect for target %s: %w", target.Name, err))
				continue
			}

			dataset.Metrics = append(dataset.Metrics, metrics...)
			dataset.Events = append(dataset.Events, events...)
		}
	}

	return dataset, nil
}

// SupportsTarget checks if Kubernetes API can monitor the given target
func (s *K8sSource) SupportsTarget(target collectors.Target) bool {
	switch target.Type {
	case "pod", "service", "deployment", "replicaset", "namespace":
		return true
	case "container":
		// Containers can be monitored if they belong to a pod
		return target.Namespace != ""
	default:
		return false
	}
}

// initializeClient initializes the Kubernetes client
func (s *K8sSource) initializeClient() error {
	var config *rest.Config
	var err error

	// Try in-cluster config first
	if config, err = rest.InClusterConfig(); err != nil {
		// Fallback to kubeconfig
		kubeconfigPath := filepath.Join(homedir.HomeDir(), ".kube", "config")
		if config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath); err != nil {
			return fmt.Errorf("failed to build kubeconfig: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	s.client = client
	s.config = config
	return nil
}

// loadNamespaces loads available namespaces
func (s *K8sSource) loadNamespaces(ctx context.Context) error {
	namespaces, err := s.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	s.namespaces = make([]string, len(namespaces.Items))
	for i, ns := range namespaces.Items {
		s.namespaces[i] = ns.Name
	}

	return nil
}

// collectForTarget collects metrics and events for a specific target
func (s *K8sSource) collectForTarget(ctx context.Context, target collectors.Target) ([]collectors.Metric, []collectors.Event, error) {
	switch target.Type {
	case "pod":
		return s.collectPodData(ctx, target)
	case "service":
		return s.collectServiceData(ctx, target)
	case "namespace":
		return s.collectNamespaceData(ctx, target)
	default:
		return []collectors.Metric{}, []collectors.Event{}, fmt.Errorf("unsupported target type: %s", target.Type)
	}
}

// collectPodData collects data for a pod
func (s *K8sSource) collectPodData(ctx context.Context, target collectors.Target) ([]collectors.Metric, []collectors.Event, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = "default"
	}

	pod, err := s.client.CoreV1().Pods(namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return []collectors.Metric{}, []collectors.Event{}, err
	}

	metrics := s.extractPodMetrics(pod, target)
	events := s.extractPodEvents(ctx, pod, target)

	return metrics, events, nil
}

// collectServiceData collects data for a service
func (s *K8sSource) collectServiceData(ctx context.Context, target collectors.Target) ([]collectors.Metric, []collectors.Event, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = "default"
	}

	service, err := s.client.CoreV1().Services(namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return []collectors.Metric{}, []collectors.Event{}, err
	}

	metrics := s.extractServiceMetrics(service, target)
	events := []collectors.Event{} // Services don't have many events

	return metrics, events, nil
}

// collectNamespaceData collects data for a namespace
func (s *K8sSource) collectNamespaceData(ctx context.Context, target collectors.Target) ([]collectors.Metric, []collectors.Event, error) {
	namespace, err := s.client.CoreV1().Namespaces().Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return []collectors.Metric{}, []collectors.Event{}, err
	}

	metrics := s.extractNamespaceMetrics(ctx, namespace, target)
	events := []collectors.Event{} // Namespace events are rare

	return metrics, events, nil
}

// extractPodMetrics extracts metrics from a pod
func (s *K8sSource) extractPodMetrics(pod *corev1.Pod, target collectors.Target) []collectors.Metric {
	now := time.Now()
	metrics := []collectors.Metric{}

	// Pod phase as a metric
	phaseValue := 0.0
	switch pod.Status.Phase {
	case corev1.PodRunning:
		phaseValue = 1.0
	case corev1.PodPending:
		phaseValue = 0.5
	case corev1.PodFailed:
		phaseValue = -1.0
	}

	metrics = append(metrics, collectors.Metric{
		Name:      "pod_phase",
		Value:     phaseValue,
		Unit:      "state",
		Target:    target,
		Timestamp: now,
		Labels: map[string]string{
			"phase": string(pod.Status.Phase),
			"node":  pod.Spec.NodeName,
		},
	})

	// Container metrics
	for _, container := range pod.Status.ContainerStatuses {
		containerTarget := collectors.Target{
			Type:      "container",
			Name:      container.Name,
			Namespace: target.Namespace,
			Labels:    target.Labels,
		}

		metrics = append(metrics, collectors.Metric{
			Name:      "container_restart_count",
			Value:     float64(container.RestartCount),
			Unit:      "count",
			Target:    containerTarget,
			Timestamp: now,
			Labels: map[string]string{
				"container": container.Name,
				"ready":     strconv.FormatBool(container.Ready),
			},
		})

		// Ready state
		readyValue := 0.0
		if container.Ready {
			readyValue = 1.0
		}

		metrics = append(metrics, collectors.Metric{
			Name:      "container_ready",
			Value:     readyValue,
			Unit:      "state",
			Target:    containerTarget,
			Timestamp: now,
			Labels: map[string]string{
				"container": container.Name,
			},
		})
	}

	return metrics
}

// extractServiceMetrics extracts metrics from a service
func (s *K8sSource) extractServiceMetrics(service *corev1.Service, target collectors.Target) []collectors.Metric {
	now := time.Now()
	metrics := []collectors.Metric{}

	// Service type as a metric
	metrics = append(metrics, collectors.Metric{
		Name:      "service_type",
		Value:     1.0,
		Unit:      "state",
		Target:    target,
		Timestamp: now,
		Labels: map[string]string{
			"type":       string(service.Spec.Type),
			"cluster_ip": service.Spec.ClusterIP,
		},
	})

	// Port count
	metrics = append(metrics, collectors.Metric{
		Name:      "service_port_count",
		Value:     float64(len(service.Spec.Ports)),
		Unit:      "count",
		Target:    target,
		Timestamp: now,
		Labels: map[string]string{
			"type": string(service.Spec.Type),
		},
	})

	return metrics
}

// extractNamespaceMetrics extracts metrics from a namespace
func (s *K8sSource) extractNamespaceMetrics(ctx context.Context, namespace *corev1.Namespace, target collectors.Target) []collectors.Metric {
	now := time.Now()
	metrics := []collectors.Metric{}

	// Namespace phase
	phaseValue := 1.0
	if namespace.Status.Phase == corev1.NamespaceTerminating {
		phaseValue = 0.0
	}

	metrics = append(metrics, collectors.Metric{
		Name:      "namespace_phase",
		Value:     phaseValue,
		Unit:      "state",
		Target:    target,
		Timestamp: now,
		Labels: map[string]string{
			"phase": string(namespace.Status.Phase),
		},
	})

	// Count pods in namespace
	pods, err := s.client.CoreV1().Pods(namespace.Name).List(ctx, metav1.ListOptions{})
	if err == nil {
		metrics = append(metrics, collectors.Metric{
			Name:      "namespace_pod_count",
			Value:     float64(len(pods.Items)),
			Unit:      "count",
			Target:    target,
			Timestamp: now,
		})
	}

	return metrics
}

// extractPodEvents extracts events related to a pod
func (s *K8sSource) extractPodEvents(ctx context.Context, pod *corev1.Pod, target collectors.Target) []collectors.Event {
	events := []collectors.Event{}

	// Get events for this pod
	fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.namespace=%s", pod.Name, pod.Namespace)
	eventList, err := s.client.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return events
	}

	// Convert Kubernetes events to our Event format
	for _, k8sEvent := range eventList.Items {
		severity := "info"
		if k8sEvent.Type == "Warning" {
			severity = "warning"
		}

		events = append(events, collectors.Event{
			Type:      strings.ToLower(k8sEvent.Reason),
			Message:   k8sEvent.Message,
			Target:    target,
			Timestamp: k8sEvent.FirstTimestamp.Time,
			Severity:  severity,
			Data: map[string]interface{}{
				"source": k8sEvent.Source.Component,
				"count":  k8sEvent.Count,
				"reason": k8sEvent.Reason,
				"object": k8sEvent.InvolvedObject.Kind,
			},
		})
	}

	return events
}

// GetNamespaces returns available namespaces
func (s *K8sSource) GetNamespaces() []string {
	return s.namespaces
}

// GetCapabilities returns Kubernetes-specific capabilities
func (s *K8sSource) GetCapabilities(ctx context.Context) *collectors.Capabilities {
	caps := collectors.DetectCapabilities(ctx)
	caps.HasKubernetes = s.IsAvailable(ctx)
	return caps
}
