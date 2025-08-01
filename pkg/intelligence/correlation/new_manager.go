package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

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

// CollectionManager provides correlation using the simple system
// This replaces the old "AI-powered" semantic engine with something that actually works
type CollectionManager struct {
	// New simple correlation system (replaces the fake AI one)
	correlationSystem *SimpleCorrelationSystem

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

// NewCollectionManager creates a manager with the simple correlation system
// Pass nil for k8sClient to disable K8s correlation
func NewCollectionManager(config Config, logger *zap.Logger, k8sClient kubernetes.Interface) *CollectionManager {
	ctx, cancel := context.WithCancel(context.Background())

	// Create simple correlation system with default config
	simpleConfig := DefaultSimpleSystemConfig()
	simpleConfig.EventBufferSize = config.EventBufferSize

	return &CollectionManager{
		correlationSystem: NewSimpleCorrelationSystem(logger, simpleConfig, k8sClient),
		eventBus:          make(chan domain.Event, config.EventBufferSize),
		insightChan:       make(chan domain.Insight, 100),
		ctx:               ctx,
		cancel:            cancel,
		config:            config,
	}
}

// Start begins correlation processing with the new simple system
func (cm *CollectionManager) Start() error {
	// Start the simple correlation system
	if err := cm.correlationSystem.Start(); err != nil {
		return fmt.Errorf("failed to start simple correlation system: %w", err)
	}

	// Start event processing
	cm.wg.Add(1)
	go cm.processEvents()

	// Forward insights from correlation system
	cm.wg.Add(1)
	go cm.forwardInsights()

	return nil
}

// ProcessEvents processes events through the new simple correlation system
func (cm *CollectionManager) ProcessEvents(events []domain.Event) []domain.Insight {
	ctx := context.Background()

	// Send events to the simple correlation system
	for _, event := range events {
		// Convert domain.Event to UnifiedEvent
		unifiedEvent := cm.convertToUnifiedEvent(&event)

		// Process through simple correlation system
		if err := cm.correlationSystem.ProcessEvent(ctx, unifiedEvent); err != nil {
			// Log error but continue processing
			continue
		}

		// Also send to event bus for batch processing
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

// convertToUnifiedEvent converts domain.Event to UnifiedEvent
func (cm *CollectionManager) convertToUnifiedEvent(event *domain.Event) *domain.UnifiedEvent {
	ue := &domain.UnifiedEvent{
		ID:        string(event.ID),
		Timestamp: event.Timestamp,
		Type:      domain.EventType(event.Type),
		Source:    string(event.Source),
	}

	// Map severity
	switch event.Severity {
	case domain.EventSeverityCritical:
		ue.Severity = domain.EventSeverityCritical
	case domain.EventSeverityHigh:
		ue.Severity = domain.EventSeverityHigh
	case domain.EventSeverityMedium:
		ue.Severity = domain.EventSeverityMedium
	case domain.EventSeverityLow:
		ue.Severity = domain.EventSeverityLow
	default:
		ue.Severity = domain.EventSeverityInfo
	}

	// Extract context information
	if event.Context.Host != "" {
		ue.Entity = &domain.EntityContext{
			Name: event.Context.Host,
		}
	}

	// Extract payload-specific data
	if genericPayload, ok := event.Payload.(domain.GenericEventPayload); ok {
		if data, exists := genericPayload.Data["kubernetes"]; exists {
			if k8sData, ok := data.(map[string]interface{}); ok {
				ue.Kubernetes = &domain.KubernetesData{
					Object: fmt.Sprintf("%v", k8sData["object"]),
					Reason: fmt.Sprintf("%v", k8sData["reason"]),
				}
			}
		}

		if data, exists := genericPayload.Data["network"]; exists {
			if netData, ok := data.(map[string]interface{}); ok {
				ue.Network = &domain.NetworkData{}
				if destIP, ok := netData["dest_ip"].(string); ok {
					ue.Network.DestIP = destIP
				}
				if destPort, ok := netData["dest_port"].(int); ok {
					ue.Network.DestPort = uint16(destPort)
				}
			}
		}
	}

	return ue
}

// processEvents handles batch event processing
func (cm *CollectionManager) processEvents() {
	defer cm.wg.Done()

	eventBuffer := make([]domain.Event, 0, 100)
	ticker := time.NewTicker(cm.config.PatternDetectionInterval)
	defer ticker.Stop()

	for {
		select {
		case event := <-cm.eventBus:
			eventBuffer = append(eventBuffer, event)

			// Process batch when threshold reached
			if len(eventBuffer) >= 10 {
				cm.processBatch(eventBuffer)
				eventBuffer = eventBuffer[:0]
			}

		case <-ticker.C:
			// Periodic batch processing
			if len(eventBuffer) > 0 {
				cm.processBatch(eventBuffer)
				eventBuffer = eventBuffer[:0]
			}

		case <-cm.ctx.Done():
			return
		}
	}
}

// processBatch processes a batch of events
func (cm *CollectionManager) processBatch(events []domain.Event) {
	// The simple correlation system already processes individual events
	// This could be used for additional batch analysis if needed

	// For now, individual event processing in ProcessEvents is sufficient
	// since our simple correlators (K8s, temporal, sequence) work per-event
}

// forwardInsights forwards insights from correlation system
func (cm *CollectionManager) forwardInsights() {
	defer cm.wg.Done()

	for {
		select {
		case insight := <-cm.correlationSystem.Insights():
			// Forward to our insight channel
			select {
			case cm.insightChan <- insight:
			case <-cm.ctx.Done():
				return
			}
		case <-cm.ctx.Done():
			return
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

// Insights returns the channel of insights
func (cm *CollectionManager) Insights() <-chan domain.Insight {
	return cm.insightChan
}

// Stop gracefully shuts down the manager
func (cm *CollectionManager) Stop() error {
	// Stop the simple correlation system
	if err := cm.correlationSystem.Stop(); err != nil {
		return fmt.Errorf("failed to stop correlation system: %w", err)
	}

	// Stop our routines
	cm.cancel()
	cm.wg.Wait()

	// Close channels
	close(cm.eventBus)
	close(cm.insightChan)

	return nil
}

// Statistics returns processing statistics
func (cm *CollectionManager) Statistics() map[string]interface{} {
	stats := map[string]interface{}{
		"event_buffer_size":  len(cm.eventBus),
		"insight_queue_size": len(cm.insightChan),
	}

	// Add correlation system stats
	if cm.correlationSystem != nil {
		stats["correlation_system_stats"] = cm.correlationSystem.GetStats()
	}

	return stats
}
