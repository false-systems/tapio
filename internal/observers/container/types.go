package container

import "fmt"

// Event types (must match C defines in container_monitor.c)
const (
	EventTypeOOMKill uint32 = 0
	EventTypeExit    uint32 = 1
)

// Exit categories (3 categories for MVP)
type ExitCategory string

const (
	ExitCategoryOOMKill ExitCategory = "oom_kill" // OOM killed by kernel
	ExitCategoryNormal  ExitCategory = "normal"   // Exit code 0 or SIGTERM
	ExitCategoryError   ExitCategory = "error"    // Non-zero exit or crash
)

// ExitClassification holds the result of classifying a container exit
type ExitClassification struct {
	Category ExitCategory
	ExitCode int32
	Signal   int32
	Evidence []string
}

// ContainerEventBPF matches the C struct layout from container_monitor.c
// Must be kept in sync with the C struct definition
type ContainerEventBPF struct {
	Type        uint32    // Event type (OOM or Exit)
	PID         uint32    // Process ID
	TID         uint32    // Thread ID
	ExitCode    int32     // Exit code
	Signal      int32     // Signal number
	CgroupPath  [256]byte // cgroup path (captured at event time)
	MemoryLimit uint64    // Memory limit from cgroup (captured in eBPF)
	MemoryUsage uint64    // Memory usage from cgroup (captured in eBPF)
	TimestampNs uint64    // Event timestamp (nanoseconds)
}

// ClassifyExit categorizes a container exit into one of 3 categories
// Priority: OOMKill > Normal > Error
func ClassifyExit(exitCode int32, signal int32, isOOMKilled bool) ExitClassification {
	var evidence []string

	// Priority 1: OOM (most specific)
	if isOOMKilled {
		evidence = append(evidence, "oom_kill event detected")
		return ExitClassification{
			Category: ExitCategoryOOMKill,
			ExitCode: exitCode,
			Signal:   signal,
			Evidence: evidence,
		}
	}

	// Priority 2: Normal exit (exit_code=0 or SIGTERM)
	if exitCode == 0 || signal == 15 {
		if exitCode == 0 {
			evidence = append(evidence, "exit_code=0")
		}
		if signal == 15 {
			evidence = append(evidence, "SIGTERM (clean shutdown)")
		}
		return ExitClassification{
			Category: ExitCategoryNormal,
			ExitCode: exitCode,
			Signal:   signal,
			Evidence: evidence,
		}
	}

	// Priority 3: Error (everything else)
	if exitCode != 0 {
		evidence = append(evidence, fmt.Sprintf("exit_code=%d", exitCode))
	}
	if signal != 0 {
		evidence = append(evidence, fmt.Sprintf("signal=%d", signal))
	}
	return ExitClassification{
		Category: ExitCategoryError,
		ExitCode: exitCode,
		Signal:   signal,
		Evidence: evidence,
	}
}
