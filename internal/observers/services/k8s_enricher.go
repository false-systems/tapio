package services

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sContext represents Kubernetes context for a connection
type K8sContext struct {
	// Pod information
	PodName      string
	PodNamespace string
	PodIP        string
	NodeName     string

	// Service information
	ServiceName      string
	ServiceNamespace string
	ServiceType      string
	ServicePorts     []ServicePort

	// Workload information
	WorkloadKind string // Deployment, StatefulSet, DaemonSet
	WorkloadName string

	// Labels and annotations
	PodLabels      map[string]string
	ServiceLabels  map[string]string
	WorkloadLabels map[string]string
	PodAnnotations map[string]string
}

// ServicePort represents a Kubernetes service port
type ServicePort struct {
	Name       string
	Port       int32
	TargetPort int32
	Protocol   string
}

// EnrichedConnection combines connection tracking connection data with K8s context
type EnrichedConnection struct {
	*ActiveConnection
	SrcK8sContext *K8sContext
	DstK8sContext *K8sContext
	ServiceFlow   *ServiceFlow
}

// ServiceFlow represents a service-to-service flow
type ServiceFlow struct {
	SourceService      string
	SourceNamespace    string
	DestinationService string
	DestinationNS      string
	Protocol           string
	Port               uint16
	FlowType           FlowType
}

// FlowType represents the type of service flow
type FlowType uint8

const (
	FlowIntraNamespace FlowType = 1 // Within same namespace
	FlowInterNamespace FlowType = 2 // Between namespaces
	FlowExternal       FlowType = 3 // To/from external services
	FlowIngress        FlowType = 4 // Ingress traffic
	FlowEgress         FlowType = 5 // Egress traffic
)

// K8sEnricher implements Kubernetes context mapping
type K8sEnricher struct {
	config *Config
	logger *zap.Logger

	// K8s client
	clientset kubernetes.Interface

	// K8s state caches
	mu            sync.RWMutex
	podCache      map[string]*K8sContext // IP -> K8sContext
	cgroupToPod   map[uint64]*K8sContext // CgroupID -> K8sContext
	serviceCache  map[string]*K8sContext // ServiceName.Namespace -> K8sContext
	enrichedConns map[ConnectionKey]*EnrichedConnection
	serviceFlows  map[string]*ServiceFlow // FlowKey -> ServiceFlow

	// connection tracking integration
	connectionsTracker *ConnectionTracker
	eventCh            chan *EnrichedConnection
	stopCh             chan struct{}
}

// NewK8sEnricher creates a new K8s enrichment K8s mapper
func NewK8sEnricher(config *Config, logger *zap.Logger, tracker *ConnectionTracker) (*K8sEnricher, error) {
	clientset, err := createK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s client: %w", err)
	}

	return &K8sEnricher{
		config:             config,
		logger:             logger.Named("k8s"),
		clientset:          clientset,
		podCache:           make(map[string]*K8sContext),
		cgroupToPod:        make(map[uint64]*K8sContext),
		serviceCache:       make(map[string]*K8sContext),
		enrichedConns:      make(map[ConnectionKey]*EnrichedConnection),
		serviceFlows:       make(map[string]*ServiceFlow),
		connectionsTracker: tracker,
		eventCh:            make(chan *EnrichedConnection, config.BufferSize),
		stopCh:             make(chan struct{}),
	}, nil
}

// createK8sClient creates a Kubernetes client
func createK8sClient() (kubernetes.Interface, error) {
	var config *rest.Config
	var err error

	// Try in-cluster config first (for pods running in K8s)
	config, err = rest.InClusterConfig()
	if err != nil {
		// Fallback to kubeconfig file (for local development)
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to get K8s config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s clientset: %w", err)
	}

	return clientset, nil
}

// Start begins K8s context mapping
func (m *K8sEnricher) Start(ctx context.Context) error {
	if !m.config.EnableK8sMapping {
		m.logger.Info("K8s enrichment K8s mapping disabled")
		return nil
	}

	m.logger.Info("Starting K8s enrichment K8s mapper")

	// Start K8s cache refresh
	go m.refreshK8sCache(ctx)

	// Start connection enrichment
	go m.enrichConnections(ctx)

	m.logger.Info("K8s enrichment K8s mapper started")
	return nil
}

// Stop stops the K8s mapper
func (m *K8sEnricher) Stop() error {
	m.logger.Info("Stopping K8s enrichment K8s mapper")
	close(m.stopCh)
	m.logger.Info("K8s enrichment K8s mapper stopped")
	return nil
}

// Events returns enriched connection events
func (m *K8sEnricher) Events() <-chan *EnrichedConnection {
	return m.eventCh
}

// GetServiceFlows returns current service flows
func (m *K8sEnricher) GetServiceFlows() map[string]*ServiceFlow {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*ServiceFlow)
	for k, v := range m.serviceFlows {
		result[k] = v
	}
	return result
}

// enrichConnections processes connection tracking events and adds K8s context
func (m *K8sEnricher) enrichConnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case event := <-m.connectionsTracker.Events():
			m.handleConnectionEvent(event)
		}
	}
}

// handleConnectionEvent enriches a connection event with K8s context
func (m *K8sEnricher) handleConnectionEvent(event *ConnectionEvent) {
	key := ConnectionKey{
		SrcIP:   event.GetSrcIPString(),
		DstIP:   event.GetDstIPString(),
		SrcPort: event.SrcPort,
		DstPort: event.DstPort,
		PID:     event.PID,
	}

	// Get the raw connection from connection tracking
	activeConns := m.connectionsTracker.GetActiveConnections()
	activeConn, exists := activeConns[key]
	if !exists {
		return
	}

	// Enrich with K8s context
	enriched := &EnrichedConnection{
		ActiveConnection: activeConn,
		SrcK8sContext:    m.getK8sContext(event.GetSrcIPString(), event.CgroupID),
		DstK8sContext:    m.getK8sContext(event.GetDstIPString(), 0), // External IPs don't have cgroup
	}

	// Generate service flow if both sides have K8s context
	if enriched.SrcK8sContext != nil && enriched.DstK8sContext != nil {
		enriched.ServiceFlow = m.generateServiceFlow(enriched)
		m.trackServiceFlow(enriched.ServiceFlow)
	}

	// Store enriched connection
	m.mu.Lock()
	m.enrichedConns[key] = enriched
	m.mu.Unlock()

	// Send to Level 3
	select {
	case m.eventCh <- enriched:
	default:
		m.logger.Warn("Enriched connection channel full, dropping event")
	}

	m.logger.Debug("Connection enriched with K8s context",
		zap.String("key", key.String()),
		zap.String("src_pod", getPodName(enriched.SrcK8sContext)),
		zap.String("dst_pod", getPodName(enriched.DstK8sContext)))
}

// getK8sContext retrieves K8s context for an IP or cgroup
func (m *K8sEnricher) getK8sContext(ip string, cgroupID uint64) *K8sContext {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Try cgroup mapping first (more reliable for pods)
	if cgroupID != 0 {
		if ctx, exists := m.cgroupToPod[cgroupID]; exists {
			return ctx
		}
	}

	// Fallback to IP mapping
	if ctx, exists := m.podCache[ip]; exists {
		return ctx
	}

	return nil
}

// generateServiceFlow creates a service flow from an enriched connection
func (m *K8sEnricher) generateServiceFlow(conn *EnrichedConnection) *ServiceFlow {
	if conn.SrcK8sContext == nil || conn.DstK8sContext == nil {
		return nil
	}

	flow := &ServiceFlow{
		SourceService:      conn.SrcK8sContext.ServiceName,
		SourceNamespace:    conn.SrcK8sContext.ServiceNamespace,
		DestinationService: conn.DstK8sContext.ServiceName,
		DestinationNS:      conn.DstK8sContext.ServiceNamespace,
		Protocol:           "TCP",
		Port:               conn.Key.DstPort,
		FlowType:           m.determineFlowType(conn.SrcK8sContext, conn.DstK8sContext),
	}

	return flow
}

// determineFlowType determines the type of service flow
func (m *K8sEnricher) determineFlowType(src, dst *K8sContext) FlowType {
	if src.ServiceNamespace == dst.ServiceNamespace {
		return FlowIntraNamespace
	}
	return FlowInterNamespace
}

// trackServiceFlow tracks a service flow
func (m *K8sEnricher) trackServiceFlow(flow *ServiceFlow) {
	if flow == nil {
		return
	}

	flowKey := fmt.Sprintf("%s.%s->%s.%s:%d",
		flow.SourceService, flow.SourceNamespace,
		flow.DestinationService, flow.DestinationNS,
		flow.Port)

	m.mu.Lock()
	m.serviceFlows[flowKey] = flow
	m.mu.Unlock()
}

// refreshK8sCache periodically refreshes the K8s cache
func (m *K8sEnricher) refreshK8sCache(ctx context.Context) {
	ticker := time.NewTicker(m.config.K8sRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.doK8sRefresh()
		}
	}
}

// doK8sRefresh performs the actual K8s cache refresh
func (m *K8sEnricher) doK8sRefresh() {
	m.logger.Debug("Refreshing K8s cache")

	if err := m.refreshPods(); err != nil {
		m.logger.Error("Failed to refresh pods", zap.Error(err))
	}

	if err := m.refreshServices(); err != nil {
		m.logger.Error("Failed to refresh services", zap.Error(err))
	}

	if err := m.refreshWorkloads(); err != nil {
		m.logger.Error("Failed to refresh workloads", zap.Error(err))
	}
}

// refreshPods updates the pod cache from K8s API
func (m *K8sEnricher) refreshPods() error {
	pods, err := m.clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear existing cache
	m.podCache = make(map[string]*K8sContext)
	m.cgroupToPod = make(map[uint64]*K8sContext)

	for _, pod := range pods.Items {
		if pod.Status.PodIP == "" {
			continue
		}

		ctx := &K8sContext{
			PodName:        pod.Name,
			PodNamespace:   pod.Namespace,
			PodIP:          pod.Status.PodIP,
			NodeName:       pod.Spec.NodeName,
			PodLabels:      pod.Labels,
			PodAnnotations: pod.Annotations,
		}

		m.podCache[pod.Status.PodIP] = ctx

		// Extract cgroup ID from annotations if available
		if cgroupStr, exists := pod.Annotations["tapio.io/cgroup-id"]; exists {
			if cgroupID, err := strconv.ParseUint(cgroupStr, 10, 64); err == nil {
				m.cgroupToPod[cgroupID] = ctx
			}
		}
	}

	m.logger.Debug("Refreshed pod cache", zap.Int("pod_count", len(pods.Items)))
	return nil
}

// refreshServices updates the service cache from K8s API
func (m *K8sEnricher) refreshServices() error {
	services, err := m.clientset.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear existing cache
	m.serviceCache = make(map[string]*K8sContext)

	for _, svc := range services.Items {
		// Convert K8s service ports to our format
		var servicePorts []ServicePort
		for _, port := range svc.Spec.Ports {
			servicePorts = append(servicePorts, ServicePort{
				Name:       port.Name,
				Port:       port.Port,
				TargetPort: port.TargetPort.IntVal,
				Protocol:   string(port.Protocol),
			})
		}

		ctx := &K8sContext{
			ServiceName:      svc.Name,
			ServiceNamespace: svc.Namespace,
			ServiceType:      string(svc.Spec.Type),
			ServicePorts:     servicePorts,
			ServiceLabels:    svc.Labels,
		}

		serviceKey := fmt.Sprintf("%s.%s", svc.Name, svc.Namespace)
		m.serviceCache[serviceKey] = ctx

		// Update pods with service information using label selector
		if svc.Spec.Selector != nil {
			for _, podCtx := range m.podCache {
				if podCtx.PodNamespace == svc.Namespace && labelsMatch(podCtx.PodLabels, svc.Spec.Selector) {
					podCtx.ServiceName = svc.Name
					podCtx.ServiceNamespace = svc.Namespace
					podCtx.ServiceType = string(svc.Spec.Type)
					podCtx.ServicePorts = servicePorts
					podCtx.ServiceLabels = svc.Labels
				}
			}

			// Update cgroup mappings
			for _, podCtx := range m.cgroupToPod {
				if podCtx.PodNamespace == svc.Namespace && labelsMatch(podCtx.PodLabels, svc.Spec.Selector) {
					podCtx.ServiceName = svc.Name
					podCtx.ServiceNamespace = svc.Namespace
					podCtx.ServiceType = string(svc.Spec.Type)
					podCtx.ServicePorts = servicePorts
					podCtx.ServiceLabels = svc.Labels
				}
			}
		}
	}

	m.logger.Debug("Refreshed service cache", zap.Int("service_count", len(services.Items)))
	return nil
}

// refreshWorkloads updates workload information from K8s API
func (m *K8sEnricher) refreshWorkloads() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get Deployments
	deployments, err := m.clientset.AppsV1().Deployments("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	// Get StatefulSets
	statefulSets, err := m.clientset.AppsV1().StatefulSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list statefulsets: %w", err)
	}

	// Get DaemonSets
	daemonSets, err := m.clientset.AppsV1().DaemonSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list daemonsets: %w", err)
	}

	workloadCount := 0

	// Process Deployments
	for _, deploy := range deployments.Items {
		workloadCount++
		m.updatePodsWithWorkload("Deployment", deploy.Name, deploy.Namespace, deploy.Spec.Selector.MatchLabels, deploy.Labels)
	}

	// Process StatefulSets
	for _, ss := range statefulSets.Items {
		workloadCount++
		m.updatePodsWithWorkload("StatefulSet", ss.Name, ss.Namespace, ss.Spec.Selector.MatchLabels, ss.Labels)
	}

	// Process DaemonSets
	for _, ds := range daemonSets.Items {
		workloadCount++
		m.updatePodsWithWorkload("DaemonSet", ds.Name, ds.Namespace, ds.Spec.Selector.MatchLabels, ds.Labels)
	}

	m.logger.Debug("Refreshed workload cache", zap.Int("workload_count", workloadCount))
	return nil
}

// updatePodsWithWorkload updates pods with workload information
func (m *K8sEnricher) updatePodsWithWorkload(kind, name, namespace string, selector, labels map[string]string) {
	// Update pods with workload information
	for _, podCtx := range m.podCache {
		if podCtx.PodNamespace == namespace && labelsMatch(podCtx.PodLabels, selector) {
			podCtx.WorkloadKind = kind
			podCtx.WorkloadName = name
			podCtx.WorkloadLabels = labels
		}
	}

	// Update cgroup mappings
	for _, podCtx := range m.cgroupToPod {
		if podCtx.PodNamespace == namespace && labelsMatch(podCtx.PodLabels, selector) {
			podCtx.WorkloadKind = kind
			podCtx.WorkloadName = name
			podCtx.WorkloadLabels = labels
		}
	}
}

// labelsMatch checks if pod labels match service/workload selector
func labelsMatch(podLabels, selectorLabels map[string]string) bool {
	for key, value := range selectorLabels {
		if podLabels[key] != value {
			return false
		}
	}
	return true
}

// Helper function to get pod name from K8s context
func getPodName(ctx *K8sContext) string {
	if ctx == nil {
		return "unknown"
	}
	return ctx.PodName
}
