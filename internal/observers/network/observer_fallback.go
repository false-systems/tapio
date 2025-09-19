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
				"observer":  "network",
				"version":   "1.0",
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
		HTTP: &domain.HTTPData{
			Method:       method,
			URL:          fmt.Sprintf("http://%s%s", host, path),
			StatusCode:   200,
			Duration:     time.Duration((10 + eventCount%100)) * time.Millisecond,
			RequestSize:  int64(100 + (eventCount*13)%500),
			ResponseSize: int64(200 + (eventCount*43)%1000),
		},
		Process: &domain.ProcessData{
			PID:     int32(1000 + eventCount%1000),
			TID:     int32(1000 + eventCount%1000),
			Command: fmt.Sprintf("mock-app-%d", eventCount%5),
		},
		Custom: map[string]string{
			"mock":     "true",
			"platform": "non-linux",
			"src_ip":   fmt.Sprintf("10.0.%d.%d", (eventCount/256)%256, eventCount%256),
			"dst_ip":   fmt.Sprintf("10.0.100.%d", (eventCount%50)+1),
			"src_port": fmt.Sprintf("%d", srcPort),
			"dst_port": "80",
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
				"observer": "network",
				"version":  "1.0",
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
			ClientIP:     fmt.Sprintf("10.0.%d.%d", (eventCount/256)%256, eventCount%256),
			ServerIP:     resolver,
		},
		Custom: map[string]string{
			"mock":      "true",
			"platform":  "non-linux",
			"src_port":  fmt.Sprintf("%d", srcPort),
			"dst_port":  "53",
			"protocol":  "UDP",
			"direction": "outbound",
		},
	}
}
