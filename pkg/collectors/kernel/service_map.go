package kernel

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// ServiceConnection represents a connection between two services
type ServiceConnection struct {
	SrcService   string
	SrcNamespace string
	SrcPod       string
	SrcIP        string
	SrcPort      uint16

	DstService   string
	DstNamespace string
	DstPod       string
	DstIP        string
	DstPort      uint16

	Protocol     string
	Count        int64
	LastSeen     time.Time
	AvgLatencyMs float64
}

// ServiceMap tracks connections between services
type ServiceMap struct {
	mu          sync.RWMutex
	connections map[string]*ServiceConnection // key: "srcIP:srcPort->dstIP:dstPort"

	// IP to service resolution
	ipToService map[string]ServiceInfo // key: "IP:Port"

	// Metrics
	totalConnections int64
	activeServices   map[string]bool
}

// ServiceInfo contains service identification info
type ServiceInfo struct {
	Name      string
	Namespace string
	PodName   string
	Labels    map[string]string
}

// NewServiceMap creates a new service map
func NewServiceMap() *ServiceMap {
	return &ServiceMap{
		connections:    make(map[string]*ServiceConnection),
		ipToService:    make(map[string]ServiceInfo),
		activeServices: make(map[string]bool),
	}
}

// RecordConnection records a network connection between services
func (sm *ServiceMap) RecordConnection(netInfo NetworkInfo, srcPod, dstPod string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Create connection key
	key := fmt.Sprintf("%s:%d->%s:%d",
		intToIP(netInfo.SAddr), netInfo.SPort,
		intToIP(netInfo.DAddr), netInfo.DPort)

	// Get or create connection
	conn, exists := sm.connections[key]
	if !exists {
		conn = &ServiceConnection{
			SrcIP:    intToIP(netInfo.SAddr),
			SrcPort:  netInfo.SPort,
			DstIP:    intToIP(netInfo.DAddr),
			DstPort:  netInfo.DPort,
			Protocol: protocolToString(netInfo.Protocol),
		}
		sm.connections[key] = conn
		sm.totalConnections++
	}

	// Update connection info
	conn.Count++
	conn.LastSeen = time.Now()

	// Resolve source service
	if srcInfo, ok := sm.ipToService[fmt.Sprintf("%s:%d", conn.SrcIP, conn.SrcPort)]; ok {
		conn.SrcService = srcInfo.Name
		conn.SrcNamespace = srcInfo.Namespace
		conn.SrcPod = srcInfo.PodName
		sm.activeServices[fmt.Sprintf("%s/%s", srcInfo.Namespace, srcInfo.Name)] = true
	} else if srcPod != "" {
		conn.SrcPod = srcPod
	}

	// Resolve destination service
	if dstInfo, ok := sm.ipToService[fmt.Sprintf("%s:%d", conn.DstIP, conn.DstPort)]; ok {
		conn.DstService = dstInfo.Name
		conn.DstNamespace = dstInfo.Namespace
		conn.DstPod = dstInfo.PodName
		sm.activeServices[fmt.Sprintf("%s/%s", dstInfo.Namespace, dstInfo.Name)] = true
	} else if dstPod != "" {
		conn.DstPod = dstPod
	}
}

// UpdateServiceEndpoint updates the IP to service mapping
func (sm *ServiceMap) UpdateServiceEndpoint(ip string, port uint16, info ServiceInfo) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := fmt.Sprintf("%s:%d", ip, port)
	sm.ipToService[key] = info
}

// RemoveServiceEndpoint removes a service endpoint
func (sm *ServiceMap) RemoveServiceEndpoint(ip string, port uint16) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := fmt.Sprintf("%s:%d", ip, port)
	delete(sm.ipToService, key)
}

// GetServiceConnections returns all connections for a service
func (sm *ServiceMap) GetServiceConnections(namespace, service string) []ServiceConnection {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var connections []ServiceConnection
	for _, conn := range sm.connections {
		if (conn.SrcNamespace == namespace && conn.SrcService == service) ||
			(conn.DstNamespace == namespace && conn.DstService == service) {
			connections = append(connections, *conn)
		}
	}

	return connections
}

// GetServiceMap returns the entire service map
func (sm *ServiceMap) GetServiceMap() map[string][]ServiceConnection {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	serviceMap := make(map[string][]ServiceConnection)

	// Group by source service
	for _, conn := range sm.connections {
		if conn.SrcService != "" {
			key := fmt.Sprintf("%s/%s", conn.SrcNamespace, conn.SrcService)
			serviceMap[key] = append(serviceMap[key], *conn)
		}
	}

	return serviceMap
}

// GetMetrics returns service map metrics
func (sm *ServiceMap) GetMetrics() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return map[string]interface{}{
		"total_connections":  sm.totalConnections,
		"active_connections": len(sm.connections),
		"tracked_services":   len(sm.activeServices),
		"service_endpoints":  len(sm.ipToService),
	}
}

// CleanupStale removes old connections
func (sm *ServiceMap) CleanupStale(maxAge time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for key, conn := range sm.connections {
		if conn.LastSeen.Before(cutoff) {
			delete(sm.connections, key)
		}
	}
}

// CreateServiceMapEvent creates a RawEvent for service connections
func (sm *ServiceMap) CreateServiceMapEvent() collectors.RawEvent {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Create a summary of active connections
	summary := make(map[string]interface{})
	for svc, conns := range sm.GetServiceMap() {
		var connList []map[string]interface{}
		for _, conn := range conns {
			connList = append(connList, map[string]interface{}{
				"dst_service": fmt.Sprintf("%s/%s", conn.DstNamespace, conn.DstService),
				"dst_ip":      conn.DstIP,
				"dst_port":    conn.DstPort,
				"protocol":    conn.Protocol,
				"count":       conn.Count,
				"last_seen":   conn.LastSeen,
			})
		}
		summary[svc] = connList
	}

	data, _ := json.Marshal(summary)

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "service_map",
		Data:      data,
		Metadata: map[string]string{
			"collector":          "kernel",
			"total_connections":  fmt.Sprintf("%d", sm.totalConnections),
			"active_connections": fmt.Sprintf("%d", len(sm.connections)),
			"tracked_services":   fmt.Sprintf("%d", len(sm.activeServices)),
		},
		TraceID: collectors.GenerateTraceID(),
		SpanID:  collectors.GenerateSpanID(),
	}
}

// Helper functions
func intToIP(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(ip>>24)&0xff,
		(ip>>16)&0xff,
		(ip>>8)&0xff,
		ip&0xff)
}

func protocolToString(proto uint8) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return fmt.Sprintf("%d", proto)
	}
}
