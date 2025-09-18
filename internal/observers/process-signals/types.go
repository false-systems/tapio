package processsignals

import "fmt"

// runtimeEvent represents a runtime signal event from eBPF (must match C struct exactly)
type runtimeEvent struct {
	Timestamp  uint64
	PID        uint32
	TGID       uint32
	PPID       uint32
	EventType  uint32
	ExitCode   uint32
	Signal     uint32
	SenderPID  uint32
	CgroupID   uint64
	Comm       [16]byte
	ParentComm [16]byte
	// Union data - 24 bytes max (3 * uint64), interpreted based on event type
	UnionData [24]byte
}

// CollectorStats tracks collector metrics
type CollectorStats struct {
	EventsGenerated  uint64
	EventsDropped    uint64
	LastEventTime    uint64
	ProcessExecs     uint64
	ProcessExits     uint64
	SignalsDelivered uint64
	SignalsGenerated uint64
	OOMKills         uint64
}

// Event types for runtime signals
const (
	EventTypeProcessExec    uint32 = 1
	EventTypeProcessExit    uint32 = 2
	EventTypeSignalGenerate uint32 = 3
	EventTypeSignalDeliver  uint32 = 4
	EventTypeOOMKill        uint32 = 5
	EventTypeCoreDump       uint32 = 6
)

// Signal constants matching eBPF definitions
const (
	SIGTERM = 15
	SIGKILL = 9
	SIGINT  = 2
	SIGQUIT = 3
	SIGABRT = 6
	SIGSEGV = 11
	SIGBUS  = 7
	SIGFPE  = 8
	SIGILL  = 4
)

// SignalInfo contains decoded signal information
type SignalInfo struct {
	Number      int    `json:"signal_number"`
	Name        string `json:"signal_name"`
	Description string `json:"description"`
	IsFatal     bool   `json:"is_fatal"`
}

// ExitInfo contains decoded exit code information
type ExitInfo struct {
	Code        int    `json:"exit_code"`
	Signal      int    `json:"exit_signal"`
	CoreDumped  bool   `json:"core_dumped"`
	Description string `json:"description"`
}

// RuntimeSignalEvent represents a processed runtime signal event
type RuntimeSignalEvent struct {
	Timestamp  uint64      `json:"timestamp"`
	EventType  string      `json:"event_type"`
	PID        uint32      `json:"pid"`
	TGID       uint32      `json:"tgid"`
	PPID       uint32      `json:"ppid"`
	Command    string      `json:"command"`
	Filename   string      `json:"filename,omitempty"`
	UID        uint32      `json:"uid"`
	GID        uint32      `json:"gid"`
	ExitInfo   *ExitInfo   `json:"exit_info,omitempty"`
	SignalInfo *SignalInfo `json:"signal_info,omitempty"`
	SenderPID  uint32      `json:"sender_pid,omitempty"`
	IsOOMKill  bool        `json:"is_oom_kill,omitempty"`
}

// PodInfo contains Kubernetes pod metadata extracted from process context
type PodInfo struct {
	PodUID    string `json:"pod_uid"`
	PodName   string `json:"pod_name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Container string `json:"container,omitempty"`
}

// Helper functions for signal and exit code decoding

// GetSignalName returns the signal name for a given signal number
func GetSignalName(signum int) string {
	switch signum {
	case SIGTERM:
		return "SIGTERM"
	case SIGKILL:
		return "SIGKILL"
	case SIGINT:
		return "SIGINT"
	case SIGQUIT:
		return "SIGQUIT"
	case SIGABRT:
		return "SIGABRT"
	case SIGSEGV:
		return "SIGSEGV"
	case SIGBUS:
		return "SIGBUS"
	case SIGFPE:
		return "SIGFPE"
	case SIGILL:
		return "SIGILL"
	default:
		return "UNKNOWN"
	}
}

// GetSignalDescription returns a human-readable description of the signal
func GetSignalDescription(signum int) string {
	switch signum {
	case SIGTERM:
		return "Termination request"
	case SIGKILL:
		return "Kill (cannot be caught or ignored)"
	case SIGINT:
		return "Interrupt from keyboard (Ctrl+C)"
	case SIGQUIT:
		return "Quit from keyboard (Ctrl+\\)"
	case SIGABRT:
		return "Abort signal from abort(3)"
	case SIGSEGV:
		return "Segmentation fault (invalid memory reference)"
	case SIGBUS:
		return "Bus error (bad memory access)"
	case SIGFPE:
		return "Floating point exception"
	case SIGILL:
		return "Illegal instruction"
	default:
		return "Unknown signal"
	}
}

// IsSignalFatal returns whether a signal typically causes process termination
func IsSignalFatal(signum int) bool {
	switch signum {
	case SIGKILL, SIGTERM, SIGQUIT, SIGABRT, SIGSEGV, SIGBUS, SIGFPE, SIGILL:
		return true
	case SIGINT:
		return true // Usually fatal unless caught
	default:
		return false
	}
}

// DecodeExitCode decodes Linux exit code into meaningful information
func DecodeExitCode(exitCode uint32) *ExitInfo {
	info := &ExitInfo{
		Code: int(exitCode & 0xFF),
	}

	// Check if process was terminated by signal
	if exitCode&0x7F != 0 {
		signum := int(exitCode & 0x7F)
		info.Signal = signum
		info.CoreDumped = (exitCode & 0x80) != 0

		signalName := GetSignalName(signum)
		if info.CoreDumped {
			info.Description = fmt.Sprintf("Terminated by %s (core dumped)", signalName)
		} else {
			info.Description = fmt.Sprintf("Terminated by %s", signalName)
		}
	} else {
		// Normal exit
		code := int((exitCode >> 8) & 0xFF)
		info.Code = code
		if code == 0 {
			info.Description = "Successful exit"
		} else {
			info.Description = fmt.Sprintf("Exited with code %d", code)
		}
	}

	return info
}
