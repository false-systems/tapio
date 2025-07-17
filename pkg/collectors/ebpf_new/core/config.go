package core

import (
	"time"
)

// DefaultConfig returns a default eBPF collector configuration
func DefaultConfig() Config {
	return Config{
		Name:               "ebpf-collector",
		Enabled:            true,
		EventBufferSize:    10000,
		RingBufferSize:     65536, // 64KB, power of 2
		BatchSize:          100,
		CollectionInterval: 100 * time.Millisecond,
		MaxEventsPerSecond: 10000,
		Timeout:            30 * time.Second,
		Programs:           []ProgramSpec{}, // Must be configured by user
		Filter:             Filter{},
	}
}

// SyscallMonitorConfig returns configuration for syscall monitoring
func SyscallMonitorConfig() Config {
	config := DefaultConfig()
	config.Name = "syscall-monitor"
	config.Programs = []ProgramSpec{
		{
			Name:         "syscall_enter",
			Type:         ProgramTypeTracepoint,
			AttachTarget: "raw_syscalls/sys_enter",
			Maps: []MapSpec{
				{
					Name:       "syscall_events",
					Type:       MapTypeRingBuf,
					KeySize:    0,
					ValueSize:  0,
					MaxEntries: 65536,
				},
			},
		},
		{
			Name:         "syscall_exit",
			Type:         ProgramTypeTracepoint,
			AttachTarget: "raw_syscalls/sys_exit",
		},
	}
	config.Filter = Filter{
		EventTypes:             []EventType{EventTypeSyscall},
		ExcludeSystemProcesses: true,
	}
	return config
}

// NetworkMonitorConfig returns configuration for network monitoring
func NetworkMonitorConfig() Config {
	config := DefaultConfig()
	config.Name = "network-monitor"
	config.Programs = []ProgramSpec{
		{
			Name:         "tcp_connect",
			Type:         ProgramTypeKprobe,
			AttachTarget: "tcp_v4_connect",
			Maps: []MapSpec{
				{
					Name:       "connection_events",
					Type:       MapTypeRingBuf,
					KeySize:    0,
					ValueSize:  0,
					MaxEntries: 32768,
				},
			},
		},
		{
			Name:         "tcp_accept",
			Type:         ProgramTypeKretprobe,
			AttachTarget: "inet_csk_accept",
		},
		{
			Name:         "udp_send",
			Type:         ProgramTypeKprobe,
			AttachTarget: "udp_sendmsg",
		},
	}
	config.Filter = Filter{
		EventTypes: []EventType{EventTypeNetworkIn, EventTypeNetworkOut},
	}
	return config
}

// ProcessMonitorConfig returns configuration for process monitoring
func ProcessMonitorConfig() Config {
	config := DefaultConfig()
	config.Name = "process-monitor"
	config.Programs = []ProgramSpec{
		{
			Name:         "exec_monitor",
			Type:         ProgramTypeTracepoint,
			AttachTarget: "sched/sched_process_exec",
			Maps: []MapSpec{
				{
					Name:       "exec_events",
					Type:       MapTypeRingBuf,
					KeySize:    0,
					ValueSize:  0,
					MaxEntries: 32768,
				},
			},
		},
		{
			Name:         "exit_monitor",
			Type:         ProgramTypeTracepoint,
			AttachTarget: "sched/sched_process_exit",
		},
		{
			Name:         "fork_monitor",
			Type:         ProgramTypeTracepoint,
			AttachTarget: "sched/sched_process_fork",
		},
	}
	config.Filter = Filter{
		EventTypes: []EventType{EventTypeProcessExec, EventTypeProcessExit},
	}
	return config
}

// MemoryMonitorConfig returns configuration for memory monitoring
func MemoryMonitorConfig() Config {
	config := DefaultConfig()
	config.Name = "memory-monitor"
	config.Programs = []ProgramSpec{
		{
			Name:         "malloc_monitor",
			Type:         ProgramTypeKprobe,
			AttachTarget: "__kmalloc",
			Maps: []MapSpec{
				{
					Name:       "alloc_events",
					Type:       MapTypeRingBuf,
					KeySize:    0,
					ValueSize:  0,
					MaxEntries: 65536,
				},
				{
					Name:       "alloc_stats",
					Type:       MapTypePerCPUHash,
					KeySize:    4,  // PID
					ValueSize:  16, // stats struct
					MaxEntries: 10000,
				},
			},
		},
		{
			Name:         "free_monitor",
			Type:         ProgramTypeKprobe,
			AttachTarget: "kfree",
		},
		{
			Name:         "oom_monitor",
			Type:         ProgramTypeKprobe,
			AttachTarget: "oom_kill_process",
		},
	}
	config.Filter = Filter{
		EventTypes: []EventType{EventTypeMemoryAlloc, EventTypeMemoryFree},
	}
	config.BatchSize = 500 // Higher batch size for high-frequency events
	config.CollectionInterval = 50 * time.Millisecond
	return config
}

// FileIOMonitorConfig returns configuration for file I/O monitoring
func FileIOMonitorConfig() Config {
	config := DefaultConfig()
	config.Name = "fileio-monitor"
	config.Programs = []ProgramSpec{
		{
			Name:         "open_monitor",
			Type:         ProgramTypeKprobe,
			AttachTarget: "do_sys_open",
			Maps: []MapSpec{
				{
					Name:       "file_events",
					Type:       MapTypeRingBuf,
					KeySize:    0,
					ValueSize:  0,
					MaxEntries: 32768,
				},
			},
		},
		{
			Name:         "read_monitor",
			Type:         ProgramTypeKprobe,
			AttachTarget: "vfs_read",
		},
		{
			Name:         "write_monitor",
			Type:         ProgramTypeKprobe,
			AttachTarget: "vfs_write",
		},
		{
			Name:         "close_monitor",
			Type:         ProgramTypeKretprobe,
			AttachTarget: "__fput",
		},
	}
	config.Filter = Filter{
		EventTypes: []EventType{EventTypeFileIO},
	}
	return config
}

// MinimalConfig returns a minimal configuration for testing
func MinimalConfig() Config {
	config := DefaultConfig()
	config.Name = "minimal-monitor"
	config.EventBufferSize = 1000
	config.RingBufferSize = 4096 // Minimum size
	config.Programs = []ProgramSpec{
		{
			Name:         "test_probe",
			Type:         ProgramTypeKprobe,
			AttachTarget: "sys_sync",
			Maps: []MapSpec{
				{
					Name:       "test_events",
					Type:       MapTypeRingBuf,
					KeySize:    0,
					ValueSize:  0,
					MaxEntries: 4096,
				},
			},
		},
	}
	return config
}