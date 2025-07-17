package core

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid_config",
			config: Config{
				Name:               "test-collector",
				Enabled:            true,
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				MaxEventsPerSecond: 1000,
				Timeout:            30 * time.Second,
				Programs: []ProgramSpec{
					{
						Name:         "test_prog",
						Type:         ProgramTypeKprobe,
						AttachTarget: "sys_open",
						Code:         []byte{0x01},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty_name",
			config: Config{
				Name:               "",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "invalid_event_buffer_size_too_small",
			config: Config{
				Name:               "test",
				EventBufferSize:    50,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be between 100 and 100000",
		},
		{
			name: "invalid_event_buffer_size_too_large",
			config: Config{
				Name:               "test",
				EventBufferSize:    200000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be between 100 and 100000",
		},
		{
			name: "no_programs",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{},
			},
			wantErr: true,
			errMsg:  "at least one program must be specified",
		},
		{
			name: "invalid_ring_buffer_size_not_power_of_2",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     5000, // Not power of 2
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be a power of 2",
		},
		{
			name: "invalid_ring_buffer_size_too_small",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     2048, // Less than 4096
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be a power of 2 and at least 4096",
		},
		{
			name: "invalid_batch_size_too_small",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          0,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be between 1 and 1000",
		},
		{
			name: "invalid_batch_size_too_large",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          2000,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be between 1 and 1000",
		},
		{
			name: "invalid_collection_interval_too_small",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 5 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be between 10ms and 1m",
		},
		{
			name: "invalid_collection_interval_too_large",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 2 * time.Minute,
				Timeout:            30 * time.Second,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be between 10ms and 1m",
		},
		{
			name: "invalid_timeout_too_small",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            500 * time.Millisecond,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be between 1s and 5m",
		},
		{
			name: "invalid_timeout_too_large",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            10 * time.Minute,
				Programs:           []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}},
			},
			wantErr: true,
			errMsg:  "must be between 1s and 5m",
		},
		{
			name: "invalid_program_spec",
			config: Config{
				Name:               "test",
				EventBufferSize:    1000,
				RingBufferSize:     4096,
				BatchSize:          10,
				CollectionInterval: 100 * time.Millisecond,
				Timeout:            30 * time.Second,
				Programs: []ProgramSpec{
					{
						Name:         "",
						Type:         ProgramTypeKprobe,
						AttachTarget: "sys_open",
						Code:         []byte{0x01},
					},
				},
			},
			wantErr: true,
			errMsg:  "program name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if err.Error() == "" || !containsString(err.Error(), tt.errMsg) {
					t.Errorf("Config.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestProgramSpecValidate(t *testing.T) {
	tests := []struct {
		name    string
		spec    ProgramSpec
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid_with_code",
			spec: ProgramSpec{
				Name:         "test_prog",
				Type:         ProgramTypeKprobe,
				AttachTarget: "sys_open",
				Code:         []byte{0x01, 0x02},
			},
			wantErr: false,
		},
		{
			name: "valid_with_code_path",
			spec: ProgramSpec{
				Name:         "test_prog",
				Type:         ProgramTypeKprobe,
				AttachTarget: "sys_open",
				CodePath:     "/path/to/prog.o",
			},
			wantErr: false,
		},
		{
			name: "empty_name",
			spec: ProgramSpec{
				Name:         "",
				Type:         ProgramTypeKprobe,
				AttachTarget: "sys_open",
				Code:         []byte{0x01},
			},
			wantErr: true,
			errMsg:  "program name is required",
		},
		{
			name: "empty_type",
			spec: ProgramSpec{
				Name:         "test",
				Type:         "",
				AttachTarget: "sys_open",
				Code:         []byte{0x01},
			},
			wantErr: true,
			errMsg:  "program type is required",
		},
		{
			name: "empty_attach_target",
			spec: ProgramSpec{
				Name:         "test",
				Type:         ProgramTypeKprobe,
				AttachTarget: "",
				Code:         []byte{0x01},
			},
			wantErr: true,
			errMsg:  "attach target is required",
		},
		{
			name: "no_code_or_path",
			spec: ProgramSpec{
				Name:         "test",
				Type:         ProgramTypeKprobe,
				AttachTarget: "sys_open",
			},
			wantErr: true,
			errMsg:  "either code or code_path must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ProgramSpec.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if err.Error() == "" || !containsString(err.Error(), tt.errMsg) {
					t.Errorf("ProgramSpec.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Name != "ebpf-collector" {
		t.Errorf("DefaultConfig() Name = %v, want %v", config.Name, "ebpf-collector")
	}

	if !config.Enabled {
		t.Errorf("DefaultConfig() Enabled = %v, want %v", config.Enabled, true)
	}

	if config.EventBufferSize != 10000 {
		t.Errorf("DefaultConfig() EventBufferSize = %v, want %v", config.EventBufferSize, 10000)
	}

	if config.RingBufferSize != 65536 {
		t.Errorf("DefaultConfig() RingBufferSize = %v, want %v", config.RingBufferSize, 65536)
	}

	// Validate the default config
	config.Programs = []ProgramSpec{{Name: "test", Type: ProgramTypeKprobe, AttachTarget: "test", Code: []byte{1}}}
	if err := config.Validate(); err != nil {
		t.Errorf("DefaultConfig() validation failed: %v", err)
	}
}

func TestPredefinedConfigs(t *testing.T) {
	configs := []struct {
		name   string
		config Config
	}{
		{"SyscallMonitor", SyscallMonitorConfig()},
		{"NetworkMonitor", NetworkMonitorConfig()},
		{"ProcessMonitor", ProcessMonitorConfig()},
		{"MemoryMonitor", MemoryMonitorConfig()},
		{"FileIOMonitor", FileIOMonitorConfig()},
		{"Minimal", MinimalConfig()},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.config.Validate(); err != nil {
				t.Errorf("%s config validation failed: %v", tc.name, err)
			}

			if len(tc.config.Programs) == 0 {
				t.Errorf("%s config has no programs", tc.name)
			}

			for i, prog := range tc.config.Programs {
				if err := prog.Validate(); err != nil {
					t.Errorf("%s config program[%d] validation failed: %v", tc.name, i, err)
				}
			}
		})
	}
}

func TestHealthStatus(t *testing.T) {
	// Test health status values
	statuses := []HealthStatus{
		HealthStatusHealthy,
		HealthStatusDegraded,
		HealthStatusUnhealthy,
	}

	for _, status := range statuses {
		if status == "" {
			t.Errorf("HealthStatus should not be empty")
		}
	}
}

func TestProgramTypes(t *testing.T) {
	// Test all program types are defined
	types := []ProgramType{
		ProgramTypeKprobe,
		ProgramTypeKretprobe,
		ProgramTypeTracepoint,
		ProgramTypeRawTracepoint,
		ProgramTypeXDP,
		ProgramTypeTC,
		ProgramTypePerfEvent,
	}

	for _, pt := range types {
		if pt == "" {
			t.Errorf("ProgramType should not be empty")
		}
	}
}

func TestMapTypes(t *testing.T) {
	// Test all map types are defined
	types := []MapType{
		MapTypeHash,
		MapTypeArray,
		MapTypeProgArray,
		MapTypePerfEventArray,
		MapTypePerCPUHash,
		MapTypePerCPUArray,
		MapTypeStackTrace,
		MapTypeCgroupArray,
		MapTypeLRUHash,
		MapTypeLRUPerCPUHash,
		MapTypeLPMTrie,
		MapTypeArrayOfMaps,
		MapTypeHashOfMaps,
		MapTypeRingBuf,
	}

	for _, mt := range types {
		if mt == "" {
			t.Errorf("MapType should not be empty")
		}
	}
}

func TestEventTypes(t *testing.T) {
	// Test all event types are defined
	types := []EventType{
		EventTypeSyscall,
		EventTypeNetworkIn,
		EventTypeNetworkOut,
		EventTypeFileIO,
		EventTypeProcessExec,
		EventTypeProcessExit,
		EventTypeMemoryAlloc,
		EventTypeMemoryFree,
		EventTypeScheduler,
		EventTypeCustom,
	}

	for _, et := range types {
		if et == "" {
			t.Errorf("EventType should not be empty")
		}
	}
}

func TestFilter(t *testing.T) {
	filter := Filter{
		EventTypes:             []EventType{EventTypeSyscall, EventTypeNetworkIn},
		ProcessIDs:             []uint32{1234, 5678},
		ContainerIDs:           []string{"container1", "container2"},
		Namespaces:             []string{"default", "kube-system"},
		MinSeverity:            domain.SeverityWarn,
		ExcludeSystemProcesses: true,
	}

	// Verify filter fields
	if len(filter.EventTypes) != 2 {
		t.Errorf("Filter.EventTypes length = %v, want %v", len(filter.EventTypes), 2)
	}

	if len(filter.ProcessIDs) != 2 {
		t.Errorf("Filter.ProcessIDs length = %v, want %v", len(filter.ProcessIDs), 2)
	}

	if !filter.ExcludeSystemProcesses {
		t.Errorf("Filter.ExcludeSystemProcesses = %v, want %v", filter.ExcludeSystemProcesses, true)
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || len(substr) > 0 && len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}