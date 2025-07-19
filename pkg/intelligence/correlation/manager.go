package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/patternrecognition"
)

// CollectionManager provides AI pattern recognition for collected events
type CollectionManager struct {
	// AI Pattern Recognition
	patternManager *patternrecognition.PatternManager

	// Event processing
	eventBus    chan domain.Event
	insightChan chan domain.Insight

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	config Config
}

// Config for the collection manager
type Config struct {
	EventBufferSize          int
	PatternDetectionInterval time.Duration
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		EventBufferSize:          1000,
		PatternDetectionInterval: 5 * time.Second,
	}
}

// NewCollectionManager creates a collection manager with AI pattern recognition
func NewCollectionManager(config Config) *CollectionManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &CollectionManager{
		patternManager: patternrecognition.NewPatternManager(),
		eventBus:       make(chan domain.Event, config.EventBufferSize),
		insightChan:    make(chan domain.Insight, 100),
		ctx:            ctx,
		cancel:         cancel,
		config:         config,
	}
}

// Start begins AI pattern recognition processing
func (cm *CollectionManager) Start() error {
	// Start pattern recognition
	if err := cm.patternManager.Start(cm.ctx); err != nil {
		return fmt.Errorf("failed to start pattern manager: %w", err)
	}

	// Start event processing goroutine
	cm.wg.Add(1)
	go cm.processEvents()

	return nil
}

// ProcessEvents processes a batch of events through AI pattern recognition
func (cm *CollectionManager) ProcessEvents(events []domain.Event) []domain.Insight {
	// Send events to processing pipeline
	for _, event := range events {
		select {
		case cm.eventBus <- event:
		case <-cm.ctx.Done():
			return nil
		}
	}

	// Collect any immediate insights
	var insights []domain.Insight
	timeout := time.After(100 * time.Millisecond)

	for {
		select {
		case insight := <-cm.insightChan:
			insights = append(insights, insight)
		case <-timeout:
			return insights
		case <-cm.ctx.Done():
			return insights
		}
	}
}

// GetInsights returns all available insights
func (cm *CollectionManager) GetInsights() []domain.Insight {
	var insights []domain.Insight

	// Non-blocking read of available insights
	for {
		select {
		case insight := <-cm.insightChan:
			insights = append(insights, insight)
		default:
			return insights
		}
	}
}

// processEvents runs continuous AI pattern recognition
func (cm *CollectionManager) processEvents() {
	defer cm.wg.Done()

	eventBuffer := make([]domain.Event, 0, 100)
	ticker := time.NewTicker(cm.config.PatternDetectionInterval)
	defer ticker.Stop()

	for {
		select {
		case event := <-cm.eventBus:
			eventBuffer = append(eventBuffer, event)

			// Process when buffer reaches threshold
			if len(eventBuffer) >= 10 {
				cm.analyzePatterns(eventBuffer)
				eventBuffer = eventBuffer[:0] // Reset buffer
			}

		case <-ticker.C:
			// Periodic pattern analysis
			if len(eventBuffer) > 0 {
				cm.analyzePatterns(eventBuffer)
				eventBuffer = eventBuffer[:0]
			}

		case <-cm.ctx.Done():
			return
		}
	}
}

// analyzePatterns runs AI pattern recognition on events
func (cm *CollectionManager) analyzePatterns(events []domain.Event) {
	matches := cm.patternManager.DetectPatterns(events)

	for _, match := range matches {
		insight := domain.Insight{
			ID:          fmt.Sprintf("ai-pattern-%d", time.Now().UnixNano()),
			Type:        "ai_pattern_detection",
			Title:       fmt.Sprintf("AI Pattern: %s", match.Pattern.Name()),
			Description: fmt.Sprintf("%s (Confidence: %.2f)", match.Description, match.Confidence),
			Confidence:  match.Confidence,
			Source:      "ai_pattern_recognition",
			Timestamp:   time.Now(),
			Metadata: map[string]interface{}{
				"pattern_type":     match.Pattern.Type(),
				"events_analyzed":  len(events),
				"recommendation":   match.Recommendation,
				"confidence_score": match.Confidence,
			},
		}

		select {
		case cm.insightChan <- insight:
		case <-cm.ctx.Done():
			return
		}
	}
}

// Insights returns the channel of AI-generated insights
func (cm *CollectionManager) Insights() <-chan domain.Insight {
	return cm.insightChan
}

// Stop gracefully shuts down pattern recognition
func (cm *CollectionManager) Stop() error {
	cm.cancel()
	cm.wg.Wait()
	close(cm.eventBus)
	close(cm.insightChan)
	return nil
}

// Statistics returns processing statistics
func (cm *CollectionManager) Statistics() map[string]interface{} {
	return map[string]interface{}{
		"event_buffer_size":     len(cm.eventBus),
		"insight_queue_size":    len(cm.insightChan),
		"pattern_manager_stats": cm.patternManager.Statistics(),
	}
}
