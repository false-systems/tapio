package journald

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors/types"
)

// SemanticEnricher adds semantic context to events at collection time
// This is CRITICAL - we enrich ONCE at source, not repeatedly during analysis
type SemanticEnricher struct {
	// Enrichment rules
	processEnricher    *processEnricher
	containerEnricher  *containerEnricher
	kubernetesEnricher *kubernetesEnricher
	errorEnricher      *errorEnricher
	
	// Caches for performance
	processCache    *enrichmentCache
	containerCache  *enrichmentCache
	
	// Pattern matchers for context extraction
	patterns map[string]*contextPattern
}

type contextPattern struct {
	regex   *regexp.Regexp
	extract func(matches []string) map[string]string
}

type enrichmentCache struct {
	mu      sync.RWMutex
	entries map[string]*enrichmentEntry
	maxSize int
}

type enrichmentEntry struct {
	data      map[string]interface{}
	timestamp int64
}

// NewSemanticEnricher creates an enricher for semantic context
func NewSemanticEnricher() *SemanticEnricher {
	e := &SemanticEnricher{
		processEnricher:    newProcessEnricher(),
		containerEnricher:  newContainerEnricher(),
		kubernetesEnricher: newKubernetesEnricher(),
		errorEnricher:      newErrorEnricher(),
		processCache:       newEnrichmentCache(1000),
		containerCache:     newEnrichmentCache(500),
		patterns:           make(map[string]*contextPattern),
	}
	
	e.initializePatterns()
	return e
}

// Enrich adds semantic context to an event
func (e *SemanticEnricher) Enrich(event *types.Event, entry *JournalEntry) {
	// Add base enrichment
	e.addBaseEnrichment(event, entry)
	
	// Process-specific enrichment
	if entry.PID > 0 {
		e.processEnricher.enrich(event, entry, e.processCache)
	}
	
	// Container enrichment
	if containerID := e.detectContainerID(entry); containerID != "" {
		e.containerEnricher.enrich(event, containerID, e.containerCache)
	}
	
	// Kubernetes enrichment
	e.kubernetesEnricher.enrich(event, entry)
	
	// Error context enrichment
	if event.Severity >= types.SeverityWarning {
		e.errorEnricher.enrich(event, entry)
	}
	
	// Pattern-based enrichment
	e.applyPatternEnrichment(event, entry)
	
	// Add event fingerprint for deduplication
	e.addFingerprint(event)
	
	// Add correlation hints
	e.addCorrelationHints(event)
}

// initializePatterns sets up context extraction patterns
func (e *SemanticEnricher) initializePatterns() {
	// IP address extraction
	e.patterns["ip_address"] = &contextPattern{
		regex: regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`),
		extract: func(matches []string) map[string]string {
			return map[string]string{"ip_address": matches[1]}
		},
	}
	
	// Port extraction
	e.patterns["port"] = &contextPattern{
		regex: regexp.MustCompile(`(?i)port[:\s]+(\d{1,5})`),
		extract: func(matches []string) map[string]string {
			return map[string]string{"port": matches[1]}
		},
	}
	
	// File path extraction
	e.patterns["file_path"] = &contextPattern{
		regex: regexp.MustCompile(`(/[\w\-./]+)`),
		extract: func(matches []string) map[string]string {
			return map[string]string{"file_path": matches[1]}
		},
	}
	
	// Error code extraction
	e.patterns["error_code"] = &contextPattern{
		regex: regexp.MustCompile(`(?i)(?:error|err|code)[:\s]+([A-Z0-9_]+)`),
		extract: func(matches []string) map[string]string {
			return map[string]string{"error_code": matches[1]}
		},
	}
	
	// Kubernetes resource extraction
	e.patterns["k8s_resource"] = &contextPattern{
		regex: regexp.MustCompile(`(?i)(pod|service|deployment|daemonset|statefulset|job)/([a-z0-9-]+)`),
		extract: func(matches []string) map[string]string {
			return map[string]string{
				"k8s_resource_type": matches[1],
				"k8s_resource_name": matches[2],
			}
		},
	}
}

// addBaseEnrichment adds basic enrichment to all events
func (e *SemanticEnricher) addBaseEnrichment(event *types.Event, entry *JournalEntry) {
	// Add systemd context
	if entry.SystemdUnit != "" {
		event.Attributes["systemd_unit"] = entry.SystemdUnit
		event.Attributes["service_type"] = e.classifyService(entry.SystemdUnit)
	}
	
	// Add boot context
	if entry.BootID != "" {
		event.Attributes["boot_id"] = entry.BootID
	}
	
	// Add machine context
	if entry.MachineID != "" {
		event.Attributes["machine_id"] = entry.MachineID
	}
	
	// Add transport info
	if entry.Transport != "" {
		event.Attributes["transport"] = entry.Transport
	}
	
	// Add user context if available
	if entry.UID >= 0 {
		event.Attributes["uid"] = entry.UID
		event.Attributes["user_type"] = e.classifyUser(entry.UID)
	}
	
	// Add SELinux context if present
	if entry.SELinuxContext != "" {
		event.Attributes["selinux_context"] = entry.SELinuxContext
	}
}

// detectContainerID attempts to detect container ID from various sources
func (e *SemanticEnricher) detectContainerID(entry *JournalEntry) string {
	// Check systemd unit for container ID
	if strings.Contains(entry.SystemdUnit, "docker-") {
		parts := strings.Split(entry.SystemdUnit, "docker-")
		if len(parts) > 1 {
			return strings.TrimSuffix(parts[1], ".scope")
		}
	}
	
	// Check message for container ID
	containerPattern := regexp.MustCompile(`[a-f0-9]{64}|[a-f0-9]{12}`)
	if matches := containerPattern.FindString(entry.Message); matches != "" {
		return matches
	}
	
	return ""
}

// applyPatternEnrichment applies pattern-based context extraction
func (e *SemanticEnricher) applyPatternEnrichment(event *types.Event, entry *JournalEntry) {
	message := entry.Message
	
	for name, pattern := range e.patterns {
		if matches := pattern.regex.FindStringSubmatch(message); matches != nil {
			extracted := pattern.extract(matches)
			for k, v := range extracted {
				event.Attributes["extracted_"+k] = v
			}
		}
	}
}

// addFingerprint creates a unique fingerprint for deduplication
func (e *SemanticEnricher) addFingerprint(event *types.Event) {
	// Create fingerprint from key fields
	parts := []string{
		event.Source.Component,
		string(event.Type),
		string(event.Category),
	}
	
	// Add key data fields
	if unit, ok := event.Data["unit"].(string); ok {
		parts = append(parts, unit)
	}
	if pattern, ok := event.Data["pattern"].(string); ok {
		parts = append(parts, pattern)
	}
	if failureType, ok := event.Data["failure_type"].(string); ok {
		parts = append(parts, failureType)
	}
	
	// Hash the parts
	h := md5.New()
	h.Write([]byte(strings.Join(parts, ":")))
	event.Attributes["fingerprint"] = hex.EncodeToString(h.Sum(nil))
}

// addCorrelationHints adds hints for the correlation engine
func (e *SemanticEnricher) addCorrelationHints(event *types.Event) {
	hints := make([]string, 0)
	
	// Add hints based on event type
	switch event.Type {
	case types.EventTypeOOM:
		hints = append(hints, "memory_pressure", "resource_exhaustion")
	case types.EventTypeContainerFailure:
		hints = append(hints, "service_disruption", "deployment_failure")
	case types.EventTypeLog:
		if event.Severity >= types.SeverityError {
			hints = append(hints, "system_instability")
		}
	}
	
	// Add hints based on patterns
	if _, ok := event.Data["network_error"]; ok {
		hints = append(hints, "network_issue", "connectivity_problem")
	}
	if _, ok := event.Data["timeout_type"]; ok {
		hints = append(hints, "performance_degradation", "latency_issue")
	}
	
	if len(hints) > 0 {
		event.Attributes["correlation_hints"] = hints
	}
}

// classifyService determines the service type
func (e *SemanticEnricher) classifyService(unit string) string {
	switch {
	case strings.Contains(unit, "docker") || strings.Contains(unit, "containerd"):
		return "container_runtime"
	case strings.Contains(unit, "kube"):
		return "kubernetes"
	case strings.Contains(unit, "etcd"):
		return "datastore"
	case strings.Contains(unit, "network"):
		return "networking"
	case strings.Contains(unit, "storage"):
		return "storage"
	default:
		return "system"
	}
}

// classifyUser determines the user type
func (e *SemanticEnricher) classifyUser(uid int) string {
	switch {
	case uid == 0:
		return "root"
	case uid < 1000:
		return "system"
	default:
		return "regular"
	}
}

// Process enricher
type processEnricher struct {
	commandPattern *regexp.Regexp
}

func newProcessEnricher() *processEnricher {
	return &processEnricher{
		commandPattern: regexp.MustCompile(`(?i)command[:\s]+(.+?)(?:\s|$)`),
	}
}

func (pe *processEnricher) enrich(event *types.Event, entry *JournalEntry, cache *enrichmentCache) {
	// Add process hierarchy info
	event.Attributes["process_tree"] = fmt.Sprintf("pid:%d", entry.PID)
	
	// Try to extract command from message
	if matches := pe.commandPattern.FindStringSubmatch(entry.Message); matches != nil {
		event.Attributes["command"] = matches[1]
	}
	
	// Add process classification
	event.Attributes["process_type"] = pe.classifyProcess(entry.SyslogIdentifier)
}

func (pe *processEnricher) classifyProcess(identifier string) string {
	switch {
	case strings.Contains(identifier, "kernel"):
		return "kernel"
	case strings.Contains(identifier, "systemd"):
		return "init"
	case strings.Contains(identifier, "kube"):
		return "kubernetes"
	case strings.Contains(identifier, "docker"):
		return "container"
	default:
		return "application"
	}
}

// Container enricher
type containerEnricher struct {
	imagePattern *regexp.Regexp
}

func newContainerEnricher() *containerEnricher {
	return &containerEnricher{
		imagePattern: regexp.MustCompile(`(?i)image[:\s]+([^\s]+)`),
	}
}

func (ce *containerEnricher) enrich(event *types.Event, containerID string, cache *enrichmentCache) {
	// Check cache first
	if cached := cache.get(containerID); cached != nil {
		for k, v := range cached {
			event.Attributes["container_"+k] = v
		}
		return
	}
	
	// Add container ID
	event.Context.ContainerID = containerID
	event.Attributes["container_id"] = containerID
	event.Labels["container"] = containerID[:12] // Short ID for labels
	
	// Try to extract image from message
	if message, ok := event.Data["message"].(string); ok {
		if matches := ce.imagePattern.FindStringSubmatch(message); matches != nil {
			event.Attributes["container_image"] = matches[1]
		}
	}
	
	// Cache the enrichment
	cache.set(containerID, map[string]interface{}{
		"id": containerID,
	})
}

// Kubernetes enricher
type kubernetesEnricher struct {
	namespacePattern *regexp.Regexp
	podPattern       *regexp.Regexp
}

func newKubernetesEnricher() *kubernetesEnricher {
	return &kubernetesEnricher{
		namespacePattern: regexp.MustCompile(`(?i)namespace[:\s]+([a-z0-9-]+)`),
		podPattern:       regexp.MustCompile(`(?i)pod[:\s]+([a-z0-9-]+)`),
	}
}

func (ke *kubernetesEnricher) enrich(event *types.Event, entry *JournalEntry) {
	message := entry.Message
	
	// Extract namespace
	if matches := ke.namespacePattern.FindStringSubmatch(message); matches != nil {
		event.Context.Namespace = matches[1]
		event.Labels["namespace"] = matches[1]
	}
	
	// Extract pod name
	if matches := ke.podPattern.FindStringSubmatch(message); matches != nil {
		event.Context.PodName = matches[1]
		event.Labels["pod"] = matches[1]
	}
	
	// Add Kubernetes context based on service
	if strings.Contains(entry.SystemdUnit, "kubelet") {
		event.Attributes["k8s_component"] = "kubelet"
	}
}

// Error enricher
type errorEnricher struct {
	stackTracePattern *regexp.Regexp
	errorTypePattern  *regexp.Regexp
}

func newErrorEnricher() *errorEnricher {
	return &errorEnricher{
		stackTracePattern: regexp.MustCompile(`(?i)(at \w+|goroutine \d+|\s+at .+:\d+)`),
		errorTypePattern:  regexp.MustCompile(`(?i)(exception|error|fault|panic):\s*([^\n]+)`),
	}
}

func (ee *errorEnricher) enrich(event *types.Event, entry *JournalEntry) {
	message := entry.Message
	
	// Check for stack trace
	if ee.stackTracePattern.MatchString(message) {
		event.Attributes["has_stack_trace"] = true
		event.Attributes["error_type"] = "crash_with_trace"
	}
	
	// Extract error type
	if matches := ee.errorTypePattern.FindStringSubmatch(message); matches != nil {
		event.Attributes["error_class"] = matches[1]
		event.Attributes["error_message"] = strings.TrimSpace(matches[2])
	}
	
	// Add error categorization
	event.Attributes["error_category"] = ee.categorizeError(message)
}

func (ee *errorEnricher) categorizeError(message string) string {
	messageLower := strings.ToLower(message)
	
	switch {
	case strings.Contains(messageLower, "permission"):
		return "permission"
	case strings.Contains(messageLower, "timeout"):
		return "timeout"
	case strings.Contains(messageLower, "connection"):
		return "network"
	case strings.Contains(messageLower, "memory"):
		return "resource"
	case strings.Contains(messageLower, "disk") || strings.Contains(messageLower, "space"):
		return "storage"
	default:
		return "general"
	}
}

// Cache implementation
func newEnrichmentCache(maxSize int) *enrichmentCache {
	return &enrichmentCache{
		entries: make(map[string]*enrichmentEntry),
		maxSize: maxSize,
	}
}

func (c *enrichmentCache) get(key string) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if entry, ok := c.entries[key]; ok {
		return entry.data
	}
	return nil
}

func (c *enrichmentCache) set(key string, data map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Simple eviction if cache is full
	if len(c.entries) >= c.maxSize {
		// Remove a random entry
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	
	c.entries[key] = &enrichmentEntry{
		data:      data,
		timestamp: 0, // Could add timestamp if needed
	}
}