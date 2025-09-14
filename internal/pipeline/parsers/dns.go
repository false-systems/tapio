package parsers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
)

// DNSEvent represents the structure of DNS events
type DNSEvent struct {
	QueryID      uint16   `json:"query_id"`
	QueryType    string   `json:"query_type"`
	QueryName    string   `json:"query_name"`
	ResponseIPs  []string `json:"response_ips,omitempty"`
	ResponseCode string   `json:"response_code"`
	LatencyMS    int64    `json:"latency_ms"`
	PID          int32    `json:"pid,omitempty"`
	ContainerID  string   `json:"container_id,omitempty"`
	PodName      string   `json:"pod_name,omitempty"`
	Namespace    string   `json:"namespace,omitempty"`
}

// DNSParser parses DNS events
type DNSParser struct{}

// NewDNSParser creates a new DNS event parser
func NewDNSParser() *DNSParser {
	return &DNSParser{}
}

// Source returns the source this parser handles
func (p *DNSParser) Source() string {
	return "dns"
}

// Parse converts a DNS RawEvent to an ObservationEvent
func (p *DNSParser) Parse(raw *domain.RawEvent) (*domain.ObservationEvent, error) {
	if raw == nil {
		return nil, fmt.Errorf("cannot parse nil event")
	}

	if raw.Source != "dns" {
		return nil, fmt.Errorf("invalid source: expected dns, got %s", raw.Source)
	}

	// Parse the DNS event from Data
	var dnsEvent DNSEvent
	if err := json.Unmarshal(raw.Data, &dnsEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DNS event: %w", err)
	}

	// Create observation event
	obs := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Timestamp: raw.Timestamp,
		Source:    "dns",
		Type:      fmt.Sprintf("dns.%s", strings.ToLower(dnsEvent.QueryType)),
	}

	// Add correlation keys
	if dnsEvent.PID > 0 {
		obs.PID = &dnsEvent.PID
	}

	if dnsEvent.ContainerID != "" {
		obs.ContainerID = &dnsEvent.ContainerID
	}

	if dnsEvent.PodName != "" {
		obs.PodName = &dnsEvent.PodName
	}

	if dnsEvent.Namespace != "" {
		obs.Namespace = &dnsEvent.Namespace
	}

	// Set action and target
	action := "query"
	obs.Action = &action
	obs.Target = &dnsEvent.QueryName

	// Set result
	result := dnsEvent.ResponseCode
	if result == "" {
		result = "success"
	}
	obs.Result = &result

	// Set metrics
	obs.Duration = &dnsEvent.LatencyMS

	// Add additional data
	obs.Data = make(map[string]string)
	obs.Data["query_id"] = fmt.Sprintf("%d", dnsEvent.QueryID)
	obs.Data["query_type"] = dnsEvent.QueryType

	if len(dnsEvent.ResponseIPs) > 0 {
		obs.Data["response_ips"] = strings.Join(dnsEvent.ResponseIPs, ",")
		count := int32(len(dnsEvent.ResponseIPs))
		obs.Count = &count
	}

	return obs, nil
}
