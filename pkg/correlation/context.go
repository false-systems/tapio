package correlation

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/falseyair/tapio/pkg/correlation/foundation"
	"github.com/falseyair/tapio/pkg/correlation/types"
)

// Context provides the execution environment for correlation rules
type Context struct {
	// Time window for this correlation cycle
	Window foundation.TimeWindow `json:"window"`

	// All events in the current window
	events []foundation.Event `json:"events"`

	// Metrics available for this window
	metrics map[string]types.MetricSeries `json:"metrics"`

	// Cached event indices for performance
	eventsBySource map[foundation.SourceType][]foundation.Event
	eventsByType   map[string][]foundation.Event
	eventsByEntity map[string][]foundation.Event

	// Correlation metadata
	CorrelationID string            `json:"correlation_id"`
	RuleID        string            `json:"rule_id"`
	Metadata      map[string]string `json:"metadata"`
}

// NewContext creates a new correlation context
func NewContext(window foundation.TimeWindow, events []types.Event) *Context {
	// Convert types.Event to foundation.Event
	foundationEvents := make([]foundation.Event, len(events))
	for i, event := range events {
		foundationEvents[i] = foundation.Event{
			ID:        event.ID,
			Timestamp: event.Timestamp,
			Source:    foundation.SourceType(event.Source),
			Type:      event.Type,
			Entity: foundation.Entity{
				Type:      event.Entity.Type,
				Name:      event.Entity.Name,
				Namespace: event.Entity.Namespace,
				Node:      event.Entity.Node,
				Pod:       event.Entity.Pod,
				Container: event.Entity.Container,
				Process:   event.Entity.Process,
				UID:       event.Entity.UID,
				Metadata:  event.Entity.Metadata,
			},
			Attributes:  event.Attributes,
			Fingerprint: event.Fingerprint,
			Labels:      event.Labels,
		}
	}

	ctx := &Context{
		Window:         window,
		events:         foundationEvents,
		metrics:        make(map[string]types.MetricSeries),
		eventsBySource: make(map[foundation.SourceType][]foundation.Event),
		eventsByType:   make(map[string][]foundation.Event),
		eventsByEntity: make(map[string][]foundation.Event),
		Metadata:       make(map[string]string),
	}

	// Build indices for fast lookups
	ctx.buildIndices()

	return ctx
}

// NewTestContext creates a context for testing with the given events
func NewTestContext(events []types.Event) *Context {
	if len(events) == 0 {
		return NewContext(foundation.TimeWindow{
			Start: time.Now().Add(-5 * time.Minute),
			End:   time.Now(),
		}, events)
	}

	// Calculate window from events
	start := events[0].Timestamp
	end := events[0].Timestamp

	for _, event := range events {
		if event.Timestamp.Before(start) {
			start = event.Timestamp
		}
		if event.Timestamp.After(end) {
			end = event.Timestamp
		}
	}

	return NewContext(foundation.TimeWindow{Start: start, End: end}, events)
}

// buildIndices creates lookup indices for efficient event filtering
func (c *Context) buildIndices() {
	for _, event := range c.events {
		// Index by source
		c.eventsBySource[event.Source] = append(c.eventsBySource[event.Source], event)

		// Index by type
		c.eventsByType[event.Type] = append(c.eventsByType[event.Type], event)

		// Index by entity
		entityKey := c.entityKey(event.Entity)
		c.eventsByEntity[entityKey] = append(c.eventsByEntity[entityKey], event)
	}
}

// entityKey creates a unique key for an entity
func (c *Context) entityKey(entity foundation.Entity) string {
	if entity.Namespace != "" {
		return fmt.Sprintf("%s:%s/%s", entity.Type, entity.Namespace, entity.Name)
	}
	return fmt.Sprintf("%s:%s", entity.Type, entity.Name)
}

// GetEvents returns events matching the given filter
func (c *Context) GetEvents(filter foundation.Filter) []foundation.Event {
	var candidates []foundation.Event

	// Use indices when possible for better performance
	if filter.Source != "" {
		candidates = c.eventsBySource[filter.Source]
	} else if filter.Type != "" {
		candidates = c.eventsByType[filter.Type]
	} else {
		candidates = c.events
	}

	var result []foundation.Event
	for _, event := range candidates {
		if filter.Matches(event) {
			result = append(result, event)
		}
	}

	// Sort by timestamp
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	// Apply limit if specified
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}

	return result
}

// GetEventsBySource returns all events from a specific source
func (c *Context) GetEventsBySource(source foundation.SourceType) []foundation.Event {
	return c.eventsBySource[source]
}

// GetEventsByType returns all events of a specific type
func (c *Context) GetEventsByType(eventType string) []foundation.Event {
	return c.eventsByType[eventType]
}

// GetEventsForEntity returns all events related to a specific entity
func (c *Context) GetEventsForEntity(entity foundation.Entity) []foundation.Event {
	entityKey := c.entityKey(entity)
	return c.eventsByEntity[entityKey]
}

// CountEvents returns the number of events matching the filter
func (c *Context) CountEvents(filter foundation.Filter) int {
	return len(c.GetEvents(filter))
}

// HasEvents checks if any events match the filter
func (c *Context) HasEvents(filter foundation.Filter) bool {
	return c.CountEvents(filter) > 0
}

// GetMetric returns a metric series by name
func (c *Context) GetMetric(name string) types.MetricSeries {
	return c.metrics[name]
}

// SetMetric adds a metric series to the context
func (c *Context) SetMetric(name string, series types.MetricSeries) {
	c.metrics[name] = series
}

// GetMetricValue returns the latest value for a metric
func (c *Context) GetMetricValue(name string) float64 {
	series, exists := c.metrics[name]
	if !exists || len(series.Points) == 0 {
		return 0
	}

	// Return the most recent value
	return series.Points[len(series.Points)-1].Value
}

// EntitiesRelated checks if two entities are related (same node, pod, etc.)
func (c *Context) EntitiesRelated(e1, e2 foundation.Entity) bool {
	// Same entity
	if e1.UID == e2.UID && e1.UID != "" {
		return true
	}

	// Same pod
	if e1.Pod == e2.Pod && e1.Pod != "" {
		return true
	}

	// Same container
	if e1.Container == e2.Container && e1.Container != "" {
		return true
	}

	// Same process
	if e1.Process == e2.Process && e1.Process != "" {
		return true
	}

	return false
}

// SameNode checks if two events occurred on the same node
func (c *Context) SameNode(e1, e2 foundation.Event) bool {
	return e1.Entity.Node != "" && e1.Entity.Node == e2.Entity.Node
}

// SamePod checks if two events occurred in the same pod
func (c *Context) SamePod(e1, e2 foundation.Event) bool {
	return e1.Entity.Pod != "" && e1.Entity.Pod == e2.Entity.Pod &&
		e1.Entity.Namespace == e2.Entity.Namespace
}

// SameNamespace checks if two events occurred in the same namespace
func (c *Context) SameNamespace(e1, e2 foundation.Event) bool {
	return e1.Entity.Namespace != "" && e1.Entity.Namespace == e2.Entity.Namespace
}

// TimeBetween calculates the duration between two events
func (c *Context) TimeBetween(e1, e2 foundation.Event) time.Duration {
	return e2.Timestamp.Sub(e1.Timestamp).Abs()
}

// EventsInSequence checks if events occurred in a specific time sequence
func (c *Context) EventsInSequence(events []foundation.Event, maxGap time.Duration) bool {
	if len(events) < 2 {
		return true
	}

	for i := 1; i < len(events); i++ {
		gap := events[i].Timestamp.Sub(events[i-1].Timestamp)
		if gap < 0 || gap > maxGap {
			return false
		}
	}

	return true
}

// CalculateFrequency calculates the frequency of events per unit time
func (c *Context) CalculateFrequency(events []foundation.Event, unit time.Duration) float64 {
	if len(events) == 0 || c.Window.Duration() == 0 {
		return 0
	}

	return float64(len(events)) / (c.Window.Duration().Seconds() / unit.Seconds())
}

// DetectSpike detects if there's a spike in event frequency
func (c *Context) DetectSpike(events []foundation.Event, baselineEvents []foundation.Event, threshold float64) bool {
	if len(baselineEvents) == 0 {
		return false
	}

	currentRate := c.CalculateFrequency(events, time.Minute)
	baselineRate := float64(len(baselineEvents)) / (c.Window.Duration().Minutes())

	if baselineRate == 0 {
		return currentRate > 0
	}

	return currentRate/baselineRate > threshold
}

// AnalyzeTrend analyzes the trend in a metric series
func (c *Context) AnalyzeTrend(series types.MetricSeries) TrendAnalysis {
	if len(series.Points) < 2 {
		return TrendAnalysis{Direction: TrendUnknown}
	}

	// Simple linear regression to determine trend
	n := float64(len(series.Points))
	sumX, sumY, sumXY, sumXX := 0.0, 0.0, 0.0, 0.0

	for i, point := range series.Points {
		x := float64(i)
		y := point.Value

		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}

	// Calculate slope (trend direction)
	slope := (n*sumXY - sumX*sumY) / (n*sumXX - sumX*sumX)

	// Calculate correlation coefficient for trend strength
	meanX := sumX / n
	meanY := sumY / n

	numerator := 0.0
	denomX := 0.0
	denomY := 0.0

	for i, point := range series.Points {
		x := float64(i)
		y := point.Value

		numerator += (x - meanX) * (y - meanY)
		denomX += (x - meanX) * (x - meanX)
		denomY += (y - meanY) * (y - meanY)
	}

	correlation := numerator / math.Sqrt(denomX*denomY)

	direction := TrendFlat
	if slope > 0.1 {
		direction = TrendIncreasing
	} else if slope < -0.1 {
		direction = TrendDecreasing
	}

	strength := TrendWeak
	if math.Abs(correlation) > 0.7 {
		strength = TrendStrong
	} else if math.Abs(correlation) > 0.3 {
		strength = TrendModerate
	}

	return TrendAnalysis{
		Direction:   direction,
		Strength:    strength,
		Slope:       slope,
		Correlation: correlation,
	}
}

// TrendDirection represents the direction of a trend
type TrendDirection string

const (
	TrendIncreasing TrendDirection = "increasing"
	TrendDecreasing TrendDirection = "decreasing"
	TrendFlat       TrendDirection = "flat"
	TrendUnknown    TrendDirection = "unknown"
)

// TrendStrength represents the strength of a trend
type TrendStrength string

const (
	TrendWeak     TrendStrength = "weak"
	TrendModerate TrendStrength = "moderate"
	TrendStrong   TrendStrength = "strong"
)

// TrendAnalysis contains the results of trend analysis
type TrendAnalysis struct {
	Direction   TrendDirection `json:"direction"`
	Strength    TrendStrength  `json:"strength"`
	Slope       float64        `json:"slope"`
	Correlation float64        `json:"correlation"`
}

// IsIncreasing returns true if the trend is increasing
func (ta TrendAnalysis) IsIncreasing() bool {
	return ta.Direction == TrendIncreasing
}

// IsDecreasing returns true if the trend is decreasing
func (ta TrendAnalysis) IsDecreasing() bool {
	return ta.Direction == TrendDecreasing
}

// IsStrong returns true if the trend is strong
func (ta TrendAnalysis) IsStrong() bool {
	return ta.Strength == TrendStrong
}

// GetUniqueNodes returns unique node names from events
func (c *Context) GetUniqueNodes(events []foundation.Event) []string {
	nodeSet := make(map[string]bool)
	for _, event := range events {
		if event.Entity.Node != "" {
			nodeSet[event.Entity.Node] = true
		}
	}

	var nodes []string
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	sort.Strings(nodes)
	return nodes
}

// GetUniquePods returns unique pod names from events
func (c *Context) GetUniquePods(events []foundation.Event) []string {
	podSet := make(map[string]bool)
	for _, event := range events {
		if event.Entity.Pod != "" {
			key := event.Entity.Namespace + "/" + event.Entity.Pod
			podSet[key] = true
		}
	}

	var pods []string
	for pod := range podSet {
		pods = append(pods, pod)
	}

	sort.Strings(pods)
	return pods
}

// GetUniqueNamespaces returns unique namespace names from events
func (c *Context) GetUniqueNamespaces(events []foundation.Event) []string {
	nsSet := make(map[string]bool)
	for _, event := range events {
		if event.Entity.Namespace != "" {
			nsSet[event.Entity.Namespace] = true
		}
	}

	var namespaces []string
	for ns := range nsSet {
		namespaces = append(namespaces, ns)
	}

	sort.Strings(namespaces)
	return namespaces
}
