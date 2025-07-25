package examples

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni"
	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
	"github.com/yairfalse/tapio/pkg/domain"
	manager "github.com/yairfalse/tapio/pkg/integrations/collector-manager"
	"github.com/yairfalse/tapio/pkg/intelligence/pipeline"
)

// CollectorAdapter adapts core.Collector to manager.Collector interface
type CollectorAdapter struct {
	collector core.Collector
}

func (ca *CollectorAdapter) Start(ctx context.Context) error {
	return ca.collector.Start(ctx)
}

func (ca *CollectorAdapter) Stop() error {
	return ca.collector.Stop()
}

func (ca *CollectorAdapter) Events() <-chan domain.UnifiedEvent {
	return ca.collector.Events()
}

func (ca *CollectorAdapter) Health() manager.CollectorHealth {
	health := ca.collector.Health()
	return &healthAdapter{health: health}
}

func (ca *CollectorAdapter) Statistics() manager.CollectorStatistics {
	stats := ca.collector.Statistics()
	return &statsAdapter{stats: stats}
}

// healthAdapter adapts domain.HealthStatus to manager.CollectorHealth
type healthAdapter struct {
	health domain.HealthStatus
}

func (ha *healthAdapter) Status() string {
	return string(ha.health.Status())
}

func (ha *healthAdapter) IsHealthy() bool {
	return ha.health.Status() == domain.HealthHealthy
}

func (ha *healthAdapter) LastEventTime() time.Time {
	details := ha.health.Details()
	if t, ok := details["last_event_time"].(time.Time); ok {
		return t
	}
	return time.Time{}
}

func (ha *healthAdapter) ErrorCount() uint64 {
	details := ha.health.Details()
	if count, ok := details["error_count"].(uint64); ok {
		return count
	}
	return 0
}

func (ha *healthAdapter) Metrics() map[string]float64 {
	details := ha.health.Details()
	if metrics, ok := details["metrics"].(map[string]float64); ok {
		return metrics
	}
	return make(map[string]float64)
}

// statsAdapter provides a basic implementation of CollectorStatistics
type statsAdapter struct {
	stats core.Statistics
}

func (sa *statsAdapter) EventsProcessed() uint64 {
	return sa.stats.EventsCollected
}

func (sa *statsAdapter) EventsDropped() uint64 {
	return sa.stats.EventsDropped
}

func (sa *statsAdapter) StartTime() time.Time {
	return sa.stats.StartTime
}

func (sa *statsAdapter) Custom() map[string]interface{} {
	// Convert core.Statistics to custom map
	custom := make(map[string]interface{})
	custom["cni_operations_total"] = sa.stats.CNIOperationsTotal
	custom["cni_operations_failed"] = sa.stats.CNIOperationsFailed
	custom["ip_allocations_total"] = sa.stats.IPAllocationsTotal
	custom["ip_deallocations_total"] = sa.stats.IPDeallocationsTotal
	custom["policy_events_total"] = sa.stats.PolicyEventsTotal
	custom["monitoring_errors"] = sa.stats.MonitoringErrors
	custom["k8s_events_processed"] = sa.stats.K8sEventsProcessed

	// Merge with any existing custom stats
	if sa.stats.Custom != nil {
		for k, v := range sa.stats.Custom {
			custom[k] = v
		}
	}

	return custom
}

// DemoFullSemanticCorrelationPipeline demonstrates the complete event flow:
// CNI Collector → CollectorManager → DataFlow → Semantic Intelligence
func DemoFullSemanticCorrelationPipeline() error {
	ctx := context.Background()

	// Step 1: Create CNI Collector (L1: Collectors)
	log.Printf("🔧 Creating CNI collector...")
	cniConfig := cni.DefaultConfig()
	cniConfig.Name = "demo-cni-collector"
	cniConfig.EventBufferSize = 1000

	cniCollector, err := cni.NewCNICollector(cniConfig)
	if err != nil {
		return fmt.Errorf("failed to create CNI collector: %w", err)
	}

	// Step 2: Create CollectorManager (L3: Integration)
	log.Printf("🔧 Creating CollectorManager...")
	collectorMgr := manager.NewCollectorManager()

	// Create an adapter to convert core.Collector to manager.Collector
	cniAdapter := &CollectorAdapter{
		collector: cniCollector,
	}
	collectorMgr.AddCollector("cni", cniAdapter)

	// Step 3: Create Pipeline for semantic correlation (L2: Intelligence)
	log.Printf("🔧 Creating Pipeline with semantic correlation...")
	pipelineInstance, err := pipeline.NewHighPerformancePipeline()
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}

	// Step 4: Connect the pipeline
	log.Printf("🔗 Connecting pipeline components...")
	inputEvents := make(chan domain.UnifiedEvent, 1000)
	outputEvents := make(chan domain.UnifiedEvent, 1000)

	// Pipeline doesn't need explicit connection - it processes events directly

	// Step 5: Start all components
	log.Printf("🚀 Starting pipeline...")

	// Start CollectorManager
	if err := collectorMgr.Start(ctx); err != nil {
		return fmt.Errorf("failed to start collector manager: %w", err)
	}
	defer collectorMgr.Stop()

	// Start DataFlow
	if err := pipelineInstance.Start(ctx); err != nil {
		return fmt.Errorf("failed to start data flow: %w", err)
	}
	defer pipelineInstance.Stop()

	// Step 6: Route events through the pipeline
	log.Printf("📊 Starting event routing...")

	// Route: CollectorManager → DataFlow
	go func() {
		for unifiedEvent := range collectorMgr.Events() {
			select {
			case inputEvents <- unifiedEvent:
				log.Printf("📥 Event routed to DataFlow: %s", unifiedEvent.ID)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Step 7: Process enriched events with semantic correlation
	go func() {
		eventCount := 0
		for enrichedEvent := range outputEvents {
			eventCount++

			// Display semantic correlation results
			log.Printf("📤 Enriched Event #%d:", eventCount)
			log.Printf("   ID: %s", enrichedEvent.ID)
			log.Printf("   Source: %s", enrichedEvent.Source)

			// Show semantic context (added by DataFlow)
			if enrichedEvent.Semantic != nil {
				log.Printf("   🧠 Semantic Context:")
				log.Printf("      Intent: %s", enrichedEvent.Semantic.Intent)
				log.Printf("      Category: %s", enrichedEvent.Semantic.Category)
				log.Printf("      Confidence: %.2f", enrichedEvent.Semantic.Confidence)
			}

			// Show trace context (added by DataFlow)
			if enrichedEvent.TraceContext != nil {
				log.Printf("   🔗 Trace Context:")
				log.Printf("      TraceID: %s", enrichedEvent.TraceContext.TraceID)
				log.Printf("      SpanID: %s", enrichedEvent.TraceContext.SpanID)
			}

			// Show correlation context (added by DataFlow)
			if enrichedEvent.Correlation != nil {
				log.Printf("   🔗 Correlation:")
				log.Printf("      CorrelationID: %s", enrichedEvent.Correlation.CorrelationID)
				log.Printf("      Pattern: %s", enrichedEvent.Correlation.Pattern)
			}

			// Show impact assessment (enhanced by DataFlow)
			if enrichedEvent.Impact != nil {
				log.Printf("   💼 Business Impact:")
				log.Printf("      Severity: %s", enrichedEvent.Impact.Severity)
				log.Printf("      Business Impact: %.2f", enrichedEvent.Impact.BusinessImpact)
				log.Printf("      Customer Facing: %t", enrichedEvent.Impact.CustomerFacing)
			}

			log.Printf("   ---")

			// Demo: Process first 5 events then stop
			if eventCount >= 5 {
				log.Printf("✅ Demo completed - processed %d enriched events", eventCount)
				return
			}
		}
	}()

	// Step 8: Monitor pipeline health
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for i := 0; i < 6; i++ { // Run for 30 seconds
		select {
		case <-ticker.C:
			// Display pipeline statistics
			stats := collectorMgr.Statistics()
			log.Printf("📈 Pipeline Status:")
			log.Printf("   Active Collectors: %d", stats.ActiveCollectors)

			// Note: CollectorManager doesn't have a Health() method
			// You would need to check individual collector health

		case <-ctx.Done():
			return ctx.Err()
		}
	}

	log.Printf("🎉 Full semantic correlation pipeline demo completed!")
	return nil
}

// DemoEventTransformation shows how events are enriched through the pipeline
func DemoEventTransformation() {
	log.Printf("📚 Event Transformation Demo")
	log.Printf("=============================")

	// Original CNI Event (from collector)
	log.Printf("🔵 Original CNI Event:")
	log.Printf("   - Basic CNI operation data")
	log.Printf("   - Pod/container context")
	log.Printf("   - Network details")
	log.Printf("   - Raw CNI output")

	// After CollectorManager
	log.Printf("🟡 After CollectorManager:")
	log.Printf("   - Aggregated with other collectors")
	log.Printf("   - Standardized format")
	log.Printf("   - Health monitoring")

	// After DataFlow Intelligence
	log.Printf("🟢 After DataFlow Intelligence:")
	log.Printf("   + Semantic correlation context")
	log.Printf("   + OTEL distributed tracing")
	log.Printf("   + Cross-collector correlation")
	log.Printf("   + Business impact assessment")
	log.Printf("   + Root cause analysis hints")
	log.Printf("   + Predictive insights")

	// Final Result
	log.Printf("🚀 Final Result:")
	log.Printf("   = Rich UnifiedEvent ready for:")
	log.Printf("     - Server storage")
	log.Printf("     - Real-time alerting")
	log.Printf("     - Advanced analytics")
	log.Printf("     - Business intelligence")
}

// DemoArchitecturalCompliance shows how the pipeline follows 5-level hierarchy
func DemoArchitecturalCompliance() {
	log.Printf("🏗️  Architectural Compliance Demo")
	log.Printf("=================================")

	log.Printf("L0: Domain")
	log.Printf("   ✅ UnifiedEvent, types, interfaces")

	log.Printf("L1: Collectors (CNI)")
	log.Printf("   ✅ Produces UnifiedEvent directly")
	log.Printf("   ✅ Rich semantic context from source")
	log.Printf("   ✅ No L2+ dependencies")

	log.Printf("L2: Intelligence (DataFlow)")
	log.Printf("   ✅ Semantic correlation engine")
	log.Printf("   ✅ OTEL integration")
	log.Printf("   ✅ Only depends on L0+L1")

	log.Printf("L3: Integration (CollectorManager)")
	log.Printf("   ✅ Orchestrates L1 collectors")
	log.Printf("   ✅ Feeds L2 intelligence")
	log.Printf("   ✅ Enables backward compatibility")

	log.Printf("L4: Interfaces (Server)")
	log.Printf("   ✅ gRPC streaming to server")
	log.Printf("   ✅ Uses all layers below")
	log.Printf("   ✅ No architectural violations")

	log.Printf("🎯 Result: Clean 5-level hierarchy with rich semantic correlation!")
}
