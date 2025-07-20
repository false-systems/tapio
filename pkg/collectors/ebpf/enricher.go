package ebpf

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EventEnricher enriches raw eBPF events with additional context
type EventEnricher struct {
	// Configuration
	enableContainerEnrichment bool
	enableK8sEnrichment       bool
	enableNetworkEnrichment   bool
	enableSecurityEnrichment  bool

	// Enrichment components
	processEnricher   *ProcessEnricher
	containerEnricher *ContainerEnricher
	k8sEnricher       *KubernetesEnricher
	networkEnricher   *NetworkEnricher
	securityEnricher  *SecurityEnricher

	// Cache for performance
	enrichmentCache map[string]*EnrichedEvent
	cacheMu         sync.RWMutex
	cacheSize       int
	cacheTTL        time.Duration

	// Hostname
	hostname string
}

// NewEventEnricher creates a new event enricher
func NewEventEnricher() *EventEnricher {
	hostname, _ := os.Hostname()

	enricher := &EventEnricher{
		enableContainerEnrichment: true,
		enableK8sEnrichment:       true,
		enableNetworkEnrichment:   true,
		enableSecurityEnrichment:  true,
		enrichmentCache:           make(map[string]*EnrichedEvent),
		cacheSize:                 10000,
		cacheTTL:                  5 * time.Minute,
		hostname:                  hostname,
	}

	// Initialize enrichment components
	enricher.processEnricher = NewProcessEnricher()
	enricher.containerEnricher = NewContainerEnricher()
	enricher.k8sEnricher = NewKubernetesEnricher()
	enricher.networkEnricher = NewNetworkEnricher()
	enricher.securityEnricher = NewSecurityEnricher()

	return enricher
}

// EnrichEvent enriches a raw eBPF event with additional context
func (e *EventEnricher) EnrichEvent(ctx context.Context, raw *RawEvent) (*EnrichedEvent, error) {
	// Check cache first
	cacheKey := e.getCacheKey(raw)
	if cached := e.getCachedEvent(cacheKey); cached != nil {
		// Update timestamp and event ID for cached event
		enriched := *cached
		enriched.EventID = generateEventID()
		enriched.Timestamp = time.Unix(0, int64(raw.Timestamp))
		enriched.Raw = raw
		return &enriched, nil
	}

	// Create enriched event
	enriched := &EnrichedEvent{
		Raw:       raw,
		EventID:   generateEventID(),
		Timestamp: time.Unix(0, int64(raw.Timestamp)),
		Hostname:  e.hostname,
		Metadata:  make(map[string]interface{}),
		Tags:      make([]string, 0),
	}

	// Process enrichment
	if err := e.enrichProcess(ctx, enriched); err != nil {
		return nil, fmt.Errorf("process enrichment failed: %w", err)
	}

	// Container enrichment
	if e.enableContainerEnrichment {
		if err := e.enrichContainer(ctx, enriched); err != nil {
			// Log error but continue
			enriched.Metadata["container_enrichment_error"] = err.Error()
		}
	}

	// Kubernetes enrichment
	if e.enableK8sEnrichment {
		if err := e.enrichKubernetes(ctx, enriched); err != nil {
			// Log error but continue
			enriched.Metadata["k8s_enrichment_error"] = err.Error()
		}
	}

	// Network enrichment
	if e.enableNetworkEnrichment && raw.Type == EventTypeNetwork {
		if err := e.enrichNetwork(ctx, enriched); err != nil {
			enriched.Metadata["network_enrichment_error"] = err.Error()
		}
	}

	// Security enrichment
	if e.enableSecurityEnrichment {
		if err := e.enrichSecurity(ctx, enriched); err != nil {
			enriched.Metadata["security_enrichment_error"] = err.Error()
		}
	}

	// Semantic analysis
	e.analyzeSemantics(enriched)

	// Cache the enriched event (without raw data to save memory)
	e.cacheEvent(cacheKey, enriched)

	return enriched, nil
}

// Process enrichment
func (e *EventEnricher) enrichProcess(ctx context.Context, enriched *EnrichedEvent) error {
	processInfo, err := e.processEnricher.EnrichProcess(enriched.Raw.PID)
	if err != nil {
		return err
	}

	enriched.ProcessInfo = processInfo
	enriched.Tags = append(enriched.Tags, "process:"+processInfo.Comm)

	return nil
}

// Container enrichment
func (e *EventEnricher) enrichContainer(ctx context.Context, enriched *EnrichedEvent) error {
	containerInfo, err := e.containerEnricher.EnrichContainer(enriched.Raw.PID)
	if err != nil {
		// Not an error if process is not in a container
		return nil
	}

	enriched.Container = containerInfo
	enriched.Tags = append(enriched.Tags, "container:"+containerInfo.Name)

	return nil
}

// Kubernetes enrichment
func (e *EventEnricher) enrichKubernetes(ctx context.Context, enriched *EnrichedEvent) error {
	if enriched.Container == nil {
		return nil // No container, no K8s context
	}

	k8sInfo, err := e.k8sEnricher.EnrichKubernetes(enriched.Container.ID)
	if err != nil {
		return nil // Not an error if not in K8s
	}

	enriched.Kubernetes = k8sInfo
	enriched.Tags = append(enriched.Tags, "namespace:"+k8sInfo.Namespace)
	enriched.Tags = append(enriched.Tags, "pod:"+k8sInfo.PodName)

	return nil
}

// Network enrichment
func (e *EventEnricher) enrichNetwork(ctx context.Context, enriched *EnrichedEvent) error {
	networkEvent, ok := enriched.Raw.Details.(*NetworkEvent)
	if !ok {
		return fmt.Errorf("expected NetworkEvent, got %T", enriched.Raw.Details)
	}

	networkContext, err := e.networkEnricher.EnrichNetwork(networkEvent)
	if err != nil {
		return err
	}

	enriched.Network = networkContext
	enriched.Tags = append(enriched.Tags, "protocol:"+networkContext.Protocol)

	return nil
}

// Security enrichment
func (e *EventEnricher) enrichSecurity(ctx context.Context, enriched *EnrichedEvent) error {
	securityContext, err := e.securityEnricher.EnrichSecurity(enriched.Raw.PID, enriched.Raw.UID)
	if err != nil {
		return err
	}

	enriched.Security = securityContext

	if securityContext.RiskScore > 0.7 {
		enriched.Tags = append(enriched.Tags, "high-risk")
	}

	return nil
}

// Semantic analysis
func (e *EventEnricher) analyzeSemantics(enriched *EnrichedEvent) {
	// Determine semantic type based on event characteristics
	enriched.SemanticType = e.determineSemanticType(enriched)
	enriched.SemanticGroup = e.determineSemanticGroup(enriched)

	// Extract trace context if available
	e.extractTraceContext(enriched)

	// Determine service name
	enriched.ServiceName = e.determineServiceName(enriched)

	// Set importance and interesting flags
	enriched.Importance = e.calculateImportance(enriched)
	enriched.Interesting = enriched.Importance > 0.5
}

func (e *EventEnricher) determineSemanticType(enriched *EnrichedEvent) string {
	switch enriched.Raw.Type {
	case EventTypeNetwork:
		if net, ok := enriched.Raw.Details.(*NetworkEvent); ok {
			switch net.SubType {
			case NetworkEventHTTP:
				return "http_request"
			case NetworkEventDNS:
				return "dns_query"
			case NetworkEventConnect:
				return "network_connection"
			default:
				return "network_event"
			}
		}
	case EventTypeProcess:
		if proc, ok := enriched.Raw.Details.(*ProcessEvent); ok {
			switch proc.SubType {
			case ProcessEventExec:
				return "process_spawn"
			case ProcessEventExit:
				return "process_exit"
			default:
				return "process_event"
			}
		}
	case EventTypeFile:
		return "file_access"
	case EventTypeSecurity:
		return "security_event"
	case EventTypeContainer:
		return "container_event"
	}
	return "system_event"
}

func (e *EventEnricher) determineSemanticGroup(enriched *EnrichedEvent) string {
	// Group related events together
	if enriched.Container != nil {
		return fmt.Sprintf("container_%s", enriched.Container.Name)
	}

	if enriched.ProcessInfo != nil {
		return fmt.Sprintf("process_%s", enriched.ProcessInfo.Comm)
	}

	return "system"
}

func (e *EventEnricher) extractTraceContext(enriched *EnrichedEvent) {
	// Try to extract trace context from various sources

	// Check environment variables for trace context
	if enriched.ProcessInfo != nil {
		// This would require reading process environment
		// For now, generate synthetic trace context for interesting events
		if enriched.Interesting {
			enriched.TraceID = generateTraceID()
			enriched.SpanID = generateSpanID()
		}
	}

	// Check network headers for HTTP events
	if enriched.Network != nil && enriched.Network.HTTPInfo != nil {
		if traceID, ok := enriched.Network.HTTPInfo.Headers["x-trace-id"]; ok {
			enriched.TraceID = traceID
		}
		if spanID, ok := enriched.Network.HTTPInfo.Headers["x-span-id"]; ok {
			enriched.SpanID = spanID
		}
	}
}

func (e *EventEnricher) determineServiceName(enriched *EnrichedEvent) string {
	// Try to determine service name from various sources

	if enriched.Kubernetes != nil {
		if enriched.Kubernetes.ServiceName != "" {
			return enriched.Kubernetes.ServiceName
		}
		return enriched.Kubernetes.PodName
	}

	if enriched.Container != nil {
		if service, ok := enriched.Container.Labels["service"]; ok {
			return service
		}
		if app, ok := enriched.Container.Labels["app"]; ok {
			return app
		}
		return enriched.Container.Name
	}

	if enriched.ProcessInfo != nil {
		return enriched.ProcessInfo.Comm
	}

	return "unknown"
}

func (e *EventEnricher) calculateImportance(enriched *EnrichedEvent) float64 {
	importance := 0.5 // Base importance

	// Security events are more important
	if enriched.Security != nil && enriched.Security.RiskScore > 0 {
		importance += enriched.Security.RiskScore * 0.3
	}

	// Network events with errors are important
	if enriched.Raw.Type == EventTypeNetwork {
		if net, ok := enriched.Raw.Details.(*NetworkEvent); ok {
			if net.L7Details != nil {
				if status, ok := net.L7Details["status_code"].(int); ok && status >= 400 {
					importance += 0.3
				}
			}
		}
	}

	// Process events in containers are more important
	if enriched.Container != nil {
		importance += 0.1
	}

	// Events in Kubernetes are more important
	if enriched.Kubernetes != nil {
		importance += 0.1
	}

	// Privileged processes are important
	if enriched.Security != nil && enriched.Security.Privileged {
		importance += 0.2
	}

	// Cap at 1.0
	if importance > 1.0 {
		importance = 1.0
	}

	return importance
}

// Cache management
func (e *EventEnricher) getCacheKey(raw *RawEvent) string {
	return fmt.Sprintf("%d:%s:%d:%d", raw.Type, raw.Comm, raw.PID, raw.UID)
}

func (e *EventEnricher) getCachedEvent(key string) *EnrichedEvent {
	e.cacheMu.RLock()
	defer e.cacheMu.RUnlock()

	if event, ok := e.enrichmentCache[key]; ok {
		return event
	}
	return nil
}

func (e *EventEnricher) cacheEvent(key string, event *EnrichedEvent) {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()

	// Simple cache size management
	if len(e.enrichmentCache) >= e.cacheSize {
		// Clear cache when it gets too large
		e.enrichmentCache = make(map[string]*EnrichedEvent)
	}

	// Store a copy without raw data to save memory
	cached := *event
	cached.Raw = nil
	e.enrichmentCache[key] = &cached
}

// ProcessEnricher enriches process information
type ProcessEnricher struct{}

func NewProcessEnricher() *ProcessEnricher {
	return &ProcessEnricher{}
}

func (pe *ProcessEnricher) EnrichProcess(pid uint32) (*ProcessInfo, error) {
	// Read process information from /proc
	procPath := fmt.Sprintf("/proc/%d", pid)

	info := &ProcessInfo{
		PID: pid,
	}

	// Read command line
	if cmdline, err := os.ReadFile(procPath + "/cmdline"); err == nil {
		args := strings.Split(string(cmdline), "\x00")
		if len(args) > 0 {
			info.Cmdline = args
			info.Exe = args[0]
		}
	}

	// Read comm (process name)
	if comm, err := os.ReadFile(procPath + "/comm"); err == nil {
		info.Comm = strings.TrimSpace(string(comm))
	}

	// Read status for UID/GID/PPID
	if status, err := os.ReadFile(procPath + "/status"); err == nil {
		lines := strings.Split(string(status), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				switch fields[0] {
				case "PPid:":
					if ppid, err := strconv.ParseUint(fields[1], 10, 32); err == nil {
						info.PPID = uint32(ppid)
					}
				case "Uid:":
					if uid, err := strconv.ParseUint(fields[1], 10, 32); err == nil {
						info.UID = uint32(uid)
					}
				case "Gid:":
					if gid, err := strconv.ParseUint(fields[1], 10, 32); err == nil {
						info.GID = uint32(gid)
					}
				}
			}
		}
	}

	// Read cgroup
	if cgroup, err := os.ReadFile(procPath + "/cgroup"); err == nil {
		info.Cgroup = strings.TrimSpace(string(cgroup))
	}

	return info, nil
}

// ContainerEnricher enriches container information
type ContainerEnricher struct{}

func NewContainerEnricher() *ContainerEnricher {
	return &ContainerEnricher{}
}

func (ce *ContainerEnricher) EnrichContainer(pid uint32) (*ContainerInfo, error) {
	// Read cgroup to determine if process is in a container
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	cgroupData, err := os.ReadFile(cgroupPath)
	if err != nil {
		return nil, err
	}

	cgroupStr := string(cgroupData)

	// Check for Docker container
	if strings.Contains(cgroupStr, "/docker/") {
		return ce.parseDockerContainer(cgroupStr)
	}

	// Check for containerd
	if strings.Contains(cgroupStr, "/containerd/") {
		return ce.parseContainerdContainer(cgroupStr)
	}

	// Check for systemd container
	if strings.Contains(cgroupStr, "/machine.slice/") {
		return ce.parseSystemdContainer(cgroupStr)
	}

	return nil, fmt.Errorf("process not in a recognized container")
}

func (ce *ContainerEnricher) parseDockerContainer(cgroup string) (*ContainerInfo, error) {
	// Extract container ID from cgroup path
	lines := strings.Split(cgroup, "\n")
	for _, line := range lines {
		if strings.Contains(line, "/docker/") {
			parts := strings.Split(line, "/")
			for i, part := range parts {
				if part == "docker" && i+1 < len(parts) {
					containerID := parts[i+1]
					return &ContainerInfo{
						ID:      containerID,
						Runtime: "docker",
						State:   "running",
					}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("could not parse Docker container ID")
}

func (ce *ContainerEnricher) parseContainerdContainer(cgroup string) (*ContainerInfo, error) {
	// Similar parsing for containerd
	lines := strings.Split(cgroup, "\n")
	for _, line := range lines {
		if strings.Contains(line, "/containerd/") {
			parts := strings.Split(line, "/")
			for i, part := range parts {
				if part == "containerd" && i+1 < len(parts) {
					containerID := parts[i+1]
					return &ContainerInfo{
						ID:      containerID,
						Runtime: "containerd",
						State:   "running",
					}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("could not parse containerd container ID")
}

func (ce *ContainerEnricher) parseSystemdContainer(cgroup string) (*ContainerInfo, error) {
	// Parse systemd-managed containers
	return &ContainerInfo{
		ID:      "systemd-container",
		Runtime: "systemd",
		State:   "running",
	}, nil
}

// KubernetesEnricher enriches Kubernetes information
type KubernetesEnricher struct{}

func NewKubernetesEnricher() *KubernetesEnricher {
	return &KubernetesEnricher{}
}

func (ke *KubernetesEnricher) EnrichKubernetes(containerID string) (*KubernetesInfo, error) {
	// In a real implementation, this would query the Kubernetes API
	// or read from container runtime metadata
	// For now, return mock data if it looks like a K8s environment

	if strings.Contains(containerID, "k8s") || strings.Contains(containerID, "pause") {
		return &KubernetesInfo{
			Namespace:     "default",
			PodName:       "sample-pod",
			NodeName:      "node-1",
			ContainerName: "app",
		}, nil
	}

	return nil, fmt.Errorf("not in Kubernetes environment")
}

// NetworkEnricher enriches network information
type NetworkEnricher struct{}

func NewNetworkEnricher() *NetworkEnricher {
	return &NetworkEnricher{}
}

func (ne *NetworkEnricher) EnrichNetwork(networkEvent *NetworkEvent) (*NetworkContext, error) {
	context := &NetworkContext{
		Protocol:        ne.getProtocolName(networkEvent.Protocol),
		Direction:       networkEvent.Direction,
		BytesTransmit:   uint64(networkEvent.Size),
		BytesReceive:    uint64(networkEvent.Size),
		PacketsTransmit: 1,
		PacketsReceive:  1,
	}

	// Enrich with L7 information if available
	if networkEvent.L7Protocol == "http" && networkEvent.L7Details != nil {
		httpInfo := &HTTPInfo{}
		if method, ok := networkEvent.L7Details["method"].(string); ok {
			httpInfo.Method = method
		}
		if url, ok := networkEvent.L7Details["url"].(string); ok {
			httpInfo.URL = url
		}
		if status, ok := networkEvent.L7Details["status_code"].(int); ok {
			httpInfo.StatusCode = status
		}
		context.HTTPInfo = httpInfo
	}

	return context, nil
}

func (ne *NetworkEnricher) getProtocolName(proto uint16) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 1:
		return "icmp"
	default:
		return fmt.Sprintf("proto_%d", proto)
	}
}

// SecurityEnricher enriches security information
type SecurityEnricher struct{}

func NewSecurityEnricher() *SecurityEnricher {
	return &SecurityEnricher{}
}

func (se *SecurityEnricher) EnrichSecurity(pid, uid uint32) (*SecurityContext, error) {
	context := &SecurityContext{
		UserNamespace: false,
		Privileged:    uid == 0,
		RiskScore:     0.0,
	}

	// Check if running as root
	if uid == 0 {
		context.RiskScore += 0.3
	}

	// Check for capabilities (simplified)
	capPath := fmt.Sprintf("/proc/%d/status", pid)
	if data, err := os.ReadFile(capPath); err == nil {
		statusStr := string(data)
		if strings.Contains(statusStr, "CapEff:") {
			context.RiskScore += 0.2
		}
	}

	// Check SELinux context if available
	selinuxPath := fmt.Sprintf("/proc/%d/attr/current", pid)
	if data, err := os.ReadFile(selinuxPath); err == nil {
		context.SELinuxContext = strings.TrimSpace(string(data))
	}

	return context, nil
}

// Helper functions
func generateEventID() string {
	return fmt.Sprintf("ebpf-%d-%d", time.Now().UnixNano(), os.Getpid())
}

func generateTraceID() string {
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}

func generateSpanID() string {
	return fmt.Sprintf("span-%d", time.Now().UnixNano())
}
