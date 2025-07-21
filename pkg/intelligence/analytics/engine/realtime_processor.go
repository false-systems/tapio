package engine

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// RealTimeProcessor handles real-time event processing
type RealTimeProcessor struct {
	maxEventsPerSecond int
	eventsProcessed    atomic.Uint64
	lastReset          time.Time
}

// NewRealTimeProcessor creates a new real-time processor
func NewRealTimeProcessor(maxEventsPerSecond int) *RealTimeProcessor {
	return &RealTimeProcessor{
		maxEventsPerSecond: maxEventsPerSecond,
		lastReset:          time.Now(),
	}
}

// Process applies real-time processing to an event
func (rtp *RealTimeProcessor) Process(ctx context.Context, event *domain.UnifiedEvent, result *AnalyticsResult) error {
	// Rate tracking
	rtp.eventsProcessed.Add(1)

	// Extract trace context if available
	if event.HasTraceContext() {
		result.Metadata["trace_id"] = event.TraceContext.TraceID
		result.Metadata["span_id"] = event.TraceContext.SpanID
		result.Metadata["sampled"] = event.TraceContext.Sampled
	}

	// Extract semantic information
	if event.Semantic != nil {
		result.Metadata["semantic_intent"] = event.Semantic.Intent
		result.Metadata["semantic_category"] = event.Semantic.Category
		result.Metadata["semantic_confidence"] = event.Semantic.Confidence

		// Boost confidence if we have high semantic confidence
		if event.Semantic.Confidence > 0.8 {
			result.ConfidenceScore *= 1.2
			if result.ConfidenceScore > 1.0 {
				result.ConfidenceScore = 1.0
			}
		}
	}

	// Extract entity information
	if event.Entity != nil {
		result.Metadata["entity_type"] = event.Entity.Type
		result.Metadata["entity_name"] = event.Entity.Name
		result.Metadata["entity_id"] = event.GetEntityID()
	}

	// Process correlation context
	if event.Correlation != nil {
		result.CorrelationID = event.Correlation.CorrelationID
		result.SemanticGroupID = event.Correlation.GroupID
		result.RelatedEvents = event.Correlation.RelatedEvents

		// Add causal chain
		if len(event.Correlation.CausalChain) > 0 {
			result.Metadata["causal_chain"] = event.Correlation.CausalChain
		}
	}

	// Layer-specific processing
	if event.IsKernelEvent() && event.Kernel != nil {
		result.Metadata["kernel_syscall"] = event.Kernel.Syscall
		result.Metadata["kernel_pid"] = event.Kernel.PID
		result.Metadata["kernel_comm"] = event.Kernel.Comm
	}

	if event.IsNetworkEvent() && event.Network != nil {
		result.Metadata["network_protocol"] = event.Network.Protocol
		result.Metadata["network_latency_ns"] = event.Network.Latency
		result.Metadata["network_status"] = event.Network.StatusCode
	}

	if event.IsApplicationEvent() && event.Application != nil {
		result.Metadata["app_level"] = event.Application.Level
		result.Metadata["app_logger"] = event.Application.Logger
		result.Metadata["app_request_id"] = event.Application.RequestID
	}

	return nil
}

// GetRate returns current events per second rate
func (rtp *RealTimeProcessor) GetRate() float64 {
	elapsed := time.Since(rtp.lastReset).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(rtp.eventsProcessed.Load()) / elapsed
}
