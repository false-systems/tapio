package bridge

import (
	"fmt"
	"strconv"
	"time"

	"github.com/yairfalse/tapio/pkg/collector"
	"github.com/yairfalse/tapio/pkg/events_correlation"
)

// TapioEventBridge converts between Tapio collector.Event and events_correlation.Event
type TapioEventBridge struct {
	manager *collector.SimpleManager
}

// NewTapioEventBridge creates a new event bridge
func NewTapioEventBridge(manager *collector.SimpleManager) *TapioEventBridge {
	return &TapioEventBridge{
		manager: manager,
	}
}

// StreamEvents creates a stream of events_correlation.Event from Tapio collectors
func (b *TapioEventBridge) StreamEvents() <-chan events_correlation.Event {
	output := make(chan events_correlation.Event, 1000)
	
	go func() {
		defer close(output)
		
		// Get events from the manager's unified stream
		tapioEvents := b.manager.Events()
		
		for tapioEvent := range tapioEvents {
			// Convert each Tapio event to events_correlation format
			if correlationEvent := b.convertEvent(tapioEvent); correlationEvent != nil {
				select {
				case output <- *correlationEvent:
				default:
					// Drop event if buffer full
				}
			}
		}
	}()
	
	return output
}

// convertEvent converts a collector.Event to events_correlation.Event
func (b *TapioEventBridge) convertEvent(tapioEvent collector.Event) *events_correlation.Event {
	// Map the source type
	var source events_correlation.EventSource
	switch tapioEvent.Source {
	case "ebpf":
		source = events_correlation.SourceEBPF
	case "kubernetes", "k8s":
		source = events_correlation.SourceKubernetes
	case "systemd":
		source = events_correlation.SourceSystemd
	case "journald":
		source = events_correlation.SourceJournald
	case "metrics":
		source = events_correlation.SourceMetrics
	default:
		// Default to eBPF for unknown sources from collectors
		source = events_correlation.SourceEBPF
	}

	// Create entity from context
	entity := events_correlation.Entity{
		Type: determineEntityType(tapioEvent),
		UID:  tapioEvent.ID,
	}

	// Extract entity details from context if available
	if tapioEvent.Context != nil {
		entity.Namespace = tapioEvent.Context.Namespace
		entity.Node = tapioEvent.Context.Node
		entity.Pod = tapioEvent.Context.Pod
		entity.Container = tapioEvent.Context.Container
		
		// Use pod name as entity name if available, otherwise use process
		if entity.Pod != "" {
			entity.Name = entity.Pod
			entity.Type = "pod"
		} else if tapioEvent.Context.ProcessName != "" {
			entity.Name = tapioEvent.Context.ProcessName
			entity.Process = tapioEvent.Context.ProcessName
			entity.Type = "process"
		}
		
		// Create metadata from context
		entity.Metadata = make(map[string]string)
		if tapioEvent.Context.ProcessName != "" {
			entity.Metadata["process_name"] = tapioEvent.Context.ProcessName
		}
		if tapioEvent.Context.PID > 0 {
			entity.Metadata["pid"] = strconv.Itoa(int(tapioEvent.Context.PID))
		}
		// ContainerID not available in EventContext, use Container field instead
		if tapioEvent.Context.Container != "" {
			entity.Metadata["container"] = tapioEvent.Context.Container
		}
	}

	// Convert data to attributes
	attributes := make(map[string]interface{})
	for k, v := range tapioEvent.Data {
		attributes[k] = v
	}

	// Add severity to attributes if available
	if tapioEvent.Severity != "" {
		attributes["severity"] = string(tapioEvent.Severity)
	}
	// Note: collector.Event doesn't have a Message field, severity and data provide the context

	// Create labels from available data
	labels := make(map[string]string)
	if tapioEvent.Context != nil {
		if tapioEvent.Context.Namespace != "" {
			labels["namespace"] = tapioEvent.Context.Namespace
		}
		if tapioEvent.Context.Node != "" {
			labels["node"] = tapioEvent.Context.Node
		}
		if tapioEvent.Context.Pod != "" {
			labels["pod"] = tapioEvent.Context.Pod
		}
		if tapioEvent.Context.Container != "" {
			labels["container"] = tapioEvent.Context.Container
		}
	}
	if tapioEvent.Source != "" {
		labels["source"] = tapioEvent.Source
	}

	// Generate fingerprint from key event characteristics
	fingerprint := generateFingerprint(tapioEvent)

	return &events_correlation.Event{
		ID:          tapioEvent.ID,
		Timestamp:   tapioEvent.Timestamp,
		Source:      source,
		Type:        tapioEvent.Type,
		Entity:      entity,
		Attributes:  attributes,
		Fingerprint: fingerprint,
		Labels:      labels,
	}
}

// determineEntityType determines the entity type based on the event
func determineEntityType(event collector.Event) string {
	if event.Context == nil {
		return "unknown"
	}

	// Priority order: pod > container > process > node
	if event.Context.Pod != "" {
		return "pod"
	}
	if event.Context.Container != "" {
		return "container"
	}
	if event.Context.ProcessName != "" || event.Context.PID > 0 {
		return "process"
	}
	if event.Context.Node != "" {
		return "node"
	}

	return "unknown"
}

// generateFingerprint creates a unique fingerprint for the event
func generateFingerprint(event collector.Event) string {
	// Create fingerprint from type + entity + key attributes
	base := fmt.Sprintf("%s:%s", event.Type, event.Source)
	
	if event.Context != nil {
		if event.Context.Pod != "" {
			base += ":" + event.Context.Pod
		} else if event.Context.ProcessName != "" {
			base += ":" + event.Context.ProcessName
		}
		
		if event.Context.Namespace != "" {
			base += ":" + event.Context.Namespace
		}
	}

	// Add key data fields that help identify similar events
	if currentUsage, ok := event.Data["current_usage"]; ok {
		base += fmt.Sprintf(":usage=%v", currentUsage)
	}
	if memUsage, ok := event.Data["memory_usage"]; ok {
		base += fmt.Sprintf(":mem=%v", memUsage)
	}
	if cpuUsage, ok := event.Data["cpu_usage"]; ok {
		base += fmt.Sprintf(":cpu=%v", cpuUsage)
	}

	return base
}

// GetInsights streams insights from the Tapio correlation engine
func (b *TapioEventBridge) GetInsights() <-chan events_correlation.Result {
	output := make(chan events_correlation.Result, 100)
	
	go func() {
		defer close(output)
		
		// Get insights from the manager's correlation engine
		tapioInsights := b.manager.Insights()
		
		for insight := range tapioInsights {
			// Convert each Tapio insight to events_correlation format
			if result := b.convertInsight(insight); result != nil {
				select {
				case output <- *result:
				default:
					// Drop insight if buffer full
				}
			}
		}
	}()
	
	return output
}

// convertInsight converts a collector.Insight to events_correlation.Result
func (b *TapioEventBridge) convertInsight(insight collector.Insight) *events_correlation.Result {
	// Map severity
	var severity events_correlation.Severity
	switch insight.Severity {
	case collector.SeverityLow:
		severity = events_correlation.SeverityLow
	case collector.SeverityMedium:
		severity = events_correlation.SeverityMedium
	case collector.SeverityHigh:
		severity = events_correlation.SeverityHigh
	case collector.SeverityCritical:
		severity = events_correlation.SeverityCritical
	default:
		severity = events_correlation.SeverityMedium
	}

	// Map category based on insight type
	var category events_correlation.Category
	switch insight.Type {
	case "oom_correlation", "oom_prediction", "memory_leak":
		category = events_correlation.CategoryResource
	case "crash_loop_correlation":
		category = events_correlation.CategoryReliability
	case "network_correlation":
		category = events_correlation.CategoryNetwork
	case "cpu_throttling", "high_cpu":
		category = events_correlation.CategoryPerformance
	default:
		category = events_correlation.CategoryResource
	}

	// Convert affected resources to entities
	var entities []events_correlation.Entity
	for _, resource := range insight.Resources {
		entity := events_correlation.Entity{
			Type:      resource.Type,
			Name:      resource.Name,
			Namespace: resource.Namespace,
			UID:       fmt.Sprintf("%s:%s:%s", resource.Type, resource.Namespace, resource.Name),
		}
		// Convert labels to metadata
		if len(resource.Labels) > 0 {
			entity.Metadata = resource.Labels
		}
		entities = append(entities, entity)
	}

	// Convert actions to recommendations
	var recommendations []string
	var actions []events_correlation.Action
	for _, action := range insight.Actions {
		recommendations = append(recommendations, action.Description)
		
		// Convert to correlation action format
		correlationAction := events_correlation.Action{
			Type:   "manual", // Most Tapio actions are manual
			Target: action.Title,
		}
		
		// Convert commands to parameters
		if len(action.Commands) > 0 {
			correlationAction.Parameters = make(map[string]string)
			for i, cmd := range action.Commands {
				correlationAction.Parameters[fmt.Sprintf("command_%d", i)] = cmd
			}
		}
		
		// Set priority based on risk
		switch action.Risk {
		case "low":
			correlationAction.Priority = "low"
		case "medium":
			correlationAction.Priority = "medium"
		case "high":
			correlationAction.Priority = "high"
		default:
			correlationAction.Priority = "medium"
		}
		
		actions = append(actions, correlationAction)
	}

	// Create evidence from related events
	evidence := events_correlation.Evidence{
		Events:   []events_correlation.Event{}, // Would need to lookup actual events
		Entities: entities,
	}

	// Add prediction data if available
	if insight.Prediction != nil {
		evidence.Metrics = make(map[string]float64)
		evidence.Metrics["probability"] = insight.Prediction.Probability
		evidence.Metrics["confidence"] = insight.Prediction.Confidence
		if insight.Prediction.TimeToEvent > 0 {
			evidence.Metrics["time_to_event_seconds"] = insight.Prediction.TimeToEvent.Seconds()
		}
	}

	// Create metadata
	metadata := make(map[string]string)
	metadata["insight_id"] = insight.ID
	metadata["insight_type"] = insight.Type
	if insight.Prediction != nil {
		metadata["prediction_type"] = insight.Prediction.Type
	}

	return &events_correlation.Result{
		RuleID:          fmt.Sprintf("tapio_%s", insight.Type),
		RuleName:        insight.Title,
		Timestamp:       insight.Timestamp,
		Confidence:      0.85, // Default confidence for Tapio insights
		Severity:        severity,
		Category:        category,
		Title:           insight.Title,
		Description:     insight.Description,
		Evidence:        evidence,
		Recommendations: recommendations,
		Actions:         actions,
		TTL:             24 * time.Hour, // Default TTL for insights
		Metadata:        metadata,
	}
}

// GetHealthStatus returns the health status of the bridge
func (b *TapioEventBridge) GetHealthStatus() map[string]interface{} {
	managerHealth := b.manager.Health()
	
	bridgeHealth := make(map[string]interface{})
	bridgeHealth["manager_health"] = managerHealth
	bridgeHealth["bridge_status"] = "healthy"
	bridgeHealth["conversion_active"] = true
	
	return bridgeHealth
}