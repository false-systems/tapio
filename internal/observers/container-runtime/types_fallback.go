//go:build !linux
// +build !linux

package containerruntime

import "time"

// Config holds Container Runtime observer configuration (fallback for non-Linux)
type Config struct {
	Name                 string        `json:"name" yaml:"name"`
	BufferSize           int           `json:"buffer_size" yaml:"buffer_size"`
	EnableOOMKill        bool          `json:"enable_oom_kill" yaml:"enable_oom_kill"`
	EnableMemoryPressure bool          `json:"enable_memory_pressure" yaml:"enable_memory_pressure"`
	EnableProcessExit    bool          `json:"enable_process_exit" yaml:"enable_process_exit"`
	EnableProcessFork    bool          `json:"enable_process_fork" yaml:"enable_process_fork"`
	BPFProgramPinPath    string        `json:"bpf_program_pin_path" yaml:"bpf_program_pin_path"`
	BPFLogLevel          int           `json:"bpf_log_level" yaml:"bpf_log_level"`
	MetricsInterval      time.Duration `json:"metrics_interval" yaml:"metrics_interval"`
	MetadataCacheSize    int           `json:"metadata_cache_size" yaml:"metadata_cache_size"`
	MetadataCacheTTL     time.Duration `json:"metadata_cache_ttl" yaml:"metadata_cache_ttl"`
	RingBufferSize       int           `json:"ring_buffer_size" yaml:"ring_buffer_size"`
	WakeupEvents         int           `json:"wakeup_events" yaml:"wakeup_events"`
	EnableSymbolCache    bool          `json:"enable_symbol_cache" yaml:"enable_symbol_cache"`
}

// BPFContainerExitEvent is a mock struct for non-Linux systems
// This is only used for tests and benchmarks on macOS/Windows
type BPFContainerExitEvent struct {
	Timestamp   uint64   // ns since boot (8 bytes)
	PID         uint32   // Process ID (4 bytes)
	TGID        uint32   // Thread Group ID (4 bytes)
	ExitCode    int32    // Exit code (4 bytes)
	CgroupID    uint64   // Cgroup ID (8 bytes)
	MemoryUsage uint64   // Memory usage in bytes (8 bytes)
	MemoryLimit uint64   // Memory limit in bytes (8 bytes)
	OOMKilled   uint8    // 1 if OOM killed, 0 otherwise (1 byte)
	Comm        [16]byte // Process command name (16 bytes)
	ContainerID [64]byte // Container ID string (64 bytes)
}

// ContainerMetadata is the Go representation of container metadata (fallback)
type ContainerMetadata struct {
	ContainerID   string            `json:"container_id"`
	ContainerName string            `json:"container_name,omitempty"`
	ImageName     string            `json:"image_name,omitempty"`
	PodUID        string            `json:"pod_uid,omitempty"`
	PodName       string            `json:"pod_name,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	Runtime       string            `json:"runtime,omitempty"`
	MemoryLimit   uint64            `json:"memory_limit"`
	CgroupID      uint64            `json:"cgroup_id"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	LastSeen      time.Time         `json:"last_seen"`
}
