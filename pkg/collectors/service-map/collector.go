package servicemap

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Collector implements the service map collector using base components
type Collector struct {
	*base.BaseCollector       // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channels
	*base.LifecycleManager    // Manages goroutines

	// Service map specific fields
	config    *Config
	logger    *zap.Logger
	k8sClient kubernetes.Interface

	// Service tracking
	services    map[string]*Service     // namespace/name -> service
	connections map[string]*Connection  // src:port->dst:port -> connection
	ipToService map[string][]string     // IP[:port] -> []service names (multiple services can share IP)
	mu          sync.RWMutex

	// eBPF state (platform-specific)
	ebpfState interface{}
	
	// Emission control
	lastEmitted      *ServiceMap
	lastEmitTime     time.Time
	pendingChanges   chan ChangeEvent
	changeDebouncer  *time.Timer
	significantChanges int32  // atomic counter

}

// NewCollector creates a new service map collector using all the new base features
func NewCollector(name string, config *Config, logger *zap.Logger) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize base collector
	baseConfig := base.BaseCollectorConfig{
		Name:               name,
		HealthCheckTimeout: 5 * time.Minute,
		ErrorRateThreshold: 0.05, // 5% error rate threshold
	}
	baseCollector := base.NewBaseCollectorWithConfig(baseConfig)

	eventChannel := base.NewEventChannelManager(config.BufferSize, name, logger)
	lifecycle := base.NewLifecycleManager(context.Background(), logger)

	// Initialize Kubernetes client if enabled
	var k8sClient kubernetes.Interface
	if config.EnableK8sDiscovery {
		client, err := createK8sClient(config.KubeConfig)
		if err != nil {
			logger.Warn("Failed to create Kubernetes client, K8s discovery disabled",
				zap.Error(err))
		} else {
			k8sClient = client
		}
	}

	c := &Collector{
		BaseCollector:       baseCollector,
		EventChannelManager: eventChannel,
		LifecycleManager:    lifecycle,
		config:              config,
		logger:              logger,
		k8sClient:           k8sClient,
		services:            make(map[string]*Service),
		connections:         make(map[string]*Connection),
		ipToService:         make(map[string][]string),
		pendingChanges:      make(chan ChangeEvent, 1000),
		lastEmitTime:        time.Now(),
	}

	return c, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return "service-map"
}

// Start starts the service map collector
func (c *Collector) Start(ctx context.Context) error {
	c.logger.Info("Starting service map collector")

	// Start K8s discovery if enabled
	if c.k8sClient != nil && c.config.EnableK8sDiscovery {
		c.LifecycleManager.Start("k8s-discovery", func() {
			c.watchKubernetesServices(ctx)
		})
	}

	// Start eBPF connection tracking if enabled
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			c.logger.Error("Failed to start eBPF", zap.Error(err))
			c.RecordError(err)
			// Continue without eBPF - K8s discovery still works
		} else {
			// Start ring buffer for high-performance eBPF event processing
			c.StartRingBuffer(ctx)
			
			c.LifecycleManager.Start("ebpf-processor", func() {
				c.processEBPFEvents(ctx)
			})
		}
	}

	// Setup default filters for noise reduction
	c.setupDefaultFilters()

	// Start smart emission controller
	c.LifecycleManager.Start("emission-controller", func() {
		c.runEmissionController(ctx)
	})


	// Start connection cleanup
	c.LifecycleManager.Start("connection-cleanup", func() {
		c.cleanupConnections(ctx)
	})

	c.SetHealthy(true)
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping service map collector")
	c.SetHealthy(false)

	// Stop ring buffer
	c.StopRingBuffer()

	// Stop filters
	c.StopFilters()

	// Stop eBPF if running
	if c.config.EnableEBPF {
		c.stopEBPF()
	}

	// Stop lifecycle manager (waits for goroutines)
	c.LifecycleManager.Stop(5 * time.Second)

	return nil
}

// Events returns the events channel - required by Collector interface
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// IsHealthy returns collector health status - required by Collector interface  
func (c *Collector) IsHealthy() bool {
	return c.BaseCollector.IsHealthy()
}

// watchKubernetesServices watches for Kubernetes service changes
func (c *Collector) watchKubernetesServices(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second) // Poll every 10 seconds
	defer ticker.Stop()

	// Initial discovery
	c.discoverServices(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.StopChannel():
			return
		case <-ticker.C:
			c.discoverServices(ctx)
		}
	}
}

// discoverServices discovers all services in the cluster
func (c *Collector) discoverServices(ctx context.Context) {
	// Get all services
	serviceList, err := c.k8sClient.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		c.RecordError(err)
		c.logger.Error("Failed to list services", zap.Error(err))
		return
	}

	// Get all endpoints
	endpointsList, err := c.k8sClient.CoreV1().Endpoints("").List(ctx, metav1.ListOptions{})
	if err != nil {
		c.RecordError(err)
		c.logger.Error("Failed to list endpoints", zap.Error(err))
		return
	}

	// Build endpoint map
	endpointMap := make(map[string]*corev1.Endpoints)
	for i := range endpointsList.Items {
		ep := &endpointsList.Items[i]
		key := fmt.Sprintf("%s/%s", ep.Namespace, ep.Name)
		endpointMap[key] = ep
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update services
	for i := range serviceList.Items {
		svc := &serviceList.Items[i]

		// Skip if namespace should be excluded
		if c.shouldExcludeNamespace(svc.Namespace) {
			continue
		}

		key := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
		
		// Get or create service
		service, exists := c.services[key]
		if !exists {
			service = &Service{
				Name:         svc.Name,
				Namespace:    svc.Namespace,
				Labels:       svc.Labels,
				Dependencies: make(map[string]*Dependency),
				Dependents:   make(map[string]*Dependent),
				FirstSeen:    time.Now(),
				Health:       HealthUnknown,
			}
			c.services[key] = service
			
			// Track new service
			c.recordChange(ChangeEvent{
				Type:      ChangeServiceAdded,
				Service:   key,
				Timestamp: time.Now(),
			})
		}

		// Update service info
		service.LastSeen = time.Now()
		oldVersion := service.Version
		service.Version = svc.Labels["version"]
		if service.Version == "" {
			service.Version = svc.Labels["app.kubernetes.io/version"]
		}
		
		// Track version change
		if oldVersion != "" && oldVersion != service.Version {
			c.recordChange(ChangeEvent{
				Type:      ChangeVersionChanged,
				Service:   key,
				Timestamp: time.Now(),
			})
		}

		// Update ports
		service.Ports = nil
		for _, port := range svc.Spec.Ports {
			service.Ports = append(service.Ports, Port{
				Name:       port.Name,
				Port:       port.Port,
				TargetPort: port.TargetPort.IntVal,
				Protocol:   string(port.Protocol),
			})

			// Auto-detect service type from port
			if c.config.AutoDetectType && service.Type == "" {
				if detectedType, ok := c.config.PortMappings[port.Port]; ok {
					service.Type = detectedType
				}
			}
		}

		// Get endpoints
		if ep, ok := endpointMap[key]; ok {
			service.Endpoints = nil
			for _, subset := range ep.Subsets {
				for _, addr := range subset.Addresses {
					for _, port := range subset.Ports {
						endpoint := Endpoint{
							IP:      addr.IP,
							Port:    port.Port,
							Ready:   true,
							PodName: "",
							NodeName: "",
						}
						
						if addr.TargetRef != nil {
							endpoint.PodName = addr.TargetRef.Name
						}
						if addr.NodeName != nil {
							endpoint.NodeName = *addr.NodeName
						}
						
						service.Endpoints = append(service.Endpoints, endpoint)
						
						// Map IP to service for connection tracking
						// Multiple services can share the same IP (NodePort, HostNetwork, etc)
						ipPortKey := fmt.Sprintf("%s:%d", addr.IP, port.Port)
						c.addIPServiceMapping(ipPortKey, key)
						c.addIPServiceMapping(addr.IP, key) // Also map just IP for broader matching
					}
				}
			}
		}

		// Auto-detect service type from image if we have pod info
		if c.config.AutoDetectType && service.Type == "" {
			service.Type = c.detectServiceType(svc)
		}

		// Set default type if still unknown
		if service.Type == "" {
			service.Type = ServiceTypeUnknown
		}

		// Update health based on endpoints
		oldHealth := service.Health
		if len(service.Endpoints) == 0 {
			service.Health = HealthDown
		} else {
			readyCount := 0
			for _, ep := range service.Endpoints {
				if ep.Ready {
					readyCount++
				}
			}
			if readyCount == len(service.Endpoints) {
				service.Health = HealthHealthy
			} else if readyCount > 0 {
				service.Health = HealthDegraded
			} else {
				service.Health = HealthDown
			}
		}
		
		// Track health change
		if oldHealth != service.Health {
			c.recordChange(ChangeEvent{
				Type:      ChangeHealthChanged,
				Service:   key,
				Timestamp: time.Now(),
			})
		}

		c.RecordEvent()
		
		// Emit service discovered event with filtering
		c.emitServiceEvent(key, service)
	}
}

// detectServiceType tries to detect service type from various sources
func (c *Collector) detectServiceType(svc *corev1.Service) ServiceType {
	// Check annotations
	if typeAnnotation, ok := svc.Annotations["tapio.io/service-type"]; ok {
		return ServiceType(typeAnnotation)
	}

	// Check labels
	if component, ok := svc.Labels["component"]; ok {
		component = strings.ToLower(component)
		for pattern, serviceType := range c.config.ImagePatterns {
			if strings.Contains(component, pattern) {
				return serviceType
			}
		}
	}

	// Try to get pods and check images
	if c.k8sClient != nil && len(svc.Spec.Selector) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		selector := labels.SelectorFromSet(svc.Spec.Selector)
		pods, err := c.k8sClient.CoreV1().Pods(svc.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector.String(),
		})

		if err == nil && len(pods.Items) > 0 {
			// Check first pod's containers
			for _, container := range pods.Items[0].Spec.Containers {
				imageName := strings.ToLower(container.Image)
				for pattern, serviceType := range c.config.ImagePatterns {
					if strings.Contains(imageName, pattern) {
						return serviceType
					}
				}
			}
		}
	}

	return ServiceTypeUnknown
}

// addIPServiceMapping safely adds a service to the IP mapping (handles multiple services per IP)
func (c *Collector) addIPServiceMapping(ip string, service string) {
	// Check if service already mapped to this IP
	services := c.ipToService[ip]
	for _, s := range services {
		if s == service {
			return // Already mapped
		}
	}
	c.ipToService[ip] = append(services, service)
}

// getServicesForIP returns all services that could be at this IP[:port]
func (c *Collector) getServicesForIP(ip string, port uint16) []string {
	// Try exact IP:port match first
	ipPortKey := fmt.Sprintf("%s:%d", ip, port)
	if services, exists := c.ipToService[ipPortKey]; exists && len(services) > 0 {
		return services
	}
	
	// Fall back to just IP
	if services, exists := c.ipToService[ip]; exists {
		return services
	}
	
	return nil
}

// shouldExcludeNamespace checks if namespace should be excluded
func (c *Collector) shouldExcludeNamespace(namespace string) bool {
	// Check explicit excludes
	for _, excluded := range c.config.ExcludeNamespaces {
		if namespace == excluded {
			return true
		}
	}

	// Check if we have explicit includes
	if len(c.config.Namespaces) > 0 {
		for _, included := range c.config.Namespaces {
			if namespace == included {
				return false
			}
		}
		return true // Not in include list
	}

	// Check system namespaces
	if c.config.IgnoreSystemNamespaces {
		systemNamespaces := []string{"kube-system", "kube-public", "kube-node-lease"}
		for _, sysNs := range systemNamespaces {
			if namespace == sysNs {
				return true
			}
		}
	}

	return false
}

// runEmissionController controls when to emit service map events
func (c *Collector) runEmissionController(ctx context.Context) {
	fullSnapshotTicker := time.NewTicker(c.config.FullSnapshotInterval)
	defer fullSnapshotTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.StopChannel():
			return
		
		case change := <-c.pendingChanges:
			// Handle change events
			c.handleChange(ctx, change)
			
		case <-fullSnapshotTicker.C:
			// Periodic full snapshot
			c.logger.Debug("Emitting periodic full snapshot")
			c.emitServiceMapEvent(false)
			
		case <-c.getDebounceChannel():
			// Debounced changes ready to emit
			if atomic.LoadInt32(&c.significantChanges) > 0 {
				c.logger.Debug("Emitting debounced changes",
					zap.Int32("changes", atomic.LoadInt32(&c.significantChanges)))
				c.emitServiceMapEvent(true)
				atomic.StoreInt32(&c.significantChanges, 0)
			}
		}
	}
}

// handleChange processes a change event and decides whether to emit immediately
func (c *Collector) handleChange(ctx context.Context, change ChangeEvent) {
	// Immediate emission for significant changes
	shouldEmitNow := false
	
	switch change.Type {
	case ChangeServiceAdded, ChangeServiceRemoved:
		shouldEmitNow = true
		c.logger.Info("Service change detected",
			zap.String("type", "service"),
			zap.String("service", change.Service))
			
	case ChangeNewDependency, ChangeDependencyRemoved:
		shouldEmitNow = true
		c.logger.Info("Dependency change detected",
			zap.String("source", change.Service),
			zap.String("target", change.Target))
			
	case ChangeHealthChanged:
		// Only immediate if service went down
		if service, exists := c.services[change.Service]; exists && service.Health == HealthDown {
			shouldEmitNow = true
			c.logger.Warn("Service health critical",
				zap.String("service", change.Service))
		}
		
	case ChangeVersionChanged:
		shouldEmitNow = true
		c.logger.Info("Service version changed",
			zap.String("service", change.Service))
			
	case ChangeConnectionUpdate:
		// Batch these - not urgent
		atomic.AddInt32(&c.significantChanges, 1)
		c.resetDebounceTimer()
	}
	
	// Check rate limiting
	if shouldEmitNow && time.Since(c.lastEmitTime) >= c.config.MinEmitInterval {
		c.emitServiceMapEvent(true)
		atomic.StoreInt32(&c.significantChanges, 0)
	} else if shouldEmitNow {
		// Too soon, debounce it
		atomic.AddInt32(&c.significantChanges, 1)
		c.resetDebounceTimer()
	}
}

// recordChange records a change event
func (c *Collector) recordChange(change ChangeEvent) {
	select {
	case c.pendingChanges <- change:
		// Sent successfully
	default:
		// Channel full, increment dropped counter
		c.logger.Debug("Change event dropped (channel full)",
			zap.String("service", change.Service))
	}
}

// resetDebounceTimer resets the debounce timer
func (c *Collector) resetDebounceTimer() {
	if c.changeDebouncer != nil {
		c.changeDebouncer.Stop()
	}
	c.changeDebouncer = time.AfterFunc(c.config.ChangeDebounce, func() {
		// Timer expired, trigger emission
		select {
		case c.pendingChanges <- ChangeEvent{Type: ChangeConnectionUpdate}:
		default:
		}
	})
}

// getDebounceChannel returns the debounce timer channel
func (c *Collector) getDebounceChannel() <-chan time.Time {
	if c.changeDebouncer == nil {
		// Return a channel that never fires
		return make(<-chan time.Time)
	}
	return c.changeDebouncer.C
}

// emitServiceMapEvent emits a service map event
func (c *Collector) emitServiceMapEvent(forceEmit bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Build service map
	serviceMap := &ServiceMap{
		Services:    make(map[string]*Service),
		Connections: make(map[string]int),
		LastUpdated: time.Now(),
	}
	
	// Check if we should skip unchanged (unless forced)
	if !forceEmit && c.config.SkipUnchanged && c.lastEmitted != nil {
		if c.isServiceMapUnchanged(serviceMap) {
			c.logger.Debug("Skipping unchanged service map emission")
			return
		}
	}

	// Copy services
	for key, svc := range c.services {
		// Skip services without recent activity
		if time.Since(svc.LastSeen) > c.config.ServiceTimeout {
			continue
		}

		serviceCopy := *svc
		serviceMap.Services[key] = &serviceCopy
	}

	// Build connection map
	for _, conn := range c.connections {
		srcServices := c.getServicesForIP(intToIP(conn.SourceIP), conn.SourcePort)
		dstServices := c.getServicesForIP(intToIP(conn.DestIP), conn.DestPort)

		// Process all service pairs
		for _, srcService := range srcServices {
			for _, dstService := range dstServices {
				if srcService != "" && dstService != "" && srcService != dstService {
					connKey := fmt.Sprintf("%s->%s", srcService, dstService)
					serviceMap.Connections[connKey]++

					// Use connection direction to correctly assign dependencies
					if conn.Direction == DirectionOutbound {
						// srcService depends on dstService
						if src, ok := serviceMap.Services[srcService]; ok {
							if src.Dependencies == nil {
								src.Dependencies = make(map[string]*Dependency)
							}
							if dep, exists := src.Dependencies[dstService]; exists {
								dep.LastSeen = time.Now()
								dep.CallRate++
							} else {
								src.Dependencies[dstService] = &Dependency{
									Target:    dstService,
									CallRate:  1,
									FirstSeen: time.Now(),
									LastSeen:  time.Now(),
									Protocol:  protocolToString(conn.Protocol),
								}
							}
						}

						// dstService has srcService as dependent
						if dst, ok := serviceMap.Services[dstService]; ok {
							if dst.Dependents == nil {
								dst.Dependents = make(map[string]*Dependent)
							}
							if dep, exists := dst.Dependents[srcService]; exists {
								dep.LastSeen = time.Now()
								dep.CallRate++
							} else {
								dst.Dependents[srcService] = &Dependent{
									Source:    srcService,
									CallRate:  1,
									FirstSeen: time.Now(),
									LastSeen:  time.Now(),
								}
							}
						}
					}
					// For inbound connections, dependencies are reversed
					// but we typically track from the client's perspective
				}
			}
		}
	}

	// Create event with tracing context
	ctx, span := c.StartSpan(context.Background(), "emit-service-map")
	defer span.End()
	
	// Convert to domain types
	domainServiceMap := &domain.ServiceMapData{
		Services:    make(map[string]domain.ServiceMapInfo),
		Connections: make(map[string]domain.ConnectionInfo),
		ClusterName: serviceMap.ClusterName,
		LastUpdated: serviceMap.LastUpdated,
	}
	
	// Convert services
	for k, v := range serviceMap.Services {
		deps := make([]string, 0, len(v.Dependencies))
		for dep := range v.Dependencies {
			deps = append(deps, dep)
		}
		dependents := make([]string, 0, len(v.Dependents))
		for dep := range v.Dependents {
			dependents = append(dependents, dep)
		}
		
		domainServiceMap.Services[k] = domain.ServiceMapInfo{
			Name:         v.Name,
			Namespace:    v.Namespace,
			Type:         string(v.Type),
			Version:      v.Version,
			Health:       string(v.Health),
			Labels:       v.Labels,
			Dependencies: deps,
			Dependents:   dependents,
			RequestRate:  v.RequestRate,
			ErrorRate:    v.ErrorRate,
			Endpoints:    len(v.Endpoints),
		}
	}
	
	// Convert connections
	for k, count := range serviceMap.Connections {
		parts := strings.Split(k, "->")
		if len(parts) == 2 {
			domainServiceMap.Connections[k] = domain.ConnectionInfo{
				Source:   parts[0],
				Target:   parts[1],
				Protocol: "TCP",
				Count:    count,
			}
		}
	}
	
	event := &domain.CollectorEvent{
		EventID:     fmt.Sprintf("service-map-%d", time.Now().UnixNano()),
		Source:      c.Name(),
		Type:        domain.EventTypeServiceMap,
		Timestamp:   time.Now(),
		Severity:    domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			ServiceMap: domainServiceMap,
		},
	}

	// Check if event should be processed (filtered)
	if !c.ShouldProcess(event) {
		c.logger.Debug("Service map event filtered")
		return
	}

	// Record event size for metrics
	eventSize := int64(len(serviceMap.Services)*100 + len(serviceMap.Connections)*50) // Rough estimate
	c.RecordEventSize(ctx, eventSize)

	// Record processing time
	start := time.Now()
	
	// Send via ring buffer if available, otherwise use channel
	var sent bool
	if c.IsRingBufferEnabled() {
		sent = c.WriteToRingBuffer(event)
	} else {
		sent = c.SendEvent(event)
	}
	
	c.RecordProcessingDuration(ctx, time.Since(start))
	
	if !sent {
		c.RecordDropWithReason(ctx, "channel_full")
	} else {
		c.RecordEventWithContext(ctx)
		// Track successful emission
		c.lastEmitted = serviceMap
		c.lastEmitTime = time.Now()
	}
}

// cleanupConnections removes old connections
func (c *Collector) cleanupConnections(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.StopChannel():
			return
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for key, conn := range c.connections {
				if now.Sub(conn.Timestamp) > c.config.ConnectionTTL {
					delete(c.connections, key)
				}
			}
			c.mu.Unlock()
		}
	}
}

// Helper functions

func createK8sClient(kubeconfig string) (kubernetes.Interface, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

func intToIP(ip uint32) string {
	return net.IPv4(byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24)).String()
}

func protocolToString(protocol uint8) string {
	switch protocol {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	default:
		return fmt.Sprintf("PROTO_%d", protocol)
	}
}

// isServiceMapUnchanged checks if the service map has changed since last emission
func (c *Collector) isServiceMapUnchanged(newMap *ServiceMap) bool {
	if c.lastEmitted == nil {
		return false // First emission
	}
	
	// Quick check: different number of services or connections
	if len(newMap.Services) != len(c.lastEmitted.Services) {
		return false
	}
	if len(newMap.Connections) != len(c.lastEmitted.Connections) {
		return false
	}
	
	// Check each service for changes
	for key, newSvc := range newMap.Services {
		oldSvc, exists := c.lastEmitted.Services[key]
		if !exists {
			return false // New service
		}
		
		// Check key properties
		if oldSvc.Version != newSvc.Version ||
			oldSvc.Health != newSvc.Health ||
			len(oldSvc.Dependencies) != len(newSvc.Dependencies) ||
			len(oldSvc.Dependents) != len(newSvc.Dependents) {
			return false
		}
	}
	
	// Check connection counts
	for connKey, newCount := range newMap.Connections {
		oldCount, exists := c.lastEmitted.Connections[connKey]
		if !exists || oldCount != newCount {
			return false
		}
	}
	
	return true // No changes detected
}

// setupDefaultFilters configures smart filters to reduce noise
func (c *Collector) setupDefaultFilters() {
	// Filter out system services if configured
	if c.config.IgnoreSystemNamespaces {
		c.AddDenyFilter("system-namespaces", func(event *domain.CollectorEvent) bool {
			if event.EventData.ServiceMap != nil {
				serviceMap := event.EventData.ServiceMap
				for serviceName := range serviceMap.Services {
					parts := strings.Split(serviceName, "/")
					if len(parts) == 2 {
						namespace := parts[0]
						systemNs := []string{"kube-system", "kube-public", "kube-node-lease"}
						for _, sysNs := range systemNs {
							if namespace == sysNs {
								return true // Deny system namespace services
							}
						}
					}
				}
			}
			return false
		})
	}

	// Filter out services with too few connections
	if c.config.MinConnectionCount > 1 {
		c.AddDenyFilter("min-connections", func(event *domain.CollectorEvent) bool {
			if event.EventData.ServiceMap != nil {
				serviceMap := event.EventData.ServiceMap
				totalConnections := 0
				for _, conn := range serviceMap.Connections {
					totalConnections += conn.Count
				}
				return totalConnections < c.config.MinConnectionCount
			}
			return false
		})
	}

	// Filter out external services if not wanted
	if !c.config.IncludeExternalServices {
		c.AddDenyFilter("no-external", func(event *domain.CollectorEvent) bool {
			// External services filtering disabled for now
			// TODO: Add IsExternal to domain.ServiceMapInfo if needed
			return false
		})
	}

	// Allow filter for important service types
	c.AddAllowFilter("important-services", func(event *domain.CollectorEvent) bool {
		if event.EventData.ServiceMap != nil {
			serviceMap := event.EventData.ServiceMap
			importantTypes := []string{"database", "queue", "api"}
			for _, service := range serviceMap.Services {
				for _, importantType := range importantTypes {
					if service.Type == importantType {
						return true // Always allow important services
					}
				}
			}
		}
		return false // Let other filters decide
	})
}

// emitServiceEvent emits a single service discovery event
func (c *Collector) emitServiceEvent(serviceKey string, service *Service) {
	ctx, span := c.StartSpan(context.Background(), "emit-service-event", 
		trace.WithAttributes(
			attribute.String("service.name", service.Name),
			attribute.String("service.namespace", service.Namespace),
			attribute.String("service.type", string(service.Type)),
		))
	defer span.End()

	event := &domain.CollectorEvent{
		EventID:     fmt.Sprintf("service-discovered-%s-%d", serviceKey, time.Now().UnixNano()),
		Source:      c.Name(),
		Type:        domain.EventTypeNetworkConnection, // Using existing type
		Timestamp:   time.Now(),
		Severity:    domain.EventSeverityInfo,
		EventData:   domain.EventDataContainer{
			Custom: map[string]string{
				"service_name": service.Name,
				"service_namespace": service.Namespace,
				"service_type": string(service.Type),
			},
		},
	}

	// Record event size
	eventSize := int64(len(serviceKey) + len(service.Name) + len(service.Version) + 200) // Rough estimate
	c.RecordEventSize(ctx, eventSize)

	// Send via ring buffer if available
	if c.IsRingBufferEnabled() {
		if c.WriteToRingBuffer(event) {
			c.RecordEventWithContext(ctx)
		}
	}
}