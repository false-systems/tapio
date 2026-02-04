package intelligence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/publisher"
)

func TestPolkuServiceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  PolkuConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: PolkuConfig{
				Publisher: publisher.Config{
					Address:   "polku:50051",
					ClusterID: "prod",
					NodeName:  "node-1",
				},
			},
			wantErr: false,
		},
		{
			name: "missing address",
			config: PolkuConfig{
				Publisher: publisher.Config{
					ClusterID: "prod",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPolkuService_Name(t *testing.T) {
	cfg := PolkuConfig{
		Publisher: publisher.Config{
			Address:   "polku:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
	}

	svc, err := NewPolkuService(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	assert.Equal(t, "intelligence-polku", svc.Name())
}

func TestPolkuService_IsCritical(t *testing.T) {
	cfg := PolkuConfig{
		Publisher: publisher.Config{
			Address:   "polku:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
		Critical: true,
	}

	svc, err := NewPolkuService(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	assert.True(t, svc.IsCritical())
}

func TestPolkuService_Emit_NilEvent(t *testing.T) {
	cfg := PolkuConfig{
		Publisher: publisher.Config{
			Address:   "polku:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
	}

	svc, err := NewPolkuService(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	err = svc.Emit(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil event")
}

func TestPolkuService_Emit_Closed(t *testing.T) {
	cfg := PolkuConfig{
		Publisher: publisher.Config{
			Address:   "polku:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
	}

	svc, err := NewPolkuService(cfg)
	require.NoError(t, err)

	// Close first
	require.NoError(t, svc.Close())

	// Emit after close should fail
	event := &domain.ObserverEvent{ID: "test", Type: "network"}
	err = svc.Emit(context.Background(), event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestPolkuService_Emit_ContextCancelled(t *testing.T) {
	cfg := PolkuConfig{
		Publisher: publisher.Config{
			Address:   "polku:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
	}

	svc, err := NewPolkuService(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	event := &domain.ObserverEvent{ID: "test", Type: "network"}
	err = svc.Emit(ctx, event)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPolkuService_IsConnected_NotConnected(t *testing.T) {
	cfg := PolkuConfig{
		Publisher: publisher.Config{
			Address:   "polku:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
	}

	svc, err := NewPolkuService(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	// Before Connect, should not be connected
	polkuSvc := svc.(*polkuService)
	assert.False(t, polkuSvc.IsConnected())
}

func TestConvertEventType_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // Expected protobuf enum name
	}{
		{"network", "network", "EBPF_TYPE_NETWORK"},
		{"container", "container", "EBPF_TYPE_CONTAINER"},
		{"kernel", "kernel", "EBPF_TYPE_MEMORY"},
		{"storage", "storage", "EBPF_TYPE_STORAGE"},
		{"unknown", "other", "EBPF_TYPE_UNSPECIFIED"},
		{"empty", "", "EBPF_TYPE_UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertEventType(tt.input)
			assert.Equal(t, tt.expected, result.String())
		})
	}
}

func TestConvertNetworkData_AllFields(t *testing.T) {
	data := &domain.NetworkEventData{
		Protocol:        "tcp",
		SrcIP:           "10.0.0.1",
		DstIP:           "10.0.0.2",
		SrcPort:         45678,
		DstPort:         80,
		BytesSent:       1024,
		BytesReceived:   2048,
		RTTCurrent:      15.5, // ms
		RetransmitCount: 3,
		DNSQuery:        "example.com",
	}

	result := convertNetworkData(data)

	assert.Equal(t, "tcp", result.Protocol)
	assert.Equal(t, "10.0.0.1", result.SrcIp)
	assert.Equal(t, "10.0.0.2", result.DstIp)
	assert.Equal(t, uint32(45678), result.SrcPort)
	assert.Equal(t, uint32(80), result.DstPort)
	assert.Equal(t, uint64(1024), result.BytesSent)
	assert.Equal(t, uint64(2048), result.BytesRecv)
	assert.Equal(t, uint64(15500), result.RttUs) // ms * 1000 = µs
	assert.Equal(t, uint64(3), result.Retransmits)
	assert.Equal(t, "example.com", result.DnsQuery)
}

func TestConvertContainerData_AllFields(t *testing.T) {
	data := &domain.ContainerEventData{
		ContainerID:   "abc123",
		ContainerName: "nginx",
		Image:         "nginx:1.21",
		State:         "Terminated",
		ExitCode:      137,
		Reason:        "OOMKilled",
		Signal:        9,
		MemoryLimit:   1073741824, // 1 GiB
		MemoryUsage:   1073741824,
		CgroupPath:    "/kubepods/pod123/abc123",
		PID:           12345,
	}

	result := convertContainerData(data)

	assert.Equal(t, "abc123", result.ContainerId)
	assert.Equal(t, "nginx", result.ContainerName)
	assert.Equal(t, "nginx:1.21", result.Image)
	assert.Equal(t, "Terminated", result.State)
	assert.Equal(t, int32(137), result.ExitCode)
	assert.Equal(t, "OOMKilled", result.ExitReason)
	assert.Equal(t, int32(9), result.Signal)
	assert.Equal(t, uint64(1073741824), result.MemoryLimit)
	assert.Equal(t, uint64(1073741824), result.MemoryUsage)
	assert.Equal(t, "/kubepods/pod123/abc123", result.CgroupPath)
	assert.Equal(t, uint32(12345), result.Pid)
}

func TestConvertMemoryData_AllFields(t *testing.T) {
	data := &domain.KernelEventData{
		OOMMemoryUsage: 1073741824, // 1 GiB
		OOMMemoryLimit: 2147483648, // 2 GiB
	}

	result := convertMemoryData(data)

	assert.Equal(t, uint64(1073741824), result.UsageBytes)
	assert.Equal(t, uint64(2147483648), result.LimitBytes)
}

func TestConvertStorageData_AllFields(t *testing.T) {
	tests := []struct {
		name           string
		data           *domain.StorageEventData
		expectedDevice string
	}{
		{
			name: "with device name",
			data: &domain.StorageEventData{
				DeviceMajor:   8,
				DeviceMinor:   0,
				DeviceName:    "sda",
				OperationType: "write",
				Bytes:         4096,
				LatencyMs:     1.5,
				ErrorName:     "",
			},
			expectedDevice: "sda",
		},
		{
			name: "without device name (uses major:minor)",
			data: &domain.StorageEventData{
				DeviceMajor:   259,
				DeviceMinor:   0,
				DeviceName:    "",
				OperationType: "read",
				Bytes:         8192,
				LatencyMs:     0.5,
				ErrorName:     "EIO",
			},
			expectedDevice: "259:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertStorageData(tt.data)

			assert.Equal(t, tt.expectedDevice, result.Device)
			assert.Equal(t, tt.data.OperationType, result.Operation)
			assert.Equal(t, tt.data.Bytes, result.Bytes)
			assert.Equal(t, uint64(tt.data.LatencyMs*1000), result.LatencyUs) // ms → µs
			assert.Equal(t, tt.data.ErrorName, result.ErrorCode)
		})
	}
}

func TestConvertToRawEvent_NetworkData(t *testing.T) {
	event := &domain.ObserverEvent{
		ID:        "test-123",
		Type:      "network",
		Subtype:   "connection_reset",
		Source:    "network-observer",
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			Protocol:  "tcp",
			SrcIP:     "10.0.0.1",
			DstIP:     "10.0.0.2",
			SrcPort:   45678,
			DstPort:   80,
			Namespace: "default",
			PodName:   "nginx-pod",
		},
	}

	raw := convertToRawEvent(event, "test-cluster", "node-1")

	require.NotNil(t, raw)
	assert.Equal(t, "test-123", raw.Id)
	assert.Equal(t, "connection_reset", raw.Subtype)
	assert.Equal(t, "test-cluster", raw.ClusterId)
	assert.Equal(t, "node-1", raw.NodeName)
	assert.Equal(t, "default", raw.Namespace)
	assert.Equal(t, "nginx-pod", raw.PodName)

	// Check network data
	require.NotNil(t, raw.GetNetwork())
	assert.Equal(t, "tcp", raw.GetNetwork().Protocol)
	assert.Equal(t, "10.0.0.1", raw.GetNetwork().SrcIp)
	assert.Equal(t, "10.0.0.2", raw.GetNetwork().DstIp)
}

func TestConvertToRawEvent_ContainerData(t *testing.T) {
	event := &domain.ObserverEvent{
		ID:        "test-456",
		Type:      "container",
		Subtype:   "oom_kill",
		Timestamp: time.Now(),
		ContainerData: &domain.ContainerEventData{
			ContainerID:   "abc123",
			ContainerName: "nginx",
			PodNamespace:  "default",
			PodName:       "nginx-pod",
			ExitCode:      137,
		},
	}

	raw := convertToRawEvent(event, "test-cluster", "node-1")

	require.NotNil(t, raw)
	assert.Equal(t, "test-456", raw.Id)
	assert.Equal(t, "default", raw.Namespace)
	assert.Equal(t, "nginx-pod", raw.PodName)
	assert.Equal(t, "abc123", raw.ContainerId)
	assert.Equal(t, "nginx", raw.ContainerName)

	// Check container data
	require.NotNil(t, raw.GetContainer())
	assert.Equal(t, "abc123", raw.GetContainer().ContainerId)
	assert.Equal(t, int32(137), raw.GetContainer().ExitCode)
}

func TestConvertToRawEvent_KernelData(t *testing.T) {
	event := &domain.ObserverEvent{
		ID:        "test-789",
		Type:      "kernel",
		Subtype:   "oom",
		Timestamp: time.Now(),
		KernelData: &domain.KernelEventData{
			OOMMemoryUsage: 1073741824,
			OOMMemoryLimit: 2147483648,
		},
	}

	raw := convertToRawEvent(event, "test-cluster", "node-1")

	require.NotNil(t, raw)
	assert.Equal(t, "test-789", raw.Id)

	// Check memory data
	require.NotNil(t, raw.GetMemory())
	assert.Equal(t, uint64(1073741824), raw.GetMemory().UsageBytes)
	assert.Equal(t, uint64(2147483648), raw.GetMemory().LimitBytes)
}

func TestConvertToRawEvent_StorageData(t *testing.T) {
	event := &domain.ObserverEvent{
		ID:        "test-storage",
		Type:      "storage",
		Subtype:   "io_error",
		Timestamp: time.Now(),
		StorageData: &domain.StorageEventData{
			DeviceName:    "sda",
			OperationType: "write",
			Bytes:         4096,
			LatencyMs:     1.5,
			ErrorName:     "EIO",
			Namespace:     "default",
			PodName:       "db-pod",
			ContainerID:   "xyz789",
		},
	}

	raw := convertToRawEvent(event, "test-cluster", "node-1")

	require.NotNil(t, raw)
	assert.Equal(t, "test-storage", raw.Id)
	assert.Equal(t, "default", raw.Namespace)
	assert.Equal(t, "db-pod", raw.PodName)
	assert.Equal(t, "xyz789", raw.ContainerId)

	// Check storage data
	require.NotNil(t, raw.GetStorage())
	assert.Equal(t, "sda", raw.GetStorage().Device)
	assert.Equal(t, "write", raw.GetStorage().Operation)
	assert.Equal(t, "EIO", raw.GetStorage().ErrorCode)
}

func TestConvertToRawEvent_NoData(t *testing.T) {
	event := &domain.ObserverEvent{
		ID:        "test-empty",
		Type:      "unknown",
		Timestamp: time.Now(),
	}

	raw := convertToRawEvent(event, "test-cluster", "node-1")

	require.NotNil(t, raw)
	assert.Equal(t, "test-empty", raw.Id)
	assert.Nil(t, raw.GetNetwork())
	assert.Nil(t, raw.GetContainer())
	assert.Nil(t, raw.GetMemory())
	assert.Nil(t, raw.GetStorage())
}
