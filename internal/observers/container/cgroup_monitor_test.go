//go:build linux

package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCgroupMonitor_NewWithDefaults tests default configuration
func TestCgroupMonitor_NewWithDefaults(t *testing.T) {
	monitor, err := NewCgroupMonitor(CgroupMonitorConfig{}, nil)
	require.NoError(t, err)
	require.NotNil(t, monitor)

	assert.Equal(t, "/sys/fs/cgroup", monitor.basePath)
	assert.Equal(t, 30*time.Second, monitor.cacheTTL)
	assert.NotNil(t, monitor.cache)
}

// TestCgroupMonitor_NewWithCustomConfig tests custom configuration
func TestCgroupMonitor_NewWithCustomConfig(t *testing.T) {
	cfg := CgroupMonitorConfig{
		BasePath:  "/custom/path",
		CacheSize: 500,
		CacheTTL:  15 * time.Second,
	}

	monitor, err := NewCgroupMonitor(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, monitor)

	assert.Equal(t, "/custom/path", monitor.basePath)
	assert.Equal(t, 15*time.Second, monitor.cacheTTL)
}

// TestCgroupMonitor_GetInfo_NotFound tests ErrCgroupNotFound for missing containers
func TestCgroupMonitor_GetInfo_NotFound(t *testing.T) {
	monitor, err := NewCgroupMonitor(CgroupMonitorConfig{
		BasePath: "/nonexistent/path",
	}, nil)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = monitor.GetInfo(ctx, "nonexistent-container")
	assert.ErrorIs(t, err, ErrCgroupNotFound)
}

// TestCgroupMonitor_FindCgroupPath_Empty tests empty container ID handling
func TestCgroupMonitor_FindCgroupPath_Empty(t *testing.T) {
	monitor, err := NewCgroupMonitor(CgroupMonitorConfig{}, nil)
	require.NoError(t, err)

	path := monitor.findCgroupPath("")
	assert.Empty(t, path)
}

// TestReadUint64File tests uint64 file reading
func TestReadUint64File(t *testing.T) {
	// Create temp file with uint64 content
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test")

	err := os.WriteFile(testFile, []byte("12345678\n"), 0644)
	require.NoError(t, err)

	value, err := readUint64File(testFile)
	require.NoError(t, err)
	assert.Equal(t, uint64(12345678), value)
}

// TestReadUint64File_NotExists tests missing file error
func TestReadUint64File_NotExists(t *testing.T) {
	_, err := readUint64File("/nonexistent/file")
	assert.Error(t, err)
}

// TestReadUint64OrMax_Value tests normal value reading
func TestReadUint64OrMax_Value(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "memory.max")

	err := os.WriteFile(testFile, []byte("536870912\n"), 0644)
	require.NoError(t, err)

	value, err := readUint64OrMax(testFile)
	require.NoError(t, err)
	assert.Equal(t, uint64(536870912), value)
}

// TestReadUint64OrMax_Max tests "max" handling (unlimited)
func TestReadUint64OrMax_Max(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "memory.max")

	err := os.WriteFile(testFile, []byte("max\n"), 0644)
	require.NoError(t, err)

	value, err := readUint64OrMax(testFile)
	require.NoError(t, err)
	assert.Equal(t, ^uint64(0), value) // Max uint64
}

// TestReadPSIFile tests PSI format parsing
func TestReadPSIFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "memory.pressure")

	content := `some avg10=1.50 avg60=2.30 avg300=3.40 total=123456
full avg10=0.50 avg60=0.80 avg300=1.00 total=654321
`
	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	psi, err := readPSIFile(testFile)
	require.NoError(t, err)

	assert.InDelta(t, 1.50, psi.SomeAvg10, 0.01)
	assert.InDelta(t, 2.30, psi.SomeAvg60, 0.01)
	assert.InDelta(t, 3.40, psi.SomeAvg300, 0.01)
	assert.Equal(t, uint64(123456), psi.SomeTotal)

	assert.InDelta(t, 0.50, psi.FullAvg10, 0.01)
	assert.InDelta(t, 0.80, psi.FullAvg60, 0.01)
	assert.InDelta(t, 1.00, psi.FullAvg300, 0.01)
	assert.Equal(t, uint64(654321), psi.FullTotal)
}

// TestReadCPUStat tests cpu.stat parsing
func TestReadCPUStat(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "cpu.stat")

	content := `usage_usec 1234567890
user_usec 1000000000
system_usec 234567890
nr_periods 1000
nr_throttled 50
throttled_usec 500000
`
	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	stat, err := readCPUStat(testFile)
	require.NoError(t, err)

	assert.Equal(t, uint64(1234567890), stat.UsageUsec)
	assert.Equal(t, uint64(1000000000), stat.UserUsec)
	assert.Equal(t, uint64(234567890), stat.SystemUsec)
	assert.Equal(t, uint64(1000), stat.NrPeriods)
	assert.Equal(t, uint64(50), stat.NrThrottled)
	assert.Equal(t, uint64(500000), stat.ThrottledUsec)
}

// TestErrorType tests error categorization for metrics
func TestErrorType(t *testing.T) {
	assert.Equal(t, "not_found", errorType(os.ErrNotExist))
	assert.Equal(t, "permission", errorType(os.ErrPermission))
	assert.Equal(t, "other", errorType(os.ErrInvalid))
}
