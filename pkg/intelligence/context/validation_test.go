package context

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestEventValidator_Validate(t *testing.T) {
	validator := NewEventValidator()

	tests := []struct {
		name    string
		event   *domain.UnifiedEvent
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil event",
			event:   nil,
			wantErr: true,
			errMsg:  "event is nil",
		},
		{
			name: "valid event",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeSystem,
				Source:    "test-collector",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			event: &domain.UnifiedEvent{
				Timestamp: time.Now(),
				Type:      domain.EventTypeSystem,
				Source:    "test-collector",
			},
			wantErr: true,
			errMsg:  "event missing ID",
		},
		{
			name: "missing timestamp",
			event: &domain.UnifiedEvent{
				ID:     "test-123",
				Type:   domain.EventTypeSystem,
				Source: "test-collector",
			},
			wantErr: true,
			errMsg:  "event missing timestamp",
		},
		{
			name: "missing type",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Source:    "test-collector",
			},
			wantErr: true,
			errMsg:  "event missing type",
		},
		{
			name: "missing source",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeSystem,
			},
			wantErr: true,
			errMsg:  "event missing source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
				}
			}
		})
	}
}

func TestEventValidator_ValidateEventAge(t *testing.T) {
	validator := NewEventValidator()

	tests := []struct {
		name      string
		timestamp time.Time
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "current event",
			timestamp: time.Now(),
			wantErr:   false,
		},
		{
			name:      "event 1 hour old",
			timestamp: time.Now().Add(-1 * time.Hour),
			wantErr:   false,
		},
		{
			name:      "event 23 hours old",
			timestamp: time.Now().Add(-23 * time.Hour),
			wantErr:   false,
		},
		{
			name:      "event 25 hours old",
			timestamp: time.Now().Add(-25 * time.Hour),
			wantErr:   true,
			errMsg:    "event too old",
		},
		{
			name:      "event 2 minutes in future",
			timestamp: time.Now().Add(2 * time.Minute),
			wantErr:   false,
		},
		{
			name:      "event 10 minutes in future",
			timestamp: time.Now().Add(10 * time.Minute),
			wantErr:   true,
			errMsg:    "event timestamp is too far in the future",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: tt.timestamp,
				Type:      domain.EventTypeSystem,
				Source:    "test",
			}
			err := validator.Validate(event)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEventAge() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateEventAge() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestEventValidator_ValidateLayerData(t *testing.T) {
	validator := NewEventValidator()

	tests := []struct {
		name    string
		event   *domain.UnifiedEvent
		wantErr bool
		errMsg  string
	}{
		{
			name: "CPU event with kernel data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeCPU,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					PID:     1234,
					Syscall: "sched_yield",
				},
			},
			wantErr: false,
		},
		{
			name: "CPU event without kernel data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeCPU,
				Source:    "ebpf",
			},
			wantErr: true,
			errMsg:  "kernel event missing kernel data",
		},
		{
			name: "kernel event with missing required fields",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeCPU,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					PID:     0,  // Valid PID
					Syscall: "", // Missing syscall to trigger validation error
				},
			},
			wantErr: true,
			errMsg:  "kernel event missing both syscall and comm",
		},
		{
			name: "memory event with kernel data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeMemory,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					PID:  1234,
					Comm: "test-process",
				},
			},
			wantErr: false,
		},
		{
			name: "memory event with application data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeMemory,
				Source:    "app",
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "out of memory",
				},
			},
			wantErr: false,
		},
		{
			name: "memory event without data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeMemory,
				Source:    "unknown",
			},
			wantErr: true,
			errMsg:  "memory event missing both kernel and application data",
		},
		{
			name: "network event with network data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol:   "TCP",
					SourceIP:   "192.168.1.1",
					SourcePort: 8080,
					DestIP:     "10.0.0.1",
					DestPort:   443,
				},
			},
			wantErr: false,
		},
		{
			name: "network event without network data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
			},
			wantErr: true,
			errMsg:  "network event missing network data",
		},
		{
			name: "network event with invalid source IP",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol:   "TCP",
					SourceIP:   "invalid-ip",
					SourcePort: 8080,
				},
			},
			wantErr: true,
			errMsg:  "network event has invalid source IP",
		},
		{
			name: "TCP network event with no ports",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeNetwork,
				Source:    "cni",
				Network: &domain.NetworkData{
					Protocol:   "TCP",
					SourcePort: 0,
					DestPort:   0,
				},
			},
			wantErr: true,
			errMsg:  "TCP network event missing both source and destination ports",
		},
		{
			name: "application event with application data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeLog,
				Source:    "app",
				Application: &domain.ApplicationData{
					Level:   "error",
					Message: "test error",
					Logger:  "test.logger",
				},
			},
			wantErr: false,
		},
		{
			name: "application event without application data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeLog,
				Source:    "app",
			},
			wantErr: true,
			errMsg:  "application event missing application data",
		},
		{
			name: "application event missing level",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeLog,
				Source:    "app",
				Application: &domain.ApplicationData{
					Message: "test message",
				},
			},
			wantErr: true,
			errMsg:  "application event missing log level",
		},
		{
			name: "infrastructure event with kubernetes data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKubernetes,
				Source:    "k8s",
				Kubernetes: &domain.KubernetesData{
					EventType: "Normal",
					Object:    "Pod/nginx-123",
				},
			},
			wantErr: false,
		},
		{
			name: "infrastructure event with entity data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKubernetes,
				Source:    "k8s",
				Entity: &domain.EntityContext{
					Type: "Pod",
					Name: "nginx-123",
				},
			},
			wantErr: false,
		},
		{
			name: "infrastructure event without data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeKubernetes,
				Source:    "k8s",
			},
			wantErr: true,
			errMsg:  "infrastructure event missing both kubernetes and entity data",
		},
		{
			name: "process event with kernel data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeProcess,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					PID:  1234,
					Comm: "nginx",
				},
			},
			wantErr: false,
		},
		{
			name: "process event without kernel data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeProcess,
				Source:    "ebpf",
			},
			wantErr: true,
			errMsg:  "process event missing kernel data",
		},
		{
			name: "process event without PID",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeProcess,
				Source:    "ebpf",
				Kernel: &domain.KernelData{
					Comm: "nginx",
				},
			},
			wantErr: true,
			errMsg:  "process event missing PID",
		},
		{
			name: "system event without specific data",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      domain.EventTypeSystem,
				Source:    "system",
			},
			wantErr: false,
		},
		{
			name: "unknown event type",
			event: &domain.UnifiedEvent{
				ID:        "test-123",
				Timestamp: time.Now(),
				Type:      "unknown-type",
				Source:    "test",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
				t.Errorf("Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestEventValidator_IsValidIP(t *testing.T) {
	tests := []struct {
		name  string
		ip    string
		valid bool
	}{
		{
			name:  "valid IPv4",
			ip:    "192.168.1.1",
			valid: true,
		},
		{
			name:  "valid IPv6",
			ip:    "2001:db8::1",
			valid: true,
		},
		{
			name:  "empty string",
			ip:    "",
			valid: false,
		},
		{
			name:  "invalid format",
			ip:    "not-an-ip",
			valid: false,
		},
		{
			name:  "mixed IPv4 and IPv6",
			ip:    "192.168.1.1::1",
			valid: false,
		},
		{
			name:  "localhost IPv4",
			ip:    "127.0.0.1",
			valid: true,
		},
		{
			name:  "localhost IPv6",
			ip:    "::1",
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidIP(tt.ip); got != tt.valid {
				t.Errorf("isValidIP(%v) = %v, want %v", tt.ip, got, tt.valid)
			}
		})
	}
}

func TestNewEventValidatorWithConfig(t *testing.T) {
	customAge := 12 * time.Hour
	validator := NewEventValidatorWithConfig(customAge)

	if validator.maxEventAge != customAge {
		t.Errorf("NewEventValidatorWithConfig() maxEventAge = %v, want %v", validator.maxEventAge, customAge)
	}

	// Test that custom age is enforced
	event := &domain.UnifiedEvent{
		ID:        "test-123",
		Timestamp: time.Now().Add(-13 * time.Hour),
		Type:      domain.EventTypeSystem,
		Source:    "test",
	}

	err := validator.Validate(event)
	if err == nil {
		t.Error("Expected error for event older than custom max age")
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 &&
		(s[0:len(substr)] == substr || (len(s) > len(substr) && contains(s[1:], substr))))
}
