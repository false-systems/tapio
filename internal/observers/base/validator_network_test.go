package base

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestValidateDNSEvent(t *testing.T) {
	v := NewEventValidator("dns", zap.NewNop(), false) // Use non-strict mode

	tests := []struct {
		name      string
		event     *domain.CollectorEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "DNS event without query name",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeDNS,
				Source:    "dns",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					DNS: &domain.DNSData{
						QueryType: "A",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "dns",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "DNS QueryName is required",
		},
		{
			name: "DNS event without query type",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeDNS,
				Source:    "dns",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					DNS: &domain.DNSData{
						QueryName: "example.com",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "dns",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "DNS QueryType is required",
		},
		{
			name: "Valid DNS event",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeDNS,
				Source:    "dns",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					DNS: &domain.DNSData{
						QueryName: "example.com",
						QueryType: "A",
						ClientIP:  "192.168.1.1",
						ServerIP:  "8.8.8.8",
						Duration:  100 * time.Millisecond,
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "dns",
						"version":  "1.0.0",
					},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateEvent(tt.event)
			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNetworkEvent(t *testing.T) {
	v := NewEventValidator("network", zap.NewNop(), false) // Use non-strict mode

	tests := []struct {
		name      string
		event     *domain.CollectorEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "Valid network L4 event",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetworkConnection,
				Source:    "network",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Network: &domain.NetworkData{
						SrcIP:    "192.168.1.1",
						DstIP:    "192.168.1.2",
						SrcPort:  12345,
						DstPort:  80,
						Protocol: "TCP",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "network",
						"version":  "1.0.0",
					},
				},
			},
			wantError: false,
		},
		{
			name: "HTTP event should use HTTP field",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeHTTP,
				Source:    "network",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Network: &domain.NetworkData{ // Wrong field!
						SrcIP: "192.168.1.1",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "network",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "HTTP events must use HTTP field",
		},
		{
			name: "Valid HTTP event",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeHTTP,
				Source:    "network",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					HTTP: &domain.HTTPData{
						Method:     "GET",
						URL:        "/api/test",
						StatusCode: 200,
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "network",
						"version":  "1.0.0",
					},
				},
			},
			wantError: false,
		},
		{
			name: "gRPC event should use GRPC field",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeGRPC,
				Source:    "network",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Network: &domain.NetworkData{ // Wrong field!
						SrcIP: "192.168.1.1",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "network",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "gRPC events must use GRPC field",
		},
		{
			name: "Valid gRPC event",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeGRPC,
				Source:    "network",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					GRPC: &domain.GRPCData{
						Method:     "GetUser",
						StatusCode: 0,
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "network",
						"version":  "1.0.0",
					},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateEvent(tt.event)
			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
