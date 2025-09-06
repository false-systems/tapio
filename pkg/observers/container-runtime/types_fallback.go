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

// ContainerMetadata is the Go representation of container metadata (fallback)
type ContainerMetadata struct {
	ContainerID string    `json:"container_id"`
	PodUID      string    `json:"pod_uid,omitempty"`
	PodName     string    `json:"pod_name,omitempty"`
	Namespace   string    `json:"namespace,omitempty"`
	MemoryLimit uint64    `json:"memory_limit"`
	CgroupID    uint64    `json:"cgroup_id"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
}
