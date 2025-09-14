# Health Observer

The Health Observer monitors critical system health indicators that signal resource exhaustion or system issues.

## What It Monitors

The observer tracks system call failures that indicate:

- **Disk Space Exhaustion** (`ENOSPC`)
- **Memory Exhaustion** (`ENOMEM`) 
- **File Descriptor Exhaustion** (`EMFILE`)
- **Disk Quota Exceeded** (`EDQUOT`)
- **Connection Failures** (`ECONNREFUSED`)
- **I/O Errors** (`EIO`)
- **Permission Issues** (`EACCES`, `EPERM`)

## Architecture

```
┌─────────────────────────────────────────┐
│         System Calls                    │
│    write(), open(), connect()...        │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│       eBPF Tracepoint                   │
│    (raw_syscalls/sys_exit)             │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│       Health Observer                   │
│   - Filters critical errors             │
│   - Categorizes by severity             │
│   - Rate limits events                  │
└────────────┬────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────┐
│       Health Events                     │
│   EventType: "health_issue"             │
│   Severity: Critical/Error/Warning      │
└─────────────────────────────────────────┘
```

## Metrics

- `health_events_processed_total` - Total health events processed
- `health_disk_space_errors_total` - Disk space exhaustion errors
- `health_memory_errors_total` - Memory exhaustion errors  
- `health_connection_refused_errors_total` - Connection refused errors
- `health_file_descriptor_errors_total` - File descriptor exhaustion
- `health_disk_quota_errors_total` - Disk quota exceeded errors

## Configuration

```go
config := &Config{
    RingBufferSize:   8 * 1024 * 1024, // 8MB
    EventChannelSize: 10000,
    RateLimitMs:      100,
    EnabledCategories: map[string]bool{
        "file":    true,
        "network": true,
        "memory":  true,
    },
}
```

## Usage

```go
observer, err := health.NewObserver(logger, config)
if err != nil {
    return err
}

// Start monitoring
if err := observer.Start(ctx); err != nil {
    return err
}

// Process events
for event := range observer.Events() {
    // Handle health events
    switch event.EventData.Kernel.ErrorMessage {
    case "Disk space exhausted":
        // Alert on disk space issues
    case "Memory exhaustion":
        // Alert on OOM conditions
    }
}
```

## Platform Support

- **Linux**: Full eBPF-based monitoring
- **Non-Linux**: Mock event generation for testing

## Why "Health" Instead of "Syscall Errors"?

This observer isn't just tracking system calls - it's monitoring the overall health of the system by detecting critical resource exhaustion and failure patterns. The name "health" better reflects its purpose as a system health monitor.