package base

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

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
