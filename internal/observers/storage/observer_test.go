//go:build linux

package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestCreateDomainEvent_LatencySpike(t *testing.T) {
	obs := &StorageObserver{name: "storage-test"}

	evt := StorageEventBPF{
		TimestampNs: 1000000000,
		LatencyNs:   100_000_000, // 100ms
		CgroupID:    12345,
		Sector:      1024,
		DevMajor:    8,
		DevMinor:    1,
		Bytes:       4096,
		PID:         1234,
		ErrorCode:   0,
		Opcode:      OpRead,
		Severity:    SeverityWarning,
		Comm:        [16]byte{'d', 'd', 0},
	}

	result := obs.createDomainEvent(evt)

	assert.Equal(t, string(domain.EventTypeStorage), result.Type)
	assert.Equal(t, "storage-test", result.Source)
	require.NotNil(t, result.StorageData)

	data := result.StorageData
	assert.Equal(t, uint32(8), data.DeviceMajor)
	assert.Equal(t, uint32(1), data.DeviceMinor)
	assert.Equal(t, "read", data.OperationType)
	assert.Equal(t, uint64(4096), data.Bytes)
	assert.InDelta(t, 100.0, data.LatencyMs, 0.1)
	assert.Equal(t, uint64(1024), data.Sector)
	assert.Equal(t, uint64(12345), data.CgroupID)
	assert.Equal(t, "dd", data.ProcessName)
	assert.Equal(t, uint32(1234), data.PID)
	assert.Equal(t, uint16(0), data.ErrorCode)
}

func TestCreateDomainEvent_IOError(t *testing.T) {
	obs := &StorageObserver{name: "storage-test"}

	evt := StorageEventBPF{
		TimestampNs: 1000000000,
		LatencyNs:   5_000_000, // 5ms
		CgroupID:    12345,
		Sector:      2048,
		DevMajor:    8,
		DevMinor:    2,
		Bytes:       512,
		PID:         5678,
		ErrorCode:   5, // EIO
		Opcode:      OpWrite,
		Severity:    SeverityCritical,
		Comm:        [16]byte{'f', 's', 'y', 'n', 'c', 0},
	}

	result := obs.createDomainEvent(evt)

	require.NotNil(t, result.StorageData)
	data := result.StorageData

	assert.Equal(t, "write", data.OperationType)
	assert.Equal(t, uint16(5), data.ErrorCode)
	assert.Equal(t, "EIO", data.ErrorName)
	assert.Equal(t, "fsync", data.ProcessName)
}

func TestCreateDomainEvent_WriteOperation(t *testing.T) {
	obs := &StorageObserver{name: "storage-test"}

	evt := StorageEventBPF{
		Opcode: OpWrite,
	}

	result := obs.createDomainEvent(evt)
	assert.Equal(t, "write", result.StorageData.OperationType)
}
