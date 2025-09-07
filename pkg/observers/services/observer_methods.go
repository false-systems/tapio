package services

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// watchKubernetesServices watches for Kubernetes service changes
func (o *Observer) watchKubernetesServices(ctx context.Context) {
	o.logger.Info("Starting Kubernetes service discovery")

	// Initial discovery
	if err := o.discoverServices(ctx); err != nil {
		o.logger.Error("Initial service discovery failed", zap.Error(err))
		o.BaseObserver.RecordError(err)
	}

	// Watch for changes
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			if err := o.discoverServices(ctx); err != nil {
				o.logger.Warn("Service discovery failed", zap.Error(err))
				o.BaseObserver.RecordError(err)
			}
		}
	}
}

// discoverServices discovers services from Kubernetes
func (o *Observer) discoverServices(ctx context.Context) error {
	ctx, span := o.tracer.Start(ctx, "services.discover")
	defer span.End()

	if o.k8sClient == nil {
		return fmt.Errorf("kubernetes client not initialized")
	}

	// Record API call
	if o.k8sApiCalls != nil {
		o.k8sApiCalls.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "list_services"),
		))
	}

	// List services based on configuration
	serviceList, err := o.listServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	// Process each service
	for _, svc := range serviceList.Items {
		if o.shouldSkipService(&svc) {
			continue
		}

		service := o.kubeServiceToService(&svc)

		// Check for changes
		o.mu.Lock()
		existing, exists := o.services[service.Namespace+"/"+service.Name]
		if !exists {
			o.services[service.Namespace+"/"+service.Name] = service
			o.mu.Unlock()

			// New service discovered
			o.recordServiceDiscovered(ctx, service)
			o.queueChange(ChangeServiceAdded, service.Namespace+"/"+service.Name, "")
		} else {
			// Check for significant changes
			if o.hasSignificantChanges(existing, service) {
				o.services[service.Namespace+"/"+service.Name] = service
				o.mu.Unlock()
				o.queueChange(ChangeServiceModified, service.Namespace+"/"+service.Name, "")
			} else {
				o.mu.Unlock()
			}
		}

		// Update IP mappings
		o.updateIPMappings(service)
	}

	// Check for removed services
	o.checkRemovedServices(ctx, serviceList)

	return nil
}

// listServices lists services based on configuration
func (o *Observer) listServices(ctx context.Context) (*corev1.ServiceList, error) {
	listOptions := metav1.ListOptions{}

	if len(o.config.Namespaces) == 0 {
		// List all namespaces except excluded ones
		return o.k8sClient.CoreV1().Services("").List(ctx, listOptions)
	}

	// List specific namespaces
	allServices := &corev1.ServiceList{}
	for _, ns := range o.config.Namespaces {
		if o.isNamespaceExcluded(ns) {
			continue
		}

		services, err := o.k8sClient.CoreV1().Services(ns).List(ctx, listOptions)
		if err != nil {
			o.logger.Warn("Failed to list services in namespace",
				zap.String("namespace", ns),
				zap.Error(err))
			continue
		}
		allServices.Items = append(allServices.Items, services.Items...)
	}

	return allServices, nil
}

// shouldSkipService checks if a service should be skipped
func (o *Observer) shouldSkipService(svc *corev1.Service) bool {
	// Skip if in excluded namespace
	if o.isNamespaceExcluded(svc.Namespace) {
		return true
	}

	// Skip if system namespace and configured to ignore
	if o.config.IgnoreSystemNamespaces && o.isSystemNamespace(svc.Namespace) {
		return true
	}

	// Skip headless services (no cluster IP)
	if svc.Spec.ClusterIP == "None" || svc.Spec.ClusterIP == "" {
		return true
	}

	return false
}

// isNamespaceExcluded checks if namespace is excluded
func (o *Observer) isNamespaceExcluded(namespace string) bool {
	for _, excluded := range o.config.ExcludeNamespaces {
		if namespace == excluded {
			return true
		}
	}
	return false
}

// isSystemNamespace checks if namespace is a system namespace
func (o *Observer) isSystemNamespace(namespace string) bool {
	return strings.HasPrefix(namespace, "kube-") ||
		namespace == "default" ||
		strings.HasPrefix(namespace, "openshift-")
}

// kubeServiceToService converts Kubernetes service to our Service type
func (o *Observer) kubeServiceToService(svc *corev1.Service) *Service {
	service := &Service{
		Name:         svc.Name,
		Namespace:    svc.Namespace,
		Type:         o.detectServiceType(svc),
		Labels:       svc.Labels,
		Version:      o.extractVersion(svc),
		Ports:        o.extractPorts(svc),
		Dependencies: make(map[string]*Dependency),
		Dependents:   make(map[string]*Dependent),
		Health:       HealthUnknown,
		FirstSeen:    time.Now(),
		LastSeen:     time.Now(),
		IsExternal:   false,
	}

	// Get endpoints
	endpoints, err := o.getEndpoints(context.Background(), svc)
	if err != nil {
		o.logger.Warn("Failed to get endpoints",
			zap.String("service", svc.Name),
			zap.String("namespace", svc.Namespace),
			zap.Error(err))
	} else {
		service.Endpoints = endpoints
		// Update health based on ready endpoints
		service.Health = o.calculateServiceHealth(endpoints)
	}

	return service
}

// detectServiceType detects the service type
func (o *Observer) detectServiceType(svc *corev1.Service) ServiceType {
	if !o.config.AutoDetectType {
		return ServiceTypeUnknown
	}

	// Check by port
	for _, port := range svc.Spec.Ports {
		if serviceType, exists := o.config.PortMappings[port.Port]; exists {
			return serviceType
		}
	}

	// Check by label
	if serviceType, exists := svc.Labels["service-type"]; exists {
		return ServiceType(serviceType)
	}

	// Check by name patterns
	name := strings.ToLower(svc.Name)
	for pattern, serviceType := range o.config.ImagePatterns {
		if strings.Contains(name, pattern) {
			return serviceType
		}
	}

	return ServiceTypeUnknown
}

// extractVersion extracts version from service
func (o *Observer) extractVersion(svc *corev1.Service) string {
	// Check standard version labels
	if version, exists := svc.Labels["version"]; exists {
		return version
	}
	if version, exists := svc.Labels["app.kubernetes.io/version"]; exists {
		return version
	}
	if version, exists := svc.Labels["chart"]; exists {
		// Helm chart version
		return version
	}
	return ""
}

// extractPorts extracts ports from service
func (o *Observer) extractPorts(svc *corev1.Service) []Port {
	var ports []Port
	for _, p := range svc.Spec.Ports {
		ports = append(ports, Port{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: p.TargetPort.IntVal,
			Protocol:   string(p.Protocol),
		})
	}
	return ports
}

// getEndpoints gets endpoints for a service
func (o *Observer) getEndpoints(ctx context.Context, svc *corev1.Service) ([]Endpoint, error) {
	endpointsList, err := o.k8sClient.CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	var endpoints []Endpoint
	for _, subset := range endpointsList.Subsets {
		for _, addr := range subset.Addresses {
			for _, port := range subset.Ports {
				endpoint := Endpoint{
					IP:    addr.IP,
					Port:  port.Port,
					Ready: true,
				}

				// Add pod info if available
				if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
					endpoint.PodName = addr.TargetRef.Name
				}
				if addr.NodeName != nil {
					endpoint.NodeName = *addr.NodeName
				}

				endpoints = append(endpoints, endpoint)
			}
		}

		// Add not ready endpoints
		for _, addr := range subset.NotReadyAddresses {
			for _, port := range subset.Ports {
				endpoint := Endpoint{
					IP:    addr.IP,
					Port:  port.Port,
					Ready: false,
				}

				if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
					endpoint.PodName = addr.TargetRef.Name
				}
				if addr.NodeName != nil {
					endpoint.NodeName = *addr.NodeName
				}

				endpoints = append(endpoints, endpoint)
			}
		}
	}

	return endpoints, nil
}

// calculateServiceHealth calculates health based on endpoints
func (o *Observer) calculateServiceHealth(endpoints []Endpoint) HealthState {
	if len(endpoints) == 0 {
		return HealthDown
	}

	readyCount := 0
	for _, ep := range endpoints {
		if ep.Ready {
			readyCount++
		}
	}

	readyRatio := float64(readyCount) / float64(len(endpoints))

	switch {
	case readyRatio == 0:
		return HealthDown
	case readyRatio < 0.5:
		return HealthDegraded
	default:
		return HealthHealthy
	}
}

// updateIPMappings updates IP to service mappings
func (o *Observer) updateIPMappings(service *Service) {
	o.mu.Lock()
	defer o.mu.Unlock()

	serviceName := service.Namespace + "/" + service.Name

	// Clear old mappings for this service
	for ip, services := range o.ipToService {
		for i, svc := range services {
			if svc == serviceName {
				o.ipToService[ip] = append(services[:i], services[i+1:]...)
				if len(o.ipToService[ip]) == 0 {
					delete(o.ipToService, ip)
				}
				break
			}
		}
	}

	// Add new mappings
	for _, endpoint := range service.Endpoints {
		ipKey := endpoint.IP
		if endpoint.Port > 0 {
			ipKey = fmt.Sprintf("%s:%d", endpoint.IP, endpoint.Port)
		}

		if _, exists := o.ipToService[ipKey]; !exists {
			o.ipToService[ipKey] = []string{}
		}
		o.ipToService[ipKey] = append(o.ipToService[ipKey], serviceName)
	}
}

// hasSignificantChanges checks if there are significant changes
func (o *Observer) hasSignificantChanges(old, new *Service) bool {
	// Health change is significant
	if old.Health != new.Health {
		if o.healthChanges != nil {
			o.healthChanges.Add(context.Background(), 1, metric.WithAttributes(
				attribute.String("service", new.Namespace+"/"+new.Name),
				attribute.String("from", string(old.Health)),
				attribute.String("to", string(new.Health)),
			))
		}
		return true
	}

	// Version change is significant
	if old.Version != new.Version && new.Version != "" {
		return true
	}

	// Endpoint count change is significant
	if len(old.Endpoints) != len(new.Endpoints) {
		return true
	}

	// Service type change is significant
	if old.Type != new.Type {
		return true
	}

	return false
}

// checkRemovedServices checks for removed services
func (o *Observer) checkRemovedServices(ctx context.Context, currentServices *corev1.ServiceList) {
	o.mu.Lock()
	defer o.mu.Unlock()

	currentMap := make(map[string]bool)
	for _, svc := range currentServices.Items {
		currentMap[svc.Namespace+"/"+svc.Name] = true
	}

	for key, service := range o.services {
		if !currentMap[key] {
			// Service was removed
			delete(o.services, key)
			o.recordServiceRemoved(ctx, service)
			o.queueChange(ChangeServiceRemoved, key, "")
		}
	}
}

// recordServiceDiscovered records service discovery metric
func (o *Observer) recordServiceDiscovered(ctx context.Context, service *Service) {
	if o.servicesDiscovered != nil {
		o.servicesDiscovered.Add(ctx, 1, metric.WithAttributes(
			attribute.String("service", service.Namespace+"/"+service.Name),
			attribute.String("type", string(service.Type)),
		))
	}
	o.logger.Info("Service discovered",
		zap.String("service", service.Name),
		zap.String("namespace", service.Namespace),
		zap.String("type", string(service.Type)))
}

// recordServiceRemoved records service removal metric
func (o *Observer) recordServiceRemoved(ctx context.Context, service *Service) {
	if o.servicesRemoved != nil {
		o.servicesRemoved.Add(ctx, 1, metric.WithAttributes(
			attribute.String("service", service.Namespace+"/"+service.Name),
			attribute.String("type", string(service.Type)),
		))
	}
	o.logger.Info("Service removed",
		zap.String("service", service.Name),
		zap.String("namespace", service.Namespace))
}

// queueChange queues a change event
func (o *Observer) queueChange(changeType ChangeType, service, target string) {
	select {
	case o.pendingChanges <- ChangeEvent{
		Type:      changeType,
		Service:   service,
		Target:    target,
		Timestamp: time.Now(),
	}:
		atomic.AddInt32(&o.significantChanges, 1)
	default:
		// Channel full, drop oldest
		select {
		case <-o.pendingChanges:
			o.pendingChanges <- ChangeEvent{
				Type:      changeType,
				Service:   service,
				Target:    target,
				Timestamp: time.Now(),
			}
		default:
		}
	}
}

// processChanges processes queued changes
func (o *Observer) processChanges(ctx context.Context) {
	debounceTimer := time.NewTimer(o.config.ChangeDebounce)
	defer debounceTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		case change := <-o.pendingChanges:
			// Reset debounce timer
			if !debounceTimer.Stop() {
				select {
				case <-debounceTimer.C:
				default:
				}
			}
			debounceTimer.Reset(o.config.ChangeDebounce)

			// Check if we should emit immediately
			if o.shouldEmitImmediately(change) {
				o.emitServiceMap(ctx)
				debounceTimer.Reset(o.config.ChangeDebounce)
			}

		case <-debounceTimer.C:
			// Debounce period expired, emit if we have changes
			if atomic.LoadInt32(&o.significantChanges) > 0 {
				o.emitServiceMap(ctx)
				atomic.StoreInt32(&o.significantChanges, 0)
			}
			debounceTimer.Reset(o.config.ChangeDebounce)
		}
	}
}

// shouldEmitImmediately checks if we should emit immediately
func (o *Observer) shouldEmitImmediately(change ChangeEvent) bool {
	if !o.config.EmitOnChange {
		return false
	}

	switch change.Type {
	case ChangeServiceAdded, ChangeServiceRemoved:
		return true
	case ChangeHealthChanged:
		// Emit immediately if service went down
		o.mu.RLock()
		service, exists := o.services[change.Service]
		o.mu.RUnlock()
		if exists && service.Health == HealthDown {
			return true
		}
	case ChangeNewDependency:
		return true
	}

	return false
}

// emitSnapshots emits periodic snapshots
func (o *Observer) emitSnapshots(ctx context.Context) {
	ticker := time.NewTicker(o.config.FullSnapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			o.emitServiceMap(ctx)
		}
	}
}

// emitServiceMap emits the current service map
func (o *Observer) emitServiceMap(ctx context.Context) {
	ctx, span := o.tracer.Start(ctx, "services.emit_map")
	defer span.End()

	// Check minimum emit interval
	if time.Since(o.lastEmitTime) < o.config.MinEmitInterval {
		return
	}

	// Build service map
	serviceMap := o.buildServiceMap()

	// Check if unchanged
	if o.config.SkipUnchanged && o.isMapUnchanged(serviceMap) {
		return
	}

	// Convert to domain event
	event := o.createServiceMapEvent(ctx, serviceMap)
	if event == nil {
		return
	}

	// Send event
	if !o.EventChannelManager.SendEvent(event) {
		o.logger.Warn("Failed to send service map event - channel full")
		if o.droppedEvents != nil {
			o.droppedEvents.Add(ctx, 1)
		}
		return
	}

	// Update state
	o.lastEmitted = serviceMap
	o.lastEmitTime = time.Now()
	o.BaseObserver.RecordEvent()

	if o.eventsProcessed != nil {
		o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", "service_map"),
			attribute.Int("services", len(serviceMap.Services)),
			attribute.Int("connections", len(serviceMap.Connections)),
		))
	}
}

// buildServiceMap builds the current service map
func (o *Observer) buildServiceMap() *ServiceMap {
	o.mu.RLock()
	defer o.mu.RUnlock()

	// Copy services
	services := make(map[string]*Service)
	for key, svc := range o.services {
		// Deep copy service
		serviceCopy := *svc
		serviceCopy.Dependencies = make(map[string]*Dependency)
		for k, v := range svc.Dependencies {
			depCopy := *v
			serviceCopy.Dependencies[k] = &depCopy
		}
		serviceCopy.Dependents = make(map[string]*Dependent)
		for k, v := range svc.Dependents {
			depCopy := *v
			serviceCopy.Dependents[k] = &depCopy
		}
		services[key] = &serviceCopy
	}

	// Build connection map
	connections := make(map[string]int)
	for _, conn := range o.connections {
		srcIP := net.IPv4(byte(conn.SourceIP>>24), byte(conn.SourceIP>>16), byte(conn.SourceIP>>8), byte(conn.SourceIP)).String()
		dstIP := net.IPv4(byte(conn.DestIP>>24), byte(conn.DestIP>>16), byte(conn.DestIP>>8), byte(conn.DestIP)).String()

		// Try to resolve to service names
		srcService := o.resolveIPToService(srcIP, conn.SourcePort)
		dstService := o.resolveIPToService(dstIP, conn.DestPort)

		if srcService != "" && dstService != "" && srcService != dstService {
			connKey := fmt.Sprintf("%s->%s", srcService, dstService)
			connections[connKey]++
		}
	}

	return &ServiceMap{
		Services:    services,
		Connections: connections,
		LastUpdated: time.Now(),
		ClusterName: o.getClusterName(),
	}
}

// resolveIPToService resolves an IP to a service name
func (o *Observer) resolveIPToService(ip string, port uint16) string {
	// Try with port first
	if port > 0 {
		key := fmt.Sprintf("%s:%d", ip, port)
		if services, exists := o.ipToService[key]; exists && len(services) > 0 {
			return services[0]
		}
	}

	// Try without port
	if services, exists := o.ipToService[ip]; exists && len(services) > 0 {
		return services[0]
	}

	return ""
}

// isMapUnchanged checks if the service map is unchanged
func (o *Observer) isMapUnchanged(newMap *ServiceMap) bool {
	if o.lastEmitted == nil {
		return false
	}

	// Compare service counts
	if len(newMap.Services) != len(o.lastEmitted.Services) {
		return false
	}

	// Compare connection counts
	if len(newMap.Connections) != len(o.lastEmitted.Connections) {
		return false
	}

	// Compare services
	for key, newSvc := range newMap.Services {
		oldSvc, exists := o.lastEmitted.Services[key]
		if !exists {
			return false
		}

		// Compare key fields
		if newSvc.Health != oldSvc.Health ||
			newSvc.Version != oldSvc.Version ||
			len(newSvc.Endpoints) != len(oldSvc.Endpoints) ||
			len(newSvc.Dependencies) != len(oldSvc.Dependencies) {
			return false
		}
	}

	return true
}

// createServiceMapEvent creates a domain event for the service map
func (o *Observer) createServiceMapEvent(ctx context.Context, serviceMap *ServiceMap) *domain.CollectorEvent {
	ctx, span := o.tracer.Start(ctx, "services.create_event")
	defer span.End()

	// Convert our ServiceMap to domain.ServiceMapData
	services := make(map[string]domain.ServiceMapInfo)
	for name, svc := range serviceMap.Services {
		services[name] = domain.ServiceMapInfo{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Type:      string(svc.Type),
			Version:   svc.Version,
			Health:    string(svc.Health),
			Labels:    svc.Labels,
			Endpoints: len(svc.Endpoints),
		}
	}

	connections := make(map[string]domain.ConnectionInfo)
	for conn, count := range serviceMap.Connections {
		connections[conn] = domain.ConnectionInfo{
			Count: count,
		}
	}

	serviceMapData := &domain.ServiceMapData{
		Services:    services,
		Connections: connections,
		ClusterName: serviceMap.ClusterName,
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("service-map-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      domain.EventTypeServiceMap,
		Source:    o.name,
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			ServiceMap: serviceMapData,
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"collector_name": o.name,
				"collector_type": "services",
			},
		},
		TraceContext: &domain.TraceContext{
			TraceID: span.SpanContext().TraceID(),
			SpanID:  span.SpanContext().SpanID(),
		},
	}
}

// getClusterName gets the cluster name
func (o *Observer) getClusterName() string {
	// Try to get from config map or environment
	// For now, return a default
	return "default-cluster"
}
