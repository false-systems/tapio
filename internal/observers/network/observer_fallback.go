//go:build !linux
// +build !linux

package network

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// startEBPF is a no-op on non-Linux platforms
func (o *Observer) startEBPF() error {
	o.logger.Warn("Network observer requires Linux with eBPF support, running in mock mode")
	return nil
}

// stopEBPF is a no-op on non-Linux platforms
func (o *Observer) stopEBPF() {
	// Nothing to clean up
}

// processEvents generates mock events on non-Linux platforms
func (o *Observer) processEvents() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	eventCount := 0
	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			eventCount++
			o.generateMockEvents(eventCount)
		}
	}
}

// generateMockEvents creates and sends mock HTTP and DNS events
func (o *Observer) generateMockEvents(eventCount int) {
	httpEvent := o.createMockHTTPEvent(eventCount)
	o.SendEvent(httpEvent)

	dnsEvent := o.createMockDNSEvent(eventCount)
	o.SendEvent(dnsEvent)

	o.logger.Debug("Sent mock network events", zap.Int("count", eventCount*2))
}

// createMockHTTPEvent generates a mock HTTP request event
func (o *Observer) createMockHTTPEvent(eventCount int) *domain.CollectorEvent {
	httpMethods := []string{"GET", "POST", "PUT", "DELETE"}
	httpPaths := []string{"/health", "/api/v1/users", "/api/v1/metrics", "/status"}
	httpHosts := []string{"api.example.com", "backend.local", "service.cluster.local"}

	method := httpMethods[eventCount%len(httpMethods)]
	path := httpPaths[eventCount%len(httpPaths)]
	host := httpHosts[eventCount%len(httpHosts)]
	srcPort := int32(40000 + (eventCount*137)%20000)

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("mock-network-http-%d", eventCount),
		Timestamp: time.Now(),
		Type:      domain.CollectorEventType(domain.EventTypeHTTP),
		Source:    o.name,
		Severity:  domain.EventSeverityInfo,
		EventData: o.createHTTPEventData(eventCount, method, path, host, srcPort),
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"protocol":  "TCP",
				"direction": "outbound",
				"l7":        "HTTP",
			},
		},
	}
}

// createHTTPEventData creates the event data for an HTTP mock event
func (o *Observer) createHTTPEventData(eventCount int, method, path, host string, srcPort int32) domain.EventDataContainer {
	return domain.EventDataContainer{
		Network: &domain.NetworkData{
			EventType:   "request",
			Protocol:    "TCP",
			SrcIP:       fmt.Sprintf("10.0.%d.%d", (eventCount/256)%256, eventCount%256),
			DstIP:       fmt.Sprintf("10.0.100.%d", (eventCount%50)+1),
			SrcPort:     srcPort,
			DstPort:     80,
			PayloadSize: int64(200 + (eventCount*43)%1000),
			Direction:   "outbound",
			L7Protocol:  "HTTP",
			L7Data: &domain.NetworkL7Data{
				Protocol: "HTTP",
				HTTPData: &domain.HTTPRequestData{
					Method: method,
					URL:    fmt.Sprintf("http://%s%s", host, path),
					Path:   path,
					Headers: map[string]string{
						"User-Agent": fmt.Sprintf("mock-client/%d.0", (eventCount%3)+1),
						"Host":       host,
					},
				},
			},
		},
		Process: &domain.ProcessData{
			PID:     int32(1000 + eventCount%1000),
			TID:     int32(1000 + eventCount%1000),
			Command: fmt.Sprintf("mock-app-%d", eventCount%5),
		},
		Custom: map[string]string{
			"mock":     "true",
			"platform": "non-linux",
		},
	}
}

// createMockDNSEvent generates a mock DNS query event
func (o *Observer) createMockDNSEvent(eventCount int) *domain.CollectorEvent {
	dnsDomains := []string{"example.com", "api.service.local", "database.prod.internal", "cache.redis.local"}
	dnsTypes := []string{"A", "AAAA", "CNAME", "MX"}
	dnsResolvers := []string{"8.8.8.8", "1.1.1.1", "10.0.0.2", "208.67.222.222"}

	dnsName := dnsDomains[eventCount%len(dnsDomains)]
	queryType := dnsTypes[eventCount%len(dnsTypes)]
	resolver := dnsResolvers[eventCount%len(dnsResolvers)]
	dnsSrcPort := int32(50000 + (eventCount*223)%10000)

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("mock-network-dns-%d", eventCount),
		Timestamp: time.Now().Add(100 * time.Millisecond),
		Type:      domain.EventTypeDNS,
		Source:    o.name,
		Severity:  domain.EventSeverityInfo,
		EventData: o.createDNSEventData(eventCount, dnsName, queryType, resolver, dnsSrcPort),
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"protocol": "UDP",
				"l7":       "DNS",
			},
		},
	}
}

// createDNSEventData creates the event data for a DNS mock event
func (o *Observer) createDNSEventData(eventCount int, dnsName, queryType, resolver string, srcPort int32) domain.EventDataContainer {
	answer := fmt.Sprintf("10.0.%d.%d", (eventCount*13)%256, (eventCount*17)%256)
	return domain.EventDataContainer{
		DNS: &domain.DNSData{
			QueryType:    queryType,
			QueryName:    dnsName,
			ResponseCode: 0,
			Answers:      []string{answer},
			Duration:     time.Duration((15 + eventCount%50)) * time.Millisecond,
			ServerIP:     resolver,
		},
		Network: &domain.NetworkData{
			EventType:   "query",
			Protocol:    "UDP",
			SrcIP:       fmt.Sprintf("10.0.%d.%d", (eventCount/256)%256, eventCount%256),
			DstIP:       resolver,
			SrcPort:     srcPort,
			DstPort:     53,
			PayloadSize: int64(40 + (eventCount*7)%100),
			Direction:   "outbound",
			L7Protocol:  "DNS",
			L7Data: &domain.NetworkL7Data{
				Protocol: "DNS",
				DNSData: &domain.DNSQueryData{
					Query:     dnsName,
					QueryType: queryType,
					Answers:   []string{answer},
				},
			},
		},
		Custom: map[string]string{
			"mock":     "true",
			"platform": "non-linux",
		},
	}
}
