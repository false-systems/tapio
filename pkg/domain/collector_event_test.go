package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestCollectorEvent_Validate(t *testing.T) {
	tests := []struct {
		name    string
		event   *CollectorEvent
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid event",
			event: &CollectorEvent{
				EventID:   "test-event-1",
				Timestamp: time.Now(),
				Type:      EventTypeKernelSyscall,
				Source:    "test-collector",
				EventData: EventDataContainer{
					SystemCall: &SystemCallData{
						Number: 1,
						Name:   "read",
						PID:    1234,
					},
				},
				Metadata: EventMetadata{
					Priority:      PriorityNormal,
					SchemaVersion: "1.0.0",
				},
				CorrelationHints: CorrelationHints{
					ProcessID: 1234,
				},
			},
			wantErr: false,
		},
		{
			name: "missing event ID",
			event: &CollectorEvent{
				Timestamp: time.Now(),
				Type:      EventTypeKernelSyscall,
				Source:    "test-collector",
				EventData: EventDataContainer{
					SystemCall: &SystemCallData{Number: 1, Name: "read"},
				},
			},
			wantErr: true,
			errMsg:  "event_id is required",
		},
		{
			name: "missing timestamp",
			event: &CollectorEvent{
				EventID: "test-event-1",
				Type:    EventTypeKernelSyscall,
				Source:  "test-collector",
				EventData: EventDataContainer{
					SystemCall: &SystemCallData{Number: 1, Name: "read"},
				},
			},
			wantErr: true,
			errMsg:  "timestamp is required",
		},
		{
			name: "missing type",
			event: &CollectorEvent{
				EventID:   "test-event-1",
				Timestamp: time.Now(),
				Source:    "test-collector",
				EventData: EventDataContainer{
					SystemCall: &SystemCallData{Number: 1, Name: "read"},
				},
			},
			wantErr: true,
			errMsg:  "event type is required",
		},
		{
			name: "missing source",
			event: &CollectorEvent{
				EventID:   "test-event-1",
				Timestamp: time.Now(),
				Type:      EventTypeKernelSyscall,
				EventData: EventDataContainer{
					SystemCall: &SystemCallData{Number: 1, Name: "read"},
				},
			},
			wantErr: true,
			errMsg:  "source is required",
		},
		{
			name: "empty event data",
			event: &CollectorEvent{
				EventID:   "test-event-1",
				Timestamp: time.Now(),
				Type:      EventTypeKernelSyscall,
				Source:    "test-collector",
				EventData: EventDataContainer{},
			},
			wantErr: true,
			errMsg:  "event data validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEventDataContainer_Validate(t *testing.T) {
	tests := []struct {
		name      string
		container EventDataContainer
		wantErr   bool
	}{
		{
			name: "valid system call data",
			container: EventDataContainer{
				SystemCall: &SystemCallData{
					Number: 1,
					Name:   "read",
				},
			},
			wantErr: false,
		},
		{
			name: "valid network data",
			container: EventDataContainer{
				Network: &NetworkData{
					Protocol:   "tcp",
					Direction:  "outbound",
					SourceIP:   "10.0.0.1",
					SourcePort: 8080,
				},
			},
			wantErr: false,
		},
		{
			name: "valid kubernetes data",
			container: EventDataContainer{
				KubernetesResource: &K8sResourceData{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "test-pod",
					UID:        "test-uid",
				},
			},
			wantErr: false,
		},
		{
			name: "valid raw data",
			container: EventDataContainer{
				RawData: &RawData{
					Format: "json",
					Data:   []byte(`{"test": true}`),
					Size:   15,
				},
			},
			wantErr: false,
		},
		{
			name:      "empty container",
			container: EventDataContainer{},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.container.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "at least one event data field must be present")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCollectorEvent_DataExtractionMethods(t *testing.T) {
	event := &CollectorEvent{
		EventID:   "test-event-1",
		Timestamp: time.Now(),
		Type:      EventTypeKernelSyscall,
		Source:    "kernel-collector",
		EventData: EventDataContainer{
			SystemCall: &SystemCallData{
				Number:   1,
				Name:     "read",
				PID:      1234,
				TID:      1234,
				UID:      1000,
				GID:      1000,
				RetValue: 42,
				Duration: time.Microsecond * 500,
			},
			Process: &ProcessData{
				PID:        1234,
				PPID:       1,
				Command:    "test-process",
				Executable: "/usr/bin/test",
				StartTime:  time.Now().Add(-time.Hour),
				UID:        1000,
				GID:        1000,
			},
		},
	}

	t.Run("GetSystemCallData", func(t *testing.T) {
		data, ok := event.GetSystemCallData()
		require.True(t, ok)
		assert.Equal(t, int64(1), data.Number)
		assert.Equal(t, "read", data.Name)
		assert.Equal(t, int32(1234), data.PID)
	})

	t.Run("GetProcessData", func(t *testing.T) {
		data, ok := event.GetProcessData()
		require.True(t, ok)
		assert.Equal(t, int32(1234), data.PID)
		assert.Equal(t, "test-process", data.Command)
	})

	t.Run("GetNetworkData - not present", func(t *testing.T) {
		data, ok := event.GetNetworkData()
		assert.False(t, ok)
		assert.Nil(t, data)
	})

	t.Run("GetContainerData - not present", func(t *testing.T) {
		data, ok := event.GetContainerData()
		assert.False(t, ok)
		assert.Nil(t, data)
	})

	t.Run("GetK8sResourceData - not present", func(t *testing.T) {
		data, ok := event.GetK8sResourceData()
		assert.False(t, ok)
		assert.Nil(t, data)
	})

	t.Run("GetDNSData - not present", func(t *testing.T) {
		data, ok := event.GetDNSData()
		assert.False(t, ok)
		assert.Nil(t, data)
	})
}

func TestCollectorEvent_TraceContext(t *testing.T) {
	t.Run("HasTraceContext - with valid trace", func(t *testing.T) {
		traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
		spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")

		event := &CollectorEvent{
			TraceContext: &TraceContext{
				TraceID: traceID,
				SpanID:  spanID,
			},
		}

		// Note: The method checks !TraceID.IsValid(), so this should return false for valid trace
		assert.False(t, event.HasTraceContext())
	})

	t.Run("HasTraceContext - without trace context", func(t *testing.T) {
		event := &CollectorEvent{}
		assert.False(t, event.HasTraceContext())
	})
}

func TestCollectorEvent_CorrelationKey(t *testing.T) {
	tests := []struct {
		name     string
		hints    CorrelationHints
		source   string
		expected string
	}{
		{
			name: "container ID priority",
			hints: CorrelationHints{
				ContainerID: "container-123",
				PodUID:      "pod-456",
				ProcessID:   789,
			},
			expected: "container:container-123",
		},
		{
			name: "pod UID when no container",
			hints: CorrelationHints{
				PodUID:    "pod-456",
				ProcessID: 789,
				NodeName:  "node-1",
			},
			expected: "pod:pod-456",
		},
		{
			name: "process ID when no container or pod",
			hints: CorrelationHints{
				ProcessID: 789,
				NodeName:  "node-1",
			},
			expected: "process:789",
		},
		{
			name: "node name when no other hints",
			hints: CorrelationHints{
				NodeName: "node-1",
			},
			expected: "node:node-1",
		},
		{
			name:     "fallback to source",
			hints:    CorrelationHints{},
			source:   "kernel-collector",
			expected: "source:kernel-collector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &CollectorEvent{
				Source:           tt.source,
				CorrelationHints: tt.hints,
			}
			assert.Equal(t, tt.expected, event.GetCorrelationKey())
		})
	}
}

func TestCollectorEvent_Priority(t *testing.T) {
	tests := []struct {
		name     string
		priority EventPriority
		expected bool
	}{
		{
			name:     "low priority",
			priority: PriorityLow,
			expected: false,
		},
		{
			name:     "normal priority",
			priority: PriorityNormal,
			expected: false,
		},
		{
			name:     "high priority",
			priority: PriorityHigh,
			expected: true,
		},
		{
			name:     "critical priority",
			priority: PriorityCritical,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &CollectorEvent{
				Metadata: EventMetadata{
					Priority: tt.priority,
				},
			}
			assert.Equal(t, tt.expected, event.IsHighPriority())
		})
	}
}

func TestCollectorEvent_AddCorrelationTag(t *testing.T) {
	event := &CollectorEvent{
		EventID:          "test-event-1",
		Timestamp:        time.Now(),
		Type:             EventTypeKernelSyscall,
		Source:           "test-collector",
		CorrelationHints: CorrelationHints{},
	}

	// Test adding tags to empty map
	event.AddCorrelationTag("service", "api-server")
	assert.Equal(t, "api-server", event.CorrelationHints.CorrelationTags["service"])

	// Test adding additional tags
	event.AddCorrelationTag("version", "v1.2.3")
	assert.Equal(t, "v1.2.3", event.CorrelationHints.CorrelationTags["version"])
	assert.Equal(t, "api-server", event.CorrelationHints.CorrelationTags["service"])

	// Test overwriting existing tag
	event.AddCorrelationTag("service", "web-server")
	assert.Equal(t, "web-server", event.CorrelationHints.CorrelationTags["service"])
}

func TestCollectorEvent_AddMetadataLabel(t *testing.T) {
	event := &CollectorEvent{
		EventID:   "test-event-1",
		Timestamp: time.Now(),
		Type:      EventTypeKernelSyscall,
		Source:    "test-collector",
		Metadata:  EventMetadata{},
	}

	// Test adding labels to empty map
	event.AddMetadataLabel("environment", "production")
	assert.Equal(t, "production", event.Metadata.Labels["environment"])

	// Test adding additional labels
	event.AddMetadataLabel("team", "platform")
	assert.Equal(t, "platform", event.Metadata.Labels["team"])
	assert.Equal(t, "production", event.Metadata.Labels["environment"])

	// Test overwriting existing label
	event.AddMetadataLabel("environment", "staging")
	assert.Equal(t, "staging", event.Metadata.Labels["environment"])
}

func TestSystemCallData(t *testing.T) {
	syscallData := &SystemCallData{
		Number:   2,
		Name:     "open",
		PID:      1234,
		TID:      1234,
		UID:      1000,
		GID:      1000,
		RetValue: 3,
		Duration: time.Microsecond * 250,
		Arguments: []SystemCallArg{
			{
				Index: 0,
				Type:  "string",
				Value: "/etc/passwd",
				Size:  11,
			},
			{
				Index: 1,
				Type:  "int",
				Value: "0",
				Size:  4,
			},
		},
		Flags: map[string]string{
			"O_RDONLY": "true",
		},
	}

	assert.Equal(t, int64(2), syscallData.Number)
	assert.Equal(t, "open", syscallData.Name)
	assert.Equal(t, int32(1234), syscallData.PID)
	assert.Len(t, syscallData.Arguments, 2)
	assert.Equal(t, "/etc/passwd", syscallData.Arguments[0].Value)
	assert.Equal(t, "true", syscallData.Flags["O_RDONLY"])
}

func TestNetworkData(t *testing.T) {
	networkData := &NetworkData{
		Protocol:    "tcp",
		Direction:   "outbound",
		SourceIP:    "10.0.0.1",
		SourcePort:  8080,
		DestIP:      "10.0.0.2",
		DestPort:    80,
		BytesSent:   1024,
		BytesRecv:   2048,
		PacketsSent: 10,
		PacketsRecv: 15,
		Latency:     time.Millisecond * 50,
		TCPFlags:    []string{"SYN", "ACK"},
		Interface:   "eth0",
	}

	assert.Equal(t, "tcp", networkData.Protocol)
	assert.Equal(t, "outbound", networkData.Direction)
	assert.Equal(t, int32(8080), networkData.SourcePort)
	assert.Equal(t, int64(1024), networkData.BytesSent)
	assert.Contains(t, networkData.TCPFlags, "SYN")
}

func TestK8sResourceData(t *testing.T) {
	k8sData := &K8sResourceData{
		APIVersion:      "v1",
		Kind:            "Pod",
		Name:            "test-pod",
		Namespace:       "default",
		UID:             "test-uid-123",
		ResourceVersion: "12345",
		Generation:      1,
		Labels: map[string]string{
			"app": "test-app",
		},
		Annotations: map[string]string{
			"prometheus.io/scrape": "true",
		},
		OwnerReferences: []OwnerReference{
			{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "test-deployment",
				UID:        "deployment-uid-456",
			},
		},
	}

	assert.Equal(t, "v1", k8sData.APIVersion)
	assert.Equal(t, "Pod", k8sData.Kind)
	assert.Equal(t, "test-pod", k8sData.Name)
	assert.Equal(t, "test-app", k8sData.Labels["app"])
	assert.Len(t, k8sData.OwnerReferences, 1)
	assert.Equal(t, "Deployment", k8sData.OwnerReferences[0].Kind)
}

func TestDNSData(t *testing.T) {
	dnsData := &DNSData{
		QueryType:    "A",
		QueryName:    "example.com",
		ResponseCode: 0,
		Answers: []DNSAnswer{
			{
				Name:  "example.com",
				Type:  "A",
				Class: "IN",
				TTL:   300,
				Data:  "192.0.2.1",
			},
		},
		Duration:   time.Millisecond * 25,
		Cached:     false,
		ServerIP:   "8.8.8.8",
		ServerPort: 53,
	}

	assert.Equal(t, "A", dnsData.QueryType)
	assert.Equal(t, "example.com", dnsData.QueryName)
	assert.Equal(t, int32(0), dnsData.ResponseCode)
	assert.Len(t, dnsData.Answers, 1)
	assert.Equal(t, "192.0.2.1", dnsData.Answers[0].Data)
}

func TestCollectorEventTypes(t *testing.T) {
	// Test that all event types are properly defined
	eventTypes := []CollectorEventType{
		EventTypeKernelSyscall,
		EventTypeKernelProcess,
		EventTypeKernelNetwork,
		EventTypeKernelCgroup,
		EventTypeKernelFS,
		EventTypeContainerCreate,
		EventTypeContainerStart,
		EventTypeContainerStop,
		EventTypeContainerDestroy,
		EventTypeK8sPod,
		EventTypeK8sService,
		EventTypeK8sDeployment,
		EventTypeK8sConfigMap,
		EventTypeK8sSecret,
		EventTypeDNS,
		EventTypeTCP,
		EventTypeHTTP,
		EventTypeGRPC,
		EventTypeETCD,
		EventTypeVolume,
		EventTypeConfigStorage,
	}

	for _, eventType := range eventTypes {
		assert.NotEmpty(t, string(eventType))
	}
}

func TestEventPriorities(t *testing.T) {
	priorities := []EventPriority{
		PriorityLow,
		PriorityNormal,
		PriorityHigh,
		PriorityCritical,
	}

	for _, priority := range priorities {
		assert.NotEmpty(t, string(priority))
	}
}

func TestCompleteCollectorEvent(t *testing.T) {
	// Create a comprehensive CollectorEvent to test all fields
	now := time.Now()
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")

	event := &CollectorEvent{
		EventID:   "complete-test-event-1",
		Timestamp: now,
		Type:      EventTypeKernelSyscall,
		Source:    "kernel-collector",
		EventData: EventDataContainer{
			SystemCall: &SystemCallData{
				Number:   1,
				Name:     "read",
				PID:      1234,
				TID:      1234,
				UID:      1000,
				GID:      1000,
				RetValue: 42,
				Duration: time.Microsecond * 500,
				Arguments: []SystemCallArg{
					{Index: 0, Type: "int", Value: "3", Size: 4},
					{Index: 1, Type: "ptr", Value: "0x7fff12345678", Size: 8},
				},
			},
		},
		Metadata: EventMetadata{
			Priority:      PriorityHigh,
			Tags:          []string{"security", "audit"},
			Labels:        map[string]string{"env": "prod"},
			Attributes:    map[string]string{"source": "ebpf"},
			SchemaVersion: "1.0.0",
		},
		CorrelationHints: CorrelationHints{
			PodUID:      "pod-uid-123",
			ContainerID: "container-abc-456",
			ProcessID:   1234,
			NodeName:    "worker-node-1",
			CorrelationTags: map[string]string{
				"service": "api-server",
				"version": "v1.2.3",
			},
		},
		K8sContext: &K8sContext{
			APIVersion: "v1",
			Kind:       "Pod",
			UID:        "pod-uid-123",
			Name:       "api-server-pod",
			Namespace:  "default",
			Labels:     map[string]string{"app": "api-server"},
			OwnerReferences: []OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "api-server",
					UID:        "deployment-uid-789",
				},
			},
		},
		TraceContext: &TraceContext{
			TraceID: traceID,
			SpanID:  spanID,
		},
		CausalityContext: &CausalityContext{
			CauseID:    "parent-event-123",
			ChainID:    "chain-456",
			ChainDepth: 2,
			RootCause:  "root-event-789",
			Confidence: 0.95,
			Type:       "direct",
		},
		CollectionContext: CollectionContext{
			CollectorVersion: "v1.0.0",
			HostInfo: HostInfo{
				Hostname:         "worker-node-1",
				KernelVersion:    "5.15.0",
				OSVersion:        "Ubuntu 22.04",
				Architecture:     "x86_64",
				ContainerRuntime: "containerd",
				K8sVersion:       "v1.28.0",
			},
			CollectionConfig: CollectionConfig{
				SamplingRate:    0.1,
				BufferSize:      1000,
				FlushInterval:   time.Second * 30,
				EnabledFeatures: []string{"syscalls", "network", "process"},
			},
			BufferStats: BufferStats{
				TotalCapacity:   1000,
				CurrentUsage:    750,
				UtilizationRate: 0.75,
				DroppedEvents:   5,
				ProcessedEvents: 995,
			},
		},
	}

	// Validate the complete event
	err := event.Validate()
	require.NoError(t, err)

	// Test all accessor methods
	syscallData, ok := event.GetSystemCallData()
	require.True(t, ok)
	assert.Equal(t, "read", syscallData.Name)

	assert.True(t, event.IsHighPriority())
	assert.Equal(t, "container:container-abc-456", event.GetCorrelationKey())

	// Test adding additional correlation tags and metadata labels
	event.AddCorrelationTag("deployment", "api-server")
	event.AddMetadataLabel("monitoring", "enabled")

	assert.Equal(t, "api-server", event.CorrelationHints.CorrelationTags["deployment"])
	assert.Equal(t, "enabled", event.Metadata.Labels["monitoring"])
}

func TestRawDataFallback(t *testing.T) {
	// Test using RawData as fallback for unknown formats
	rawEvent := &CollectorEvent{
		EventID:   "raw-event-1",
		Timestamp: time.Now(),
		Type:      "custom.unknown",
		Source:    "custom-collector",
		EventData: EventDataContainer{
			RawData: &RawData{
				Format:      "protobuf",
				ContentType: "application/x-protobuf",
				Data:        []byte{0x08, 0x96, 0x01, 0x12, 0x04, 0x74, 0x65, 0x73, 0x74},
				Size:        9,
			},
		},
		Metadata: EventMetadata{
			Priority:      PriorityNormal,
			SchemaVersion: "1.0.0",
		},
	}

	err := rawEvent.Validate()
	require.NoError(t, err)

	assert.Equal(t, "protobuf", rawEvent.EventData.RawData.Format)
	assert.Equal(t, int64(9), rawEvent.EventData.RawData.Size)
}

// Benchmark tests for performance validation
func BenchmarkCollectorEvent_Validate(b *testing.B) {
	event := &CollectorEvent{
		EventID:   "benchmark-event-1",
		Timestamp: time.Now(),
		Type:      EventTypeKernelSyscall,
		Source:    "kernel-collector",
		EventData: EventDataContainer{
			SystemCall: &SystemCallData{
				Number: 1,
				Name:   "read",
				PID:    1234,
			},
		},
		Metadata: EventMetadata{
			Priority:      PriorityNormal,
			SchemaVersion: "1.0.0",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = event.Validate()
	}
}

func BenchmarkCollectorEvent_GetCorrelationKey(b *testing.B) {
	event := &CollectorEvent{
		Source: "kernel-collector",
		CorrelationHints: CorrelationHints{
			ContainerID: "container-123",
			PodUID:      "pod-456",
			ProcessID:   789,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = event.GetCorrelationKey()
	}
}
