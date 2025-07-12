package journald

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parsers provides message parsing and extraction capabilities
type Parsers struct {
	config   *ParsersConfig
	parsers  map[string]MessageParser
	patterns map[string]*regexp.Regexp
}

// ParsersConfig configures the message parsers
type ParsersConfig struct {
	EnableStructuredParsing bool
	CustomParsers           map[string]ParserDefinition
	CommonPatterns          map[string]string
}

// ParserDefinition defines a custom parser
type ParserDefinition struct {
	Pattern     string
	Fields      []string
	Type        string
	Description string
}

// MessageParser interface for parsing log messages
type MessageParser interface {
	Parse(message string) (*ParsedMessage, error)
	GetType() string
	CanParse(message string) bool
}

// ParsedMessage represents a parsed log message
type ParsedMessage struct {
	OriginalMessage string
	ParsedFields    map[string]interface{}
	MessageType     string
	Timestamp       *time.Time
	Severity        string
	Component       string
	Action          string
	Details         map[string]interface{}
	Metrics         map[string]float64
	Errors          []string
}

// SystemdParser parses systemd messages
type SystemdParser struct{}

// DockerParser parses Docker messages
type DockerParser struct{}

// KubernetesParser parses Kubernetes messages
type KubernetesParser struct{}

// GenericParser parses generic structured messages
type GenericParser struct {
	patterns map[string]*regexp.Regexp
}

// NewParsers creates a new parsers instance
func NewParsers(config *ParsersConfig) *Parsers {
	if config == nil {
		config = DefaultParsersConfig()
	}

	parsers := &Parsers{
		config:   config,
		parsers:  make(map[string]MessageParser),
		patterns: make(map[string]*regexp.Regexp),
	}

	// Initialize default parsers
	parsers.parsers["systemd"] = &SystemdParser{}
	parsers.parsers["docker"] = &DockerParser{}
	parsers.parsers["kubernetes"] = &KubernetesParser{}
	parsers.parsers["generic"] = &GenericParser{
		patterns: parsers.patterns,
	}

	// Compile common patterns
	parsers.compilePatterns()

	return parsers
}

// DefaultParsersConfig returns the default parsers configuration
func DefaultParsersConfig() *ParsersConfig {
	return &ParsersConfig{
		EnableStructuredParsing: true,
		CommonPatterns: map[string]string{
			"timestamp":   `\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d{3,9})?(?:Z|[+-]\d{2}:?\d{2})?`,
			"ipv4":        `(?:\d{1,3}\.){3}\d{1,3}`,
			"ipv6":        `(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}`,
			"mac_address": `(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}`,
			"uuid":        `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`,
			"pid":         `\[(\d+)\]`,
			"severity":    `(?i)(emergency|alert|critical|error|warning|notice|info|debug)`,
			"duration":    `(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)`,
			"memory_size": `(\d+(?:\.\d+)?)(B|KB|MB|GB|TB|KiB|MiB|GiB|TiB)`,
			"percentage":  `(\d+(?:\.\d+)?)%`,
			"http_status": `\b([1-5]\d{2})\b`,
		},
	}
}

// ParseMessage parses a log message using appropriate parser
func (p *Parsers) ParseMessage(entry *LogEntry) (*ParsedMessage, error) {
	if !p.config.EnableStructuredParsing {
		return &ParsedMessage{
			OriginalMessage: entry.Message,
			ParsedFields:    make(map[string]interface{}),
			Details:         make(map[string]interface{}),
			Metrics:         make(map[string]float64),
		}, nil
	}

	// Try service-specific parsers first
	var parser MessageParser
	switch {
	case strings.Contains(entry.Service, "systemd"):
		parser = p.parsers["systemd"]
	case strings.Contains(entry.Service, "docker"):
		parser = p.parsers["docker"]
	case strings.Contains(entry.Service, "kube"):
		parser = p.parsers["kubernetes"]
	default:
		parser = p.parsers["generic"]
	}

	if parser != nil && parser.CanParse(entry.Message) {
		return parser.Parse(entry.Message)
	}

	// Fallback to generic parser
	return p.parsers["generic"].Parse(entry.Message)
}

// compilePatterns compiles regex patterns
func (p *Parsers) compilePatterns() {
	for name, pattern := range p.config.CommonPatterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			p.patterns[name] = compiled
		}
	}
}

// SystemdParser implementation
func (sp *SystemdParser) GetType() string {
	return "systemd"
}

func (sp *SystemdParser) CanParse(message string) bool {
	return strings.Contains(message, "systemd") ||
		strings.Contains(message, "Starting") ||
		strings.Contains(message, "Started") ||
		strings.Contains(message, "Stopping") ||
		strings.Contains(message, "Stopped") ||
		strings.Contains(message, "Failed")
}

func (sp *SystemdParser) Parse(message string) (*ParsedMessage, error) {
	parsed := &ParsedMessage{
		OriginalMessage: message,
		ParsedFields:    make(map[string]interface{}),
		Details:         make(map[string]interface{}),
		Metrics:         make(map[string]float64),
		MessageType:     "systemd",
	}

	// Parse systemd state changes
	if strings.Contains(message, "Starting") {
		parsed.Action = "starting"
		if match := regexp.MustCompile(`Starting (.+?)\.\.\.`).FindStringSubmatch(message); len(match) > 1 {
			parsed.Component = match[1]
			parsed.ParsedFields["service"] = match[1]
		}
	} else if strings.Contains(message, "Started") {
		parsed.Action = "started"
		if match := regexp.MustCompile(`Started (.+?)\.`).FindStringSubmatch(message); len(match) > 1 {
			parsed.Component = match[1]
			parsed.ParsedFields["service"] = match[1]
		}
	} else if strings.Contains(message, "Stopping") {
		parsed.Action = "stopping"
		if match := regexp.MustCompile(`Stopping (.+?)\.\.\.`).FindStringSubmatch(message); len(match) > 1 {
			parsed.Component = match[1]
			parsed.ParsedFields["service"] = match[1]
		}
	} else if strings.Contains(message, "Stopped") {
		parsed.Action = "stopped"
		if match := regexp.MustCompile(`Stopped (.+?)\.`).FindStringSubmatch(message); len(match) > 1 {
			parsed.Component = match[1]
			parsed.ParsedFields["service"] = match[1]
		}
	} else if strings.Contains(message, "Failed") {
		parsed.Action = "failed"
		parsed.Severity = "error"
		if match := regexp.MustCompile(`Failed to (.+?)\.`).FindStringSubmatch(message); len(match) > 1 {
			parsed.Details["failure_reason"] = match[1]
		}
	}

	// Extract process ID if present
	if match := regexp.MustCompile(`\[(\d+)\]`).FindStringSubmatch(message); len(match) > 1 {
		if pid, err := strconv.Atoi(match[1]); err == nil {
			parsed.ParsedFields["pid"] = pid
		}
	}

	return parsed, nil
}

// DockerParser implementation
func (dp *DockerParser) GetType() string {
	return "docker"
}

func (dp *DockerParser) CanParse(message string) bool {
	return strings.Contains(message, "container") ||
		strings.Contains(message, "image") ||
		strings.Contains(message, "docker") ||
		regexp.MustCompile(`[0-9a-f]{12}`).MatchString(message)
}

func (dp *DockerParser) Parse(message string) (*ParsedMessage, error) {
	parsed := &ParsedMessage{
		OriginalMessage: message,
		ParsedFields:    make(map[string]interface{}),
		Details:         make(map[string]interface{}),
		Metrics:         make(map[string]float64),
		MessageType:     "docker",
	}

	// Parse container actions
	if strings.Contains(message, "container start") {
		parsed.Action = "container_start"
		if match := regexp.MustCompile(`container start ([0-9a-f]+)`).FindStringSubmatch(message); len(match) > 1 {
			parsed.ParsedFields["container_id"] = match[1]
		}
	} else if strings.Contains(message, "container stop") {
		parsed.Action = "container_stop"
		if match := regexp.MustCompile(`container stop ([0-9a-f]+)`).FindStringSubmatch(message); len(match) > 1 {
			parsed.ParsedFields["container_id"] = match[1]
		}
	} else if strings.Contains(message, "container die") {
		parsed.Action = "container_die"
		parsed.Severity = "warning"
		if match := regexp.MustCompile(`container die ([0-9a-f]+)`).FindStringSubmatch(message); len(match) > 1 {
			parsed.ParsedFields["container_id"] = match[1]
		}
		// Extract exit code
		if match := regexp.MustCompile(`exitCode=(\d+)`).FindStringSubmatch(message); len(match) > 1 {
			if exitCode, err := strconv.Atoi(match[1]); err == nil {
				parsed.ParsedFields["exit_code"] = exitCode
				if exitCode != 0 {
					parsed.Severity = "error"
				}
			}
		}
	}

	// Extract container name
	if match := regexp.MustCompile(`name=([^,\s]+)`).FindStringSubmatch(message); len(match) > 1 {
		parsed.ParsedFields["container_name"] = match[1]
		parsed.Component = match[1]
	}

	// Extract image
	if match := regexp.MustCompile(`image=([^,\s]+)`).FindStringSubmatch(message); len(match) > 1 {
		parsed.ParsedFields["image"] = match[1]
	}

	return parsed, nil
}

// KubernetesParser implementation
func (kp *KubernetesParser) GetType() string {
	return "kubernetes"
}

func (kp *KubernetesParser) CanParse(message string) bool {
	return strings.Contains(message, "kubelet") ||
		strings.Contains(message, "kube-proxy") ||
		strings.Contains(message, "pod") ||
		strings.Contains(message, "namespace")
}

func (kp *KubernetesParser) Parse(message string) (*ParsedMessage, error) {
	parsed := &ParsedMessage{
		OriginalMessage: message,
		ParsedFields:    make(map[string]interface{}),
		Details:         make(map[string]interface{}),
		Metrics:         make(map[string]float64),
		MessageType:     "kubernetes",
	}

	// Parse pod events
	if strings.Contains(message, "Created pod") {
		parsed.Action = "pod_created"
		if match := regexp.MustCompile(`Created pod: (.+)`).FindStringSubmatch(message); len(match) > 1 {
			parsed.ParsedFields["pod_name"] = match[1]
			parsed.Component = match[1]
		}
	} else if strings.Contains(message, "Killing pod") {
		parsed.Action = "pod_killing"
		parsed.Severity = "warning"
		if match := regexp.MustCompile(`Killing pod "(.+?)"`).FindStringSubmatch(message); len(match) > 1 {
			parsed.ParsedFields["pod_name"] = match[1]
			parsed.Component = match[1]
		}
	}

	// Extract namespace
	if match := regexp.MustCompile(`namespace[:/]?"?([^"\s,]+)"?`).FindStringSubmatch(message); len(match) > 1 {
		parsed.ParsedFields["namespace"] = match[1]
	}

	// Parse resource usage
	if strings.Contains(message, "memory") && strings.Contains(message, "usage") {
		if match := regexp.MustCompile(`(\d+(?:\.\d+)?)(Mi|Gi|Ki|M|G|K)?B?`).FindStringSubmatch(message); len(match) > 1 {
			if value, err := strconv.ParseFloat(match[1], 64); err == nil {
				unit := match[2]
				switch unit {
				case "Ki", "K":
					value *= 1024
				case "Mi", "M":
					value *= 1024 * 1024
				case "Gi", "G":
					value *= 1024 * 1024 * 1024
				}
				parsed.Metrics["memory_usage_bytes"] = value
			}
		}
	}

	return parsed, nil
}

// GenericParser implementation
func (gp *GenericParser) GetType() string {
	return "generic"
}

func (gp *GenericParser) CanParse(message string) bool {
	return true // Generic parser can handle any message
}

func (gp *GenericParser) Parse(message string) (*ParsedMessage, error) {
	parsed := &ParsedMessage{
		OriginalMessage: message,
		ParsedFields:    make(map[string]interface{}),
		Details:         make(map[string]interface{}),
		Metrics:         make(map[string]float64),
		MessageType:     "generic",
	}

	// Extract common patterns
	for name, pattern := range gp.patterns {
		if matches := pattern.FindAllString(message, -1); len(matches) > 0 {
			if len(matches) == 1 {
				parsed.ParsedFields[name] = matches[0]
			} else {
				parsed.ParsedFields[name] = matches
			}
		}
	}

	// Determine severity from message content
	messageLower := strings.ToLower(message)
	switch {
	case strings.Contains(messageLower, "emergency") || strings.Contains(messageLower, "panic"):
		parsed.Severity = "emergency"
	case strings.Contains(messageLower, "alert"):
		parsed.Severity = "alert"
	case strings.Contains(messageLower, "critical") || strings.Contains(messageLower, "fatal"):
		parsed.Severity = "critical"
	case strings.Contains(messageLower, "error") || strings.Contains(messageLower, "err"):
		parsed.Severity = "error"
	case strings.Contains(messageLower, "warning") || strings.Contains(messageLower, "warn"):
		parsed.Severity = "warning"
	case strings.Contains(messageLower, "notice"):
		parsed.Severity = "notice"
	case strings.Contains(messageLower, "info"):
		parsed.Severity = "info"
	case strings.Contains(messageLower, "debug"):
		parsed.Severity = "debug"
	}

	// Extract numeric values for metrics
	if numPattern := regexp.MustCompile(`(\d+(?:\.\d+)?)`); numPattern.MatchString(message) {
		matches := numPattern.FindAllStringSubmatch(message, -1)
		for i, match := range matches {
			if len(match) > 1 {
				if value, err := strconv.ParseFloat(match[1], 64); err == nil {
					parsed.Metrics[fmt.Sprintf("numeric_value_%d", i)] = value
				}
			}
		}
	}

	// Extract quoted strings
	if quotedPattern := regexp.MustCompile(`"([^"]+)"`); quotedPattern.MatchString(message) {
		matches := quotedPattern.FindAllStringSubmatch(message, -1)
		var quotedStrings []string
		for _, match := range matches {
			if len(match) > 1 {
				quotedStrings = append(quotedStrings, match[1])
			}
		}
		if len(quotedStrings) > 0 {
			parsed.ParsedFields["quoted_strings"] = quotedStrings
		}
	}

	return parsed, nil
}

// ExtractMetrics extracts performance metrics from parsed messages
func (p *Parsers) ExtractMetrics(parsed *ParsedMessage) map[string]float64 {
	metrics := make(map[string]float64)

	// Copy existing metrics
	for k, v := range parsed.Metrics {
		metrics[k] = v
	}

	// Extract duration metrics
	if durationPattern := p.patterns["duration"]; durationPattern != nil {
		if matches := durationPattern.FindAllStringSubmatch(parsed.OriginalMessage, -1); len(matches) > 0 {
			for i, match := range matches {
				if len(match) >= 3 {
					if value, err := strconv.ParseFloat(match[1], 64); err == nil {
						unit := match[2]
						// Convert to nanoseconds
						switch unit {
						case "ns":
							// already in nanoseconds
						case "us", "µs":
							value *= 1000
						case "ms":
							value *= 1000000
						case "s":
							value *= 1000000000
						case "m":
							value *= 60 * 1000000000
						case "h":
							value *= 3600 * 1000000000
						}
						metrics[fmt.Sprintf("duration_ns_%d", i)] = value
					}
				}
			}
		}
	}

	// Extract memory size metrics
	if memoryPattern := p.patterns["memory_size"]; memoryPattern != nil {
		if matches := memoryPattern.FindAllStringSubmatch(parsed.OriginalMessage, -1); len(matches) > 0 {
			for i, match := range matches {
				if len(match) >= 3 {
					if value, err := strconv.ParseFloat(match[1], 64); err == nil {
						unit := match[2]
						// Convert to bytes
						switch unit {
						case "B":
							// already in bytes
						case "KB":
							value *= 1000
						case "MB":
							value *= 1000000
						case "GB":
							value *= 1000000000
						case "TB":
							value *= 1000000000000
						case "KiB":
							value *= 1024
						case "MiB":
							value *= 1024 * 1024
						case "GiB":
							value *= 1024 * 1024 * 1024
						case "TiB":
							value *= 1024 * 1024 * 1024 * 1024
						}
						metrics[fmt.Sprintf("memory_bytes_%d", i)] = value
					}
				}
			}
		}
	}

	return metrics
}

// GetStatistics returns parser statistics
func (p *Parsers) GetStatistics() map[string]interface{} {
	return map[string]interface{}{
		"available_parsers":  len(p.parsers),
		"compiled_patterns":  len(p.patterns),
		"structured_parsing": p.config.EnableStructuredParsing,
	}
}
