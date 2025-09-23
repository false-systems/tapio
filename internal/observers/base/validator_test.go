package base

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestNewEventValidator(t *testing.T) {
	tests := []struct {
		name         string
		observerType string
		logger       *zap.Logger
		strictMode   bool
	}{
		{
			name:         "With logger",
			observerType: "test",
			logger:       zap.NewNop(),
			strictMode:   true,
		},
		{
			name:         "Without logger (nil)",
			observerType: "test",
			logger:       nil,
			strictMode:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewEventValidator(tt.observerType, tt.logger, tt.strictMode)
			assert.NotNil(t, v)
			assert.Equal(t, tt.observerType, v.observerType)
			assert.Equal(t, tt.strictMode, v.strictMode)
			assert.NotNil(t, v.logger) // Should never be nil (uses NopLogger if nil)
		})
	}
}

func TestValidateEvent_RequiredFields(t *testing.T) {
	v := NewEventValidator("test", zap.NewNop(), false) // Use non-strict mode to avoid panics in tests

	tests := []struct {
		name      string
		event     *domain.CollectorEvent
		wantError bool
		errorMsg  string
	}{
		{
			name:      "Nil event",
			event:     nil,
			wantError: true,
			errorMsg:  "event is nil",
		},
		{
			name: "Missing EventID",
			event: &domain.CollectorEvent{
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
			},
			wantError: true,
			errorMsg:  "missing required fields: EventID",
		},
		{
			name: "Missing Timestamp",
			event: &domain.CollectorEvent{
				EventID:  "test-1",
				Type:     domain.EventTypeKernelProcess,
				Source:   "test",
				Severity: domain.EventSeverityInfo,
			},
			wantError: true,
			errorMsg:  "missing required fields: Timestamp",
		},
		{
			name: "Missing Type",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
			},
			wantError: true,
			errorMsg:  "missing required fields: Type",
		},
		{
			name: "Missing Source",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Severity:  domain.EventSeverityInfo,
			},
			wantError: true,
			errorMsg:  "missing required fields: Source",
		},
		{
			name: "Missing Severity",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
			},
			wantError: true,
			errorMsg:  "missing required fields: Severity",
		},
		{
			name: "Valid event with all required fields",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
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
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateEvent_TypeMatchesData(t *testing.T) {
	v := NewEventValidator("test", zap.NewNop(), false) // Use non-strict mode

	tests := []struct {
		name      string
		event     *domain.CollectorEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "Process event without process data",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "requires Process field",
		},
		{
			name: "Network event without network data",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetworkConnection,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "requires Network field",
		},
		{
			name: "DNS event without DNS data",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeDNS,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "requires DNS field",
		},
		{
			name: "Storage IO event without storage data",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeStorageIO,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "requires StorageIO field",
		},
		{
			name: "Valid process event with data",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
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
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateMetadata(t *testing.T) {
	v := NewEventValidator("test", zap.NewNop(), false) // Use non-strict mode

	tests := []struct {
		name      string
		event     *domain.CollectorEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "Missing Labels",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{},
			},
			wantError: true,
			errorMsg:  "Metadata.Labels is required",
		},
		{
			name: "Missing observer label",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"version": "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "Metadata label 'observer' should be",
		},
		{
			name: "Missing version label",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
					},
				},
			},
			wantError: true,
			errorMsg:  "Metadata label 'version' is required",
		},
		{
			name: "Valid metadata with all required labels",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
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
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

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

func TestValidateStorageIOEvent(t *testing.T) {
	v := NewEventValidator("storage-io", zap.NewNop(), false) // Use non-strict mode

	tests := []struct {
		name      string
		event     *domain.CollectorEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "Storage IO event without path",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeStorageIO,
				Source:    "storage-io",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					StorageIO: &domain.StorageIOData{
						Operation: "read",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "storage-io",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "StorageIO Path is required",
		},
		{
			name: "Storage IO event without operation",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeStorageIO,
				Source:    "storage-io",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					StorageIO: &domain.StorageIOData{
						Path: "/var/log/test.log",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "storage-io",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "StorageIO Operation is required",
		},
		{
			name: "Valid Storage IO event",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeStorageIO,
				Source:    "storage-io",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					StorageIO: &domain.StorageIOData{
						Path:      "/var/log/test.log",
						Operation: "read",
						Size:      4096,
						Duration:  10 * time.Millisecond,
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "storage-io",
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

func TestValidateMemoryEvent(t *testing.T) {
	v := NewEventValidator("memory", zap.NewNop(), false) // Use non-strict mode

	tests := []struct {
		name      string
		event     *domain.CollectorEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "Memory event without operation",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeMemoryAllocation,
				Source:    "memory",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Memory: &domain.MemoryData{
						Size: 1024,
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "memory",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
			errorMsg:  "Memory Operation is required",
		},
		{
			name: "Valid memory event",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeMemoryAllocation,
				Source:    "memory",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Memory: &domain.MemoryData{
						Operation: "malloc",
						Size:      1024,
						Address:   0x7fff5fbff000,
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "memory",
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

func TestValidateProcessSignalsEvent(t *testing.T) {
	tests := []struct {
		name      string
		event     *domain.CollectorEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "Process signals event without required fields",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "process-signals",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID: 1234,
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "process-signals",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true, // Missing Command field
		},
		{
			name: "Valid process signals event",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "process-signals",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test-process",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "process-signals",
						"version":  "1.0.0",
					},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewEventValidator("process-signals", zap.NewNop(), false) // Use non-strict mode
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

func TestValidateNoMapAbuse(t *testing.T) {
	tests := []struct {
		name       string
		event      *domain.CollectorEvent
		strictMode bool
		wantError  bool
		errorMsg   string
	}{
		{
			name: "Event with small Custom field is OK",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
					Custom: map[string]string{
						"key": "value",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			strictMode: false,
			wantError:  false, // Custom field is allowed to have map[string]string
		},
		{
			name: "Event with too many Custom entries",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
					Custom: map[string]string{
						"key1": "value1", "key2": "value2", "key3": "value3",
						"key4": "value4", "key5": "value5", "key6": "value6",
						"key7": "value7", "key8": "value8", "key9": "value9",
						"key10": "value10", "key11": "value11",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			strictMode: false,
			wantError:  true,
			errorMsg:   "Custom field has 11 entries",
		},
		{
			name: "Event with dns_ prefix in Custom without DNS field",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
					Custom: map[string]string{
						"dns_server": "8.8.8.8",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			strictMode: false,
			wantError:  true,
			errorMsg:   "protocol-specific data in Custom field should use typed fields: dns_server",
		},
		{
			name: "Event with http_ prefix in Custom without HTTP field",
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
					Custom: map[string]string{
						"http_status": "200",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			strictMode: false,
			wantError:  true,
			errorMsg:   "protocol-specific data in Custom field should use typed fields: http_status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewEventValidator("test", zap.NewNop(), tt.strictMode)
			err := validator.ValidateEvent(tt.event)
			if tt.wantError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateEvent_StrictMode(t *testing.T) {
	tests := []struct {
		name       string
		strictMode bool
		event      *domain.CollectorEvent
		wantPanic  bool
		wantError  bool
	}{
		{
			name:       "Strict mode - validation failure causes panic",
			strictMode: true,
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				// Missing Severity field
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			wantPanic: true,
		},
		{
			name:       "Non-strict mode - validation failure returns error",
			strictMode: false,
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				// Missing Severity field
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			wantError: true,
		},
		{
			name:       "Valid event passes in any mode",
			strictMode: true,
			event: &domain.CollectorEvent{
				EventID:   "test-1",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelProcess,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					Process: &domain.ProcessData{
						PID:     1234,
						Command: "test",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			},
			wantPanic: false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewEventValidator("test", zap.NewNop(), tt.strictMode)

			if tt.wantPanic {
				assert.Panics(t, func() {
					v.ValidateEvent(tt.event)
				})
			} else {
				err := v.ValidateEvent(tt.event)
				if tt.wantError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			}
		})
	}
}
