package ebpf

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RawEventFormatter formats eBPF events in a human-readable style
type RawEventFormatter struct {
	includeTimestamp bool
	includeMetadata  bool
	colorOutput      bool
	verboseMode      bool
}

// RawEventFormatterOptions contains formatting options
type RawEventFormatterOptions struct {
	IncludeTimestamp bool `json:"include_timestamp"`
	IncludeMetadata  bool `json:"include_metadata"`
	ColorOutput      bool `json:"color_output"`
	VerboseMode      bool `json:"verbose_mode"`
}

// NewRawEventFormatter creates a new raw event formatter
func NewRawEventFormatter(opts *RawEventFormatterOptions) *RawEventFormatter {
	if opts == nil {
		opts = &RawEventFormatterOptions{
			IncludeTimestamp: true,
			IncludeMetadata:  false,
			ColorOutput:      true,
			VerboseMode:      false,
		}
	}

	return &RawEventFormatter{
		includeTimestamp: opts.IncludeTimestamp,
		includeMetadata:  opts.IncludeMetadata,
		colorOutput:      opts.ColorOutput,
		verboseMode:      opts.VerboseMode,
	}
}

// FormatEvent formats a raw eBPF event for human readability
func (f *RawEventFormatter) FormatEvent(event *RawEvent) string {
	var parts []string

	// Add timestamp
	if f.includeTimestamp {
		timestamp := time.Unix(0, int64(event.Timestamp))
		parts = append(parts, f.colorize(timestamp.Format("15:04:05.000"), colorGray))
	}

	// Add event type indicator
	typeIndicator := f.getEventTypeIndicator(event.Type)
	parts = append(parts, f.colorize(typeIndicator, f.getEventTypeColor(event.Type)))

	// Add process info
	processInfo := fmt.Sprintf("%s[%d]", event.Comm, event.PID)
	if event.UID == 0 {
		processInfo = f.colorize(processInfo, colorRed) // Root processes in red
	} else {
		processInfo = f.colorize(processInfo, colorBlue)
	}
	parts = append(parts, processInfo)

	// Add event-specific information
	eventDesc := f.formatEventDescription(event)
	parts = append(parts, eventDesc)

	// Add metadata if requested
	if f.includeMetadata {
		metadata := f.formatMetadata(event)
		if metadata != "" {
			parts = append(parts, f.colorize(metadata, colorGray))
		}
	}

	return strings.Join(parts, " ")
}

// FormatEventJSON formats an event as JSON for programmatic consumption
func (f *RawEventFormatter) FormatEventJSON(event *RawEvent) (string, error) {
	formattedEvent := f.convertToStructuredEvent(event)
	data, err := json.MarshalIndent(formattedEvent, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FormatEventCompact formats an event in compact style
func (f *RawEventFormatter) FormatEventCompact(event *RawEvent) string {
	timestamp := time.Unix(0, int64(event.Timestamp))
	return fmt.Sprintf("%s %s %s[%d] %s",
		timestamp.Format("15:04:05"),
		f.getEventTypeIndicator(event.Type),
		event.Comm,
		event.PID,
		f.formatEventDescription(event))
}

// getEventTypeIndicator returns a visual indicator for the event type
func (f *RawEventFormatter) getEventTypeIndicator(eventType EventType) string {
	switch eventType {
	case EventTypeNetwork:
		return "🌐" // or "NET" for non-emoji terminals
	case EventTypeProcess:
		return "⚙️" // or "PROC"
	case EventTypeFile:
		return "📁" // or "FILE"
	case EventTypeSyscall:
		return "🔧" // or "SYS"
	case EventTypeSecurity:
		return "🛡️" // or "SEC"
	case EventTypeContainer:
		return "📦" // or "CTR"
	case EventTypeMemory:
		return "💾" // or "MEM"
	case EventTypeCPU:
		return "🖥️" // or "CPU"
	default:
		return "❓" // or "UNK"
	}
}

// formatEventDescription formats the main event description
func (f *RawEventFormatter) formatEventDescription(event *RawEvent) string {
	switch event.Type {
	case EventTypeNetwork:
		return f.formatNetworkEvent(event)
	case EventTypeProcess:
		return f.formatProcessEvent(event)
	case EventTypeFile:
		return f.formatFileEvent(event)
	case EventTypeSyscall:
		return f.formatSyscallEvent(event)
	case EventTypeSecurity:
		return f.formatSecurityEvent(event)
	case EventTypeContainer:
		return f.formatContainerEvent(event)
	default:
		return f.formatGenericEvent(event)
	}
}

// formatNetworkEvent formats network events for structured output
func (f *RawEventFormatter) formatNetworkEvent(event *RawEvent) string {
	if net, ok := event.Details.(*NetworkEvent); ok {
		direction := "→"
		if net.Direction == "ingress" {
			direction = "←"
		}

		proto := f.getProtocolName(net.Protocol)

		var desc string
		if f.verboseMode {
			desc = fmt.Sprintf("%s %s:%d %s %s:%d (%s) [%s]",
				proto,
				net.SourceIP, net.SourcePort,
				direction,
				net.DestIP, net.DestPort,
				net.SubType.String(),
				f.formatBytes(net.Size))
		} else {
			desc = fmt.Sprintf("%s %s:%d %s %s:%d",
				proto,
				net.SourceIP, net.SourcePort,
				direction,
				net.DestIP, net.DestPort)
		}

		// Add L7 details if available
		if net.L7Protocol != "" {
			l7Info := f.formatL7Details(net)
			if l7Info != "" {
				desc += " " + f.colorize(l7Info, colorCyan)
			}
		}

		return desc
	}
	return "network event"
}

// formatProcessEvent formats process events
func (f *RawEventFormatter) formatProcessEvent(event *RawEvent) string {
	if proc, ok := event.Details.(*ProcessEvent); ok {
		switch proc.SubType {
		case ProcessEventExec:
			if len(proc.Args) > 0 {
				return fmt.Sprintf("exec %s", f.colorize(strings.Join(proc.Args, " "), colorGreen))
			}
			return "exec"
		case ProcessEventExit:
			exitCode := ""
			if proc.ExitCode != 0 {
				exitCode = fmt.Sprintf(" (exit %d)", proc.ExitCode)
				exitCode = f.colorize(exitCode, colorRed)
			}
			return "exit" + exitCode
		case ProcessEventFork:
			return fmt.Sprintf("fork → PID %d", event.PID)
		default:
			return fmt.Sprintf("process %s", proc.SubType.String())
		}
	}
	return "process event"
}

// formatFileEvent formats file events
func (f *RawEventFormatter) formatFileEvent(event *RawEvent) string {
	if file, ok := event.Details.(*FileEvent); ok {
		action := file.SubType.String()
		path := f.colorize(file.Path, colorYellow)

		switch file.SubType {
		case FileEventRead, FileEventWrite:
			if file.ReadSize > 0 {
				return fmt.Sprintf("%s %s [%s]", action, path, f.formatBytes(uint32(file.ReadSize)))
			}
		case FileEventRename:
			if file.NewPath != "" {
				return fmt.Sprintf("%s %s → %s", action, path, f.colorize(file.NewPath, colorYellow))
			}
		case FileEventChmod:
			if file.NewMode > 0 {
				return fmt.Sprintf("%s %s (mode %o)", action, path, file.NewMode)
			}
		}

		return fmt.Sprintf("%s %s", action, path)
	}
	return "file event"
}

// formatSyscallEvent formats syscall events
func (f *RawEventFormatter) formatSyscallEvent(event *RawEvent) string {
	if sys, ok := event.Details.(*SyscallEvent); ok {
		syscallName := f.colorize(sys.Name, colorMagenta)

		if f.verboseMode {
			args := make([]string, len(sys.Args))
			for i, arg := range sys.Args {
				args[i] = fmt.Sprintf("0x%x", arg)
			}

			if sys.Entry {
				return fmt.Sprintf("%s(%s)", syscallName, strings.Join(args, ", "))
			} else {
				retStr := fmt.Sprintf("%d", sys.Return)
				if sys.Error != 0 {
					retStr = f.colorize(fmt.Sprintf("-%d (errno)", sys.Error), colorRed)
				}
				return fmt.Sprintf("%s = %s", syscallName, retStr)
			}
		} else {
			direction := "→"
			if !sys.Entry {
				direction = "←"
			}
			return fmt.Sprintf("%s %s", direction, syscallName)
		}
	}
	return "syscall"
}

// formatSecurityEvent formats security events
func (f *RawEventFormatter) formatSecurityEvent(event *RawEvent) string {
	if sec, ok := event.Details.(*SecurityEvent); ok {
		action := sec.Action
		subject := sec.Subject
		object := f.colorize(sec.Object, colorYellow)
		result := sec.Result

		if result == "denied" {
			result = f.colorize(result, colorRed)
		} else {
			result = f.colorize(result, colorGreen)
		}

		return fmt.Sprintf("%s %s %s %s → %s", sec.Type, action, subject, object, result)
	}
	return "security event"
}

// formatContainerEvent formats container events
func (f *RawEventFormatter) formatContainerEvent(event *RawEvent) string {
	if ctr, ok := event.Details.(*ContainerEvent); ok {
		action := ctr.Action
		name := f.colorize(ctr.ContainerName, colorCyan)

		if ctr.Image != "" {
			return fmt.Sprintf("%s %s (%s)", action, name, ctr.Image)
		}
		return fmt.Sprintf("%s %s", action, name)
	}
	return "container event"
}

// formatGenericEvent formats unknown event types
func (f *RawEventFormatter) formatGenericEvent(event *RawEvent) string {
	return fmt.Sprintf("%s event", event.Type.String())
}

// formatL7Details formats Layer 7 protocol details
func (f *RawEventFormatter) formatL7Details(net *NetworkEvent) string {
	switch net.L7Protocol {
	case "http":
		if details := net.L7Details; details != nil {
			if method, ok := details["method"].(string); ok {
				if url, ok := details["url"].(string); ok {
					status := ""
					if statusCode, ok := details["status_code"].(int); ok {
						status = fmt.Sprintf(" %d", statusCode)
						if statusCode >= 400 {
							status = f.colorize(status, colorRed)
						} else if statusCode >= 300 {
							status = f.colorize(status, colorYellow)
						} else {
							status = f.colorize(status, colorGreen)
						}
					}
					return fmt.Sprintf("HTTP %s %s%s", method, url, status)
				}
			}
		}
		return "HTTP"
	case "dns":
		if details := net.L7Details; details != nil {
			if query, ok := details["query"].(string); ok {
				return fmt.Sprintf("DNS %s", query)
			}
		}
		return "DNS"
	case "grpc":
		if details := net.L7Details; details != nil {
			if method, ok := details["method"].(string); ok {
				return fmt.Sprintf("gRPC %s", method)
			}
		}
		return "gRPC"
	default:
		return strings.ToUpper(net.L7Protocol)
	}
}

// formatMetadata formats additional event metadata
func (f *RawEventFormatter) formatMetadata(event *RawEvent) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("cpu=%d", event.CPU))
	parts = append(parts, fmt.Sprintf("uid=%d", event.UID))

	if event.TID != event.PID {
		parts = append(parts, fmt.Sprintf("tid=%d", event.TID))
	}

	return fmt.Sprintf("[%s]", strings.Join(parts, " "))
}

// convertToStructuredEvent converts to structured JSON format
func (f *RawEventFormatter) convertToStructuredEvent(event *RawEvent) map[string]interface{} {
	timestamp := time.Unix(0, int64(event.Timestamp))

	structuredEvent := map[string]interface{}{
		"time":      timestamp.Format(time.RFC3339Nano),
		"node_name": "localhost", // Would be actual node name in production
		"event_type": map[string]interface{}{
			"type":     event.Type.String(),
			"sub_type": f.getEventSubType(event),
		},
		"source": map[string]interface{}{
			"ID":       event.PID,
			"identity": event.UID,
			"labels":   []string{event.Comm},
		},
		"summary": f.formatEventDescription(event),
	}

	// Add event-specific fields
	switch event.Type {
	case EventTypeNetwork:
		if net, ok := event.Details.(*NetworkEvent); ok {
			structuredEvent["l4"] = map[string]interface{}{
				"TCP": map[string]interface{}{
					"source_port":      net.SourcePort,
					"destination_port": net.DestPort,
				},
			}
			structuredEvent["source_endpoint"] = map[string]interface{}{
				"IP": net.SourceIP,
			}
			structuredEvent["destination_endpoint"] = map[string]interface{}{
				"IP": net.DestIP,
			}
			if net.L7Protocol != "" {
				structuredEvent["l7"] = map[string]interface{}{
					"type": strings.ToUpper(net.L7Protocol),
				}
			}
		}
	case EventTypeProcess:
		if proc, ok := event.Details.(*ProcessEvent); ok {
			structuredEvent["process"] = map[string]interface{}{
				"command": proc.Args,
				"parent":  proc.ParentPID,
			}
		}
	}

	return structuredEvent
}

// Helper methods
func (f *RawEventFormatter) getEventSubType(event *RawEvent) string {
	switch event.Type {
	case EventTypeNetwork:
		if net, ok := event.Details.(*NetworkEvent); ok {
			return net.SubType.String()
		}
	case EventTypeProcess:
		if proc, ok := event.Details.(*ProcessEvent); ok {
			return proc.SubType.String()
		}
	case EventTypeFile:
		if file, ok := event.Details.(*FileEvent); ok {
			return file.SubType.String()
		}
	}
	return ""
}

func (f *RawEventFormatter) getProtocolName(protocol uint16) string {
	switch protocol {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 1:
		return "ICMP"
	default:
		return fmt.Sprintf("Proto-%d", protocol)
	}
}

func (f *RawEventFormatter) formatBytes(bytes uint32) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	} else {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
}

func (f *RawEventFormatter) getEventTypeColor(eventType EventType) string {
	switch eventType {
	case EventTypeNetwork:
		return colorBlue
	case EventTypeProcess:
		return colorGreen
	case EventTypeFile:
		return colorYellow
	case EventTypeSecurity:
		return colorRed
	case EventTypeContainer:
		return colorCyan
	default:
		return colorWhite
	}
}

// Color constants (ANSI escape codes)
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorWhite   = "\033[37m"
	colorGray    = "\033[90m"
)

func (f *RawEventFormatter) colorize(text, color string) string {
	if !f.colorOutput {
		return text
	}
	return color + text + colorReset
}

// String methods for enum types
func (n NetworkEventType) String() string {
	switch n {
	case NetworkEventConnect:
		return "connect"
	case NetworkEventAccept:
		return "accept"
	case NetworkEventClose:
		return "close"
	case NetworkEventSend:
		return "send"
	case NetworkEventRecv:
		return "recv"
	case NetworkEventDNS:
		return "dns"
	case NetworkEventHTTP:
		return "http"
	case NetworkEventTLS:
		return "tls"
	default:
		return "unknown"
	}
}

func (p ProcessEventType) String() string {
	switch p {
	case ProcessEventExec:
		return "exec"
	case ProcessEventExit:
		return "exit"
	case ProcessEventFork:
		return "fork"
	case ProcessEventSignal:
		return "signal"
	case ProcessEventSetuid:
		return "setuid"
	case ProcessEventSetgid:
		return "setgid"
	default:
		return "unknown"
	}
}

func (f FileEventType) String() string {
	switch f {
	case FileEventOpen:
		return "open"
	case FileEventClose:
		return "close"
	case FileEventRead:
		return "read"
	case FileEventWrite:
		return "write"
	case FileEventCreate:
		return "create"
	case FileEventDelete:
		return "delete"
	case FileEventRename:
		return "rename"
	case FileEventChmod:
		return "chmod"
	case FileEventChown:
		return "chown"
	default:
		return "unknown"
	}
}
