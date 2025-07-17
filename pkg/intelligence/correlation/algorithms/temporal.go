package algorithms
import (
	"fmt"
	"math"
	"sort"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
)
// temporalAnalyzer implements temporal correlation analysis
type temporalAnalyzer struct {
	config core.AlgorithmConfig
}
// NewTemporalAnalyzer creates a new temporal analyzer
func NewTemporalAnalyzer(config core.AlgorithmConfig) core.TemporalAnalyzer {
	return &temporalAnalyzer{
		config: config,
	}
}
// AnalyzeSequence analyzes temporal relationships in an event sequence
func (t *temporalAnalyzer) AnalyzeSequence(events []domain.Event) ([]core.TemporalRelation, error) {
	if len(events) < 2 {
		return nil, core.ErrInsufficientData
	}
	// Sort events by timestamp
	sortedEvents := make([]domain.Event, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})
	var relations []core.TemporalRelation
	// Analyze pairwise temporal relationships
	for i := 0; i < len(sortedEvents); i++ {
		for j := i + 1; j < len(sortedEvents); j++ {
			relation := t.analyzeEventPair(sortedEvents[i], sortedEvents[j])
			if relation.Confidence >= t.config.MinConfidence {
				relations = append(relations, relation)
			}
		}
	}
	return relations, nil
}
// FindCausalChains identifies potential causal chains in events
func (t *temporalAnalyzer) FindCausalChains(events []domain.Event) ([]core.CausalChain, error) {
	if len(events) < 2 {
		return nil, core.ErrInsufficientData
	}
	// First analyze temporal relationships
	relations, err := t.AnalyzeSequence(events)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze sequence: %w", err)
	}
	// Group related events into chains
	chains := t.buildChainsFromRelations(events, relations)
	// Filter chains by confidence and length
	var validChains []core.CausalChain
	for _, chain := range chains {
		if chain.Confidence >= t.config.MinConfidence && len(chain.Events) >= 2 {
			validChains = append(validChains, chain)
		}
	}
	return validChains, nil
}
// ComputeTemporalDistance calculates the temporal distance between two events
func (t *temporalAnalyzer) ComputeTemporalDistance(event1, event2 domain.Event) time.Duration {
	if event1.Timestamp.Before(event2.Timestamp) {
		return event2.Timestamp.Sub(event1.Timestamp)
	}
	return event1.Timestamp.Sub(event2.Timestamp)
}
// GroupByTimeWindows groups events into time windows
func (t *temporalAnalyzer) GroupByTimeWindows(events []domain.Event, windowSize time.Duration) ([][]domain.Event, error) {
	if len(events) == 0 {
		return nil, nil
	}
	// Sort events by timestamp
	sortedEvents := make([]domain.Event, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})
	var windows [][]domain.Event
	var currentWindow []domain.Event
	var windowStart time.Time
	for _, event := range sortedEvents {
		// Start new window if needed
		if len(currentWindow) == 0 {
			windowStart = event.Timestamp
			currentWindow = []domain.Event{event}
			continue
		}
		// Check if event fits in current window
		if event.Timestamp.Sub(windowStart) <= windowSize {
			currentWindow = append(currentWindow, event)
		} else {
			// Close current window and start new one
			windows = append(windows, currentWindow)
			windowStart = event.Timestamp
			currentWindow = []domain.Event{event}
		}
	}
	// Add last window if not empty
	if len(currentWindow) > 0 {
		windows = append(windows, currentWindow)
	}
	return windows, nil
}
// FindCoOccurringEvents finds events that occur close in time
func (t *temporalAnalyzer) FindCoOccurringEvents(events []domain.Event, maxGap time.Duration) ([][]domain.Event, error) {
	if len(events) < 2 {
		return nil, nil
	}
	// Sort events by timestamp
	sortedEvents := make([]domain.Event, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})
	var groups [][]domain.Event
	var currentGroup []domain.Event
	for i, event := range sortedEvents {
		if i == 0 {
			currentGroup = []domain.Event{event}
			continue
		}
		// Check if event co-occurs with the last event in current group
		lastEvent := currentGroup[len(currentGroup)-1]
		gap := event.Timestamp.Sub(lastEvent.Timestamp)
		if gap <= maxGap {
			currentGroup = append(currentGroup, event)
		} else {
			// Close current group and start new one
			if len(currentGroup) > 1 {
				groups = append(groups, currentGroup)
			}
			currentGroup = []domain.Event{event}
		}
	}
	// Add last group if it has multiple events
	if len(currentGroup) > 1 {
		groups = append(groups, currentGroup)
	}
	return groups, nil
}
// Helper methods
// analyzeEventPair analyzes the temporal relationship between two events
func (t *temporalAnalyzer) analyzeEventPair(event1, event2 domain.Event) core.TemporalRelation {
	timeDiff := event2.Timestamp.Sub(event1.Timestamp)
	relation := core.TemporalRelation{
		EventA:         event1.ID,
		EventB:         event2.ID,
		TimeDifference: timeDiff,
	}
	// Determine temporal relationship type
	if timeDiff.Abs() <= time.Second {
		relation.Relation = core.TemporalTypeConcurrent
		relation.Confidence = 0.9
	} else if timeDiff > 0 {
		relation.Relation = core.TemporalTypeBefore
		relation.Confidence = t.computeTemporalConfidence(timeDiff)
	} else {
		relation.Relation = core.TemporalTypeAfter
		relation.Confidence = t.computeTemporalConfidence(-timeDiff)
	}
	// Boost confidence for events from related sources
	if t.areSourcesRelated(event1.Source, event2.Source) {
		relation.Confidence = math.Min(1.0, relation.Confidence*1.2)
	}
	// Boost confidence for events with similar context
	if t.haveSimilarContext(event1, event2) {
		relation.Confidence = math.Min(1.0, relation.Confidence*1.1)
	}
	return relation
}
// computeTemporalConfidence computes confidence based on temporal distance
func (t *temporalAnalyzer) computeTemporalConfidence(timeDiff time.Duration) float64 {
	// Closer events in time have higher confidence
	maxTime := t.config.TimeWindow
	if timeDiff > maxTime {
		return 0.0
	}
	// Exponential decay function
	decay := float64(timeDiff) / float64(maxTime)
	return math.Exp(-decay * 3) // 3 is the decay constant
}
// areSourcesRelated checks if two sources are related
func (t *temporalAnalyzer) areSourcesRelated(source1, source2 domain.Source) bool {
	// Define source relationships
	relatedSources := map[domain.Source][]domain.Source{
		domain.SourceEBPF:     {domain.SourceKubernetes, domain.SourceSystemd},
		domain.SourceKubernetes: {domain.SourceEBPF, domain.SourceJournald, domain.SourceSystemd},
		domain.SourceSystemd:  {domain.SourceEBPF, domain.SourceJournald, domain.SourceKubernetes},
		domain.SourceJournald: {domain.SourceKubernetes, domain.SourceSystemd},
	}
	if related, exists := relatedSources[source1]; exists {
		for _, relatedSource := range related {
			if relatedSource == source2 {
				return true
			}
		}
	}
	return false
}
// haveSimilarContext checks if two events have similar context
func (t *temporalAnalyzer) haveSimilarContext(event1, event2 domain.Event) bool {
	// Check host similarity
	if event1.Context.Host != "" && event2.Context.Host != "" {
		if event1.Context.Host == event2.Context.Host {
			return true
		}
	}
	// Check container similarity
	if event1.Context.Container != "" && event2.Context.Container != "" {
		if event1.Context.Container == event2.Context.Container {
			return true
		}
	}
	// Check PID similarity (same process)
	if event1.Context.PID != nil && event2.Context.PID != nil {
		if *event1.Context.PID == *event2.Context.PID {
			return true
		}
	}
	// Check label overlap
	commonLabels := 0
	for key, value := range event1.Context.Labels {
		if event2Value, exists := event2.Context.Labels[key]; exists && event2Value == value {
			commonLabels++
		}
	}
	// If more than 50% of labels match, consider similar context
	totalLabels := len(event1.Context.Labels)
	if totalLabels > 0 && float64(commonLabels)/float64(totalLabels) > 0.5 {
		return true
	}
	return false
}
// buildChainsFromRelations builds causal chains from temporal relations
func (t *temporalAnalyzer) buildChainsFromRelations(events []domain.Event, relations []core.TemporalRelation) []core.CausalChain {
	// Create adjacency map for building chains
	adjacency := make(map[domain.EventID][]domain.EventID)
	for _, relation := range relations {
		if relation.Relation == core.TemporalTypeBefore {
			adjacency[relation.EventA] = append(adjacency[relation.EventA], relation.EventB)
		}
	}
	var chains []core.CausalChain
	visited := make(map[domain.EventID]bool)
	// Find chains starting from each unvisited event
	for _, event := range events {
		if !visited[event.ID] {
			chain := t.buildChainFromEvent(event.ID, adjacency, visited)
			if len(chain.Events) >= 2 {
				chain.ID = fmt.Sprintf("chain_%d", len(chains))
				chain.Confidence = t.computeChainConfidence(chain, relations)
				chain.Category = t.determineChainCategory(chain, events)
				chains = append(chains, chain)
			}
		}
	}
	return chains
}
// buildChainFromEvent builds a chain starting from a specific event
func (t *temporalAnalyzer) buildChainFromEvent(startEvent domain.EventID, adjacency map[domain.EventID][]domain.EventID, visited map[domain.EventID]bool) core.CausalChain {
	chain := core.CausalChain{
		Events: []domain.EventID{startEvent},
	}
	visited[startEvent] = true
	current := startEvent
	// Follow the chain
	for {
		next, hasNext := adjacency[current]
		if !hasNext || len(next) == 0 {
			break
		}
		// Choose the most likely next event (first unvisited one)
		var nextEvent domain.EventID
		found := false
		for _, candidate := range next {
			if !visited[candidate] {
				nextEvent = candidate
				found = true
				break
			}
		}
		if !found {
			break
		}
		chain.Events = append(chain.Events, nextEvent)
		visited[nextEvent] = true
		current = nextEvent
	}
	return chain
}
// computeChainConfidence computes confidence for a causal chain
func (t *temporalAnalyzer) computeChainConfidence(chain core.CausalChain, relations []core.TemporalRelation) float64 {
	if len(chain.Events) < 2 {
		return 0.0
	}
	// Find average confidence of relations in the chain
	var totalConfidence float64
	var relationCount int
	for i := 0; i < len(chain.Events)-1; i++ {
		eventA := chain.Events[i]
		eventB := chain.Events[i+1]
		// Find the relation between these events
		for _, relation := range relations {
			if relation.EventA == eventA && relation.EventB == eventB {
				totalConfidence += relation.Confidence
				relationCount++
				break
			}
		}
	}
	if relationCount == 0 {
		return 0.0
	}
	averageConfidence := totalConfidence / float64(relationCount)
	// Apply chain length penalty (longer chains are less reliable)
	lengthPenalty := 1.0 / math.Sqrt(float64(len(chain.Events)))
	return math.Min(1.0, averageConfidence*lengthPenalty)
}
// determineChainCategory determines the category of a causal chain
func (t *temporalAnalyzer) determineChainCategory(chain core.CausalChain, events []domain.Event) core.ChainCategory {
	// Create event lookup map
	eventMap := make(map[domain.EventID]domain.Event)
	for _, event := range events {
		eventMap[event.ID] = event
	}
	// Analyze event types and sources in the chain
	hasMemoryEvents := false
	hasNetworkEvents := false
	hasServiceEvents := false
	hasSystemEvents := false
	for _, eventID := range chain.Events {
		if event, exists := eventMap[eventID]; exists {
			switch event.Type {
			case domain.EventTypeMemory:
				hasMemoryEvents = true
			case domain.EventTypeNetwork:
				hasNetworkEvents = true
			case domain.EventTypeService:
				hasServiceEvents = true
			case domain.EventTypeSystem:
				hasSystemEvents = true
			}
			// Check severity for failure indication
			if event.Severity == domain.SeverityError || event.Severity == domain.SeverityCritical {
				return core.ChainCategoryFailure
			}
		}
	}
	// Determine category based on event types
	if hasMemoryEvents {
		return core.ChainCategoryResource
	}
	if hasNetworkEvents {
		return core.ChainCategoryNetwork
	}
	if hasServiceEvents {
		return core.ChainCategoryPerformance
	}
	if hasSystemEvents {
		return core.ChainCategoryFailure
	}
	return core.ChainCategoryPerformance
}