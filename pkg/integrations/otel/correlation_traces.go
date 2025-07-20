package telemetry

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// CorrelationTracer provides distributed tracing for correlation analysis
type CorrelationTracer struct {
	tracer     trace.Tracer
	exporter   *OpenTelemetryExporter
	translator *collector.SimplePIDTranslator
}

// NewCorrelationTracer creates a new correlation tracer
func NewCorrelationTracer(exporter *OpenTelemetryExporter) *CorrelationTracer {
	return &CorrelationTracer{
		tracer:     exporter.tracer,
		exporter:   exporter,
		translator: exporter.translator,
	}
}

// TraceCorrelationAnalysis creates a root span for correlation analysis with event timeline
func (ct *CorrelationTracer) TraceCorrelationAnalysis(
	ctx context.Context,
	correlationID string,
	events []correlation.Event,
) (context.Context, trace.Span) {

	// Create root span for the entire correlation analysis
	ctx, span := ct.tracer.Start(ctx, "tapio.correlation.analysis",
		trace.WithAttributes(
			attribute.String("correlation.id", correlationID),
			attribute.Int("correlation.event_count", len(events)),
			attribute.String("correlation.sources", ct.getEventSources(events)),
			attribute.String("correlation.timespan", ct.getTimespan(events)),
			attribute.String("analysis.type", "distributed_correlation"),
		),
	)

	// Add metadata about the analysis
	span.SetAttributes(
		attribute.String("tapio.component", "correlation_engine"),
		attribute.String("tapio.version", "1.0.0"),
		attribute.Bool("correlation.distributed", true),
	)

	// Add span events for each correlated event in chronological order
	for i, event := range events {
		span.AddEvent("correlation.event_detected", trace.WithAttributes(
			attribute.String("event.source", string(event.Source)),
			attribute.String("event.type", event.Type),
			attribute.Int64("event.timestamp", event.Timestamp.Unix()),
			attribute.Float64("event.confidence", event.Confidence),
			attribute.Int("event.sequence", i+1),
			attribute.String("event.description", event.Description),
		))
	}

	return ctx, span
}

// TraceLayerAnalysis creates spans for each analysis layer (eBPF, K8s, systemd, etc.)
func (ct *CorrelationTracer) TraceLayerAnalysis(
	ctx context.Context,
	layer string,
	targetResource string,
	analysisType string,
) (context.Context, trace.Span) {

	spanName := fmt.Sprintf("tapio.analysis.%s", layer)
	ctx, span := ct.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("analysis.layer", layer),
			attribute.String("analysis.target", targetResource),
			attribute.String("analysis.type", analysisType),
			attribute.String("tapio.component", "correlation_engine"),
		),
	)

	// Add layer-specific attributes
	switch layer {
	case "ebpf":
		span.SetAttributes(
			attribute.String("data.source", "kernel"),
			attribute.String("analysis.scope", "process_level"),
			attribute.Bool("real_time", true),
		)
	case "kubernetes":
		span.SetAttributes(
			attribute.String("data.source", "k8s_api"),
			attribute.String("analysis.scope", "cluster_level"),
			attribute.Bool("api_driven", true),
		)
	case "systemd":
		span.SetAttributes(
			attribute.String("data.source", "journald"),
			attribute.String("analysis.scope", "system_level"),
		)
	case "network":
		span.SetAttributes(
			attribute.String("data.source", "netstat"),
			attribute.String("analysis.scope", "network_level"),
		)
	}

	return ctx, span
}

// TraceRootCauseAnalysis creates trace chain for root cause determination
func (ct *CorrelationTracer) TraceRootCauseAnalysis(
	ctx context.Context,
	pattern string,
	confidence float64,
	findings []correlation.Finding,
) (context.Context, trace.Span) {

	ctx, span := ct.tracer.Start(ctx, "tapio.analysis.root_cause",
		trace.WithAttributes(
			attribute.String("rootcause.pattern", pattern),
			attribute.Float64("rootcause.confidence", confidence),
			attribute.String("rootcause.severity", ct.getSeverity(confidence)),
			attribute.Int("rootcause.findings_count", len(findings)),
		),
	)

	// Add prediction attributes if high confidence
	if confidence > 0.8 {
		span.SetAttributes(
			attribute.Bool("rootcause.actionable", true),
			attribute.String("rootcause.urgency", "high"),
		)
		span.SetStatus(codes.Ok, "High confidence root cause identified")
	} else {
		span.SetAttributes(
			attribute.Bool("rootcause.actionable", false),
			attribute.String("rootcause.urgency", "low"),
		)
		span.SetStatus(codes.Error, "Low confidence root cause analysis")
	}

	// Add findings as span events
	for i, finding := range findings {
		span.AddEvent("rootcause.finding", trace.WithAttributes(
			attribute.String("finding.type", finding.GetType()),
			attribute.Float64("finding.confidence", finding.Confidence),
			attribute.String("finding.description", finding.Description),
			attribute.Int("finding.sequence", i+1),
		))
	}

	return ctx, span
}

// TraceEventCausalChain creates connected spans showing event causation
func (ct *CorrelationTracer) TraceEventCausalChain(
	ctx context.Context,
	events []correlation.Event,
	causalRelationships []CausalLink,
) (context.Context, trace.Span) {

	// Root span for the entire causal chain
	ctx, rootSpan := ct.tracer.Start(ctx, "tapio.timeline.causal_chain",
		trace.WithAttributes(
			attribute.Int("timeline.event_count", len(events)),
			attribute.Int("timeline.causal_links", len(causalRelationships)),
			attribute.String("timeline.span", ct.getTimespan(events)),
		),
	)

	// Create child spans for each event in chronological order
	for i, event := range events {
		eventCtx, eventSpan := ct.tracer.Start(ctx,
			fmt.Sprintf("tapio.timeline.event_%d", i+1),
			trace.WithAttributes(
				attribute.String("event.source", string(event.Source)),
				attribute.String("event.type", event.Type),
				attribute.Int64("event.timestamp", event.Timestamp.Unix()),
				attribute.String("event.description", event.Description),
				attribute.Float64("event.confidence", event.Confidence),
				attribute.Int("event.position", i+1),
			),
		)

		// Add Kubernetes context if available through translator
		if event.PID != 0 && ct.translator != nil {
			if k8sContext, err := ct.translator.GetPodInfo(event.PID); err == nil {
				eventSpan.SetAttributes(
					attribute.String("k8s.pod.name", k8sContext.Pod),
					attribute.String("k8s.namespace", k8sContext.Namespace),
					attribute.String("k8s.container.name", k8sContext.Container),
					attribute.String("k8s.node.name", k8sContext.Node),
					attribute.Int64("process.pid", int64(k8sContext.PID)),
				)
			}
		}

		// Add causal links as span links
		for _, link := range causalRelationships {
			if link.FromEventIndex == i {
				// Link to the target event span
				spanContext := trace.SpanContextFromContext(eventCtx)
				eventSpan.AddLink(trace.Link{
					SpanContext: spanContext,
					Attributes: []attribute.KeyValue{
						attribute.String("relationship.type", link.RelationType),
						attribute.Float64("relationship.confidence", link.Confidence),
						attribute.String("relationship.description", link.Description),
					},
				})
			}
		}

		eventSpan.End()
	}

	return ctx, rootSpan
}

// TraceMultiLayerCorrelation creates distributed traces across system layers
func (ct *CorrelationTracer) TraceMultiLayerCorrelation(
	ctx context.Context,
	correlationID string,
	layers []LayerAnalysis,
) (context.Context, trace.Span) {

	ctx, rootSpan := ct.tracer.Start(ctx, "tapio.correlation.multi_layer",
		trace.WithAttributes(
			attribute.String("correlation.id", correlationID),
			attribute.Int("correlation.layers_count", len(layers)),
			attribute.StringSlice("correlation.layers", ct.getLayerNames(layers)),
		),
	)

	// Create child spans for each layer analysis
	for _, layer := range layers {
		_, layerSpan := ct.TraceLayerAnalysis(ctx,
			layer.Name, layer.Target, layer.AnalysisType)

		// Add layer-specific findings
		for _, finding := range layer.Findings {
			layerSpan.AddEvent("layer.finding", trace.WithAttributes(
				attribute.String("finding.type", finding.GetType()),
				attribute.Float64("finding.confidence", finding.Confidence),
				attribute.String("finding.impact", finding.GetImpact()),
			))
		}

		// Add performance metrics
		layerSpan.SetAttributes(
			attribute.Int64("layer.analysis_duration_ms", layer.Duration.Milliseconds()),
			attribute.Int("layer.data_points", layer.DataPoints),
			attribute.Float64("layer.accuracy", layer.Accuracy),
		)

		layerSpan.End()
	}

	return ctx, rootSpan
}

// TracePredictiveAnalysis creates spans for predictive correlation analysis
func (ct *CorrelationTracer) TracePredictiveAnalysis(
	ctx context.Context,
	prediction types.Prediction,
	historicalData []HistoricalEvent,
) (context.Context, trace.Span) {

	ctx, span := ct.tracer.Start(ctx, "tapio.analysis.predictive",
		trace.WithAttributes(
			attribute.String("prediction.reason", prediction.Reason),
			attribute.Float64("prediction.confidence", prediction.Confidence),
			attribute.Float64("prediction.time_to_failure", prediction.TimeToFailure.Seconds()),
			attribute.Int("prediction.historical_data_points", len(historicalData)),
		),
	)

	// Add prediction accuracy indicators
	if prediction.Confidence > 0.9 {
		span.SetAttributes(
			attribute.String("prediction.accuracy", "high"),
			attribute.Bool("prediction.actionable", true),
		)
		span.SetStatus(codes.Ok, "High confidence prediction")
	} else if prediction.Confidence > 0.7 {
		span.SetAttributes(
			attribute.String("prediction.accuracy", "medium"),
			attribute.Bool("prediction.actionable", true),
		)
	} else {
		span.SetAttributes(
			attribute.String("prediction.accuracy", "low"),
			attribute.Bool("prediction.actionable", false),
		)
	}

	// Add historical data points as events
	for i, historicalEvent := range historicalData {
		span.AddEvent("prediction.historical_data", trace.WithAttributes(
			attribute.Int64("historical.timestamp", historicalEvent.Timestamp.Unix()),
			attribute.String("historical.event_type", historicalEvent.EventType),
			attribute.Float64("historical.value", historicalEvent.Value),
			attribute.Int("historical.sequence", i+1),
		))
	}

	return ctx, span
}

// TracePerformanceMetrics records correlation analysis performance metrics
func (ct *CorrelationTracer) TracePerformanceMetrics(
	ctx context.Context,
	analysisLatency time.Duration,
	confidence float64,
	dataPoints int,
	memoryUsageMB float64,
) {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}

	// Record performance metrics as span attributes
	span.SetAttributes(
		attribute.Int64("performance.analysis_duration_ms", analysisLatency.Milliseconds()),
		attribute.Float64("performance.confidence_score", confidence),
		attribute.Int("performance.data_points_analyzed", dataPoints),
		attribute.Float64("performance.memory_usage_mb", memoryUsageMB),
		attribute.Float64("performance.throughput_events_per_second",
			float64(dataPoints)/analysisLatency.Seconds()),
	)

	// Add performance classification
	if analysisLatency.Milliseconds() < 100 {
		span.SetAttributes(attribute.String("performance.class", "fast"))
	} else if analysisLatency.Milliseconds() < 1000 {
		span.SetAttributes(attribute.String("performance.class", "normal"))
	} else {
		span.SetAttributes(attribute.String("performance.class", "slow"))
	}
}

// Helper data structures

// CausalLink represents a causal relationship between events
type CausalLink struct {
	FromEventIndex int
	ToEventIndex   int
	RelationType   string // "causes", "triggers", "precedes", "correlates"
	Confidence     float64
	Description    string
}

// LayerAnalysis represents analysis results from a specific system layer
type LayerAnalysis struct {
	Name         string
	Target       string
	AnalysisType string
	Findings     []correlation.Finding
	Duration     time.Duration
	DataPoints   int
	Accuracy     float64
}

// HistoricalEvent represents historical data used for predictions
type HistoricalEvent struct {
	Timestamp time.Time
	EventType string
	Value     float64
}

// Helper methods

func (ct *CorrelationTracer) getEventSources(events []correlation.Event) string {
	sources := make(map[string]bool)
	for _, event := range events {
		sources[string(event.Source)] = true
	}

	sourceList := make([]string, 0, len(sources))
	for source := range sources {
		sourceList = append(sourceList, source)
	}

	if len(sourceList) == 0 {
		return "none"
	}
	if len(sourceList) == 1 {
		return sourceList[0]
	}
	if len(sourceList) == 2 {
		return sourceList[0] + "," + sourceList[1]
	}
	return fmt.Sprintf("%s,+%d", sourceList[0], len(sourceList)-1)
}

func (ct *CorrelationTracer) getTimespan(events []correlation.Event) string {
	if len(events) == 0 {
		return "0s"
	}
	if len(events) == 1 {
		return "instant"
	}

	earliest := events[0].Timestamp
	latest := events[0].Timestamp

	for _, event := range events {
		if event.Timestamp.Before(earliest) {
			earliest = event.Timestamp
		}
		if event.Timestamp.After(latest) {
			latest = event.Timestamp
		}
	}

	duration := latest.Sub(earliest)
	if duration < time.Second {
		return fmt.Sprintf("%.0fms", duration.Seconds()*1000)
	}
	if duration < time.Minute {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	return fmt.Sprintf("%.1fm", duration.Minutes())
}

func (ct *CorrelationTracer) getSeverity(confidence float64) string {
	if confidence >= 0.9 {
		return "critical"
	}
	if confidence >= 0.7 {
		return "high"
	}
	if confidence >= 0.5 {
		return "medium"
	}
	return "low"
}

func (ct *CorrelationTracer) getLayerNames(layers []LayerAnalysis) []string {
	names := make([]string, len(layers))
	for i, layer := range layers {
		names[i] = layer.Name
	}
	return names
}

// Timeline Visualization Methods

// TraceTimelineVisualization creates a comprehensive timeline trace with visual markers
func (ct *CorrelationTracer) TraceTimelineVisualization(
	ctx context.Context,
	correlationID string,
	events []correlation.Event,
	timeWindow time.Duration,
) (context.Context, trace.Span) {

	ctx, span := ct.tracer.Start(ctx, "tapio.timeline.visualization",
		trace.WithAttributes(
			attribute.String("timeline.correlation_id", correlationID),
			attribute.Int("timeline.event_count", len(events)),
			attribute.Float64("timeline.window_seconds", timeWindow.Seconds()),
			attribute.String("timeline.type", "interactive_visualization"),
		),
	)

	// Calculate timeline statistics
	if len(events) > 0 {
		earliest, latest := ct.getTimelineBounds(events)
		span.SetAttributes(
			attribute.Int64("timeline.start", earliest.Unix()),
			attribute.Int64("timeline.end", latest.Unix()),
			attribute.Float64("timeline.span_seconds", latest.Sub(earliest).Seconds()),
		)

		// Add timeline density information
		eventDensity := float64(len(events)) / latest.Sub(earliest).Seconds()
		span.SetAttributes(
			attribute.Float64("timeline.event_density_per_second", eventDensity),
			attribute.String("timeline.density_class", ct.classifyDensity(eventDensity)),
		)
	}

	// Create timeline segments for better visualization
	segments := ct.createTimelineSegments(events, 5) // 5 segments
	for i, segment := range segments {
		span.AddEvent("timeline.segment", trace.WithAttributes(
			attribute.Int("segment.index", i),
			attribute.Int64("segment.start", segment.Start.Unix()),
			attribute.Int64("segment.end", segment.End.Unix()),
			attribute.Int("segment.event_count", segment.EventCount),
			attribute.String("segment.dominant_source", segment.DominantSource),
			attribute.Float64("segment.severity_score", segment.SeverityScore),
		))
	}

	return ctx, span
}

// TraceTimelineHeatmap creates a heatmap visualization trace for event density
func (ct *CorrelationTracer) TraceTimelineHeatmap(
	ctx context.Context,
	events []correlation.Event,
	bucketSize time.Duration,
) (context.Context, trace.Span) {

	ctx, span := ct.tracer.Start(ctx, "tapio.timeline.heatmap",
		trace.WithAttributes(
			attribute.Float64("heatmap.bucket_size_seconds", bucketSize.Seconds()),
			attribute.Int("heatmap.total_events", len(events)),
		),
	)

	// Create time buckets for heatmap
	buckets := ct.createTimeBuckets(events, bucketSize)

	// Find hotspots (high activity periods)
	hotspots := ct.identifyHotspots(buckets)
	span.SetAttributes(
		attribute.Int("heatmap.bucket_count", len(buckets)),
		attribute.Int("heatmap.hotspot_count", len(hotspots)),
	)

	// Add bucket data as events
	for _, bucket := range buckets {
		span.AddEvent("heatmap.bucket", trace.WithAttributes(
			attribute.Int64("bucket.time", bucket.Time.Unix()),
			attribute.Int("bucket.event_count", bucket.Count),
			attribute.Float64("bucket.intensity", bucket.Intensity),
			attribute.StringSlice("bucket.event_types", bucket.EventTypes),
			attribute.Bool("bucket.is_hotspot", bucket.IsHotspot),
		))
	}

	return ctx, span
}

// TraceEventFlow creates a flow visualization trace showing event progression
func (ct *CorrelationTracer) TraceEventFlow(
	ctx context.Context,
	events []correlation.Event,
	flowType string, // "sequential", "parallel", "branching"
) (context.Context, trace.Span) {

	ctx, span := ct.tracer.Start(ctx, "tapio.timeline.event_flow",
		trace.WithAttributes(
			attribute.String("flow.type", flowType),
			attribute.Int("flow.event_count", len(events)),
		),
	)

	// Analyze event flow patterns
	flowAnalysis := ct.analyzeEventFlow(events, flowType)
	span.SetAttributes(
		attribute.Int("flow.critical_path_length", flowAnalysis.CriticalPathLength),
		attribute.Int("flow.parallel_branches", flowAnalysis.ParallelBranches),
		attribute.Float64("flow.complexity_score", flowAnalysis.ComplexityScore),
	)

	// Trace flow paths
	for i, path := range flowAnalysis.Paths {
		_, pathSpan := ct.tracer.Start(ctx,
			fmt.Sprintf("tapio.timeline.flow_path_%d", i+1),
			trace.WithAttributes(
				attribute.Int("path.index", i),
				attribute.Int("path.length", len(path.Events)),
				attribute.String("path.type", path.Type),
				attribute.Float64("path.confidence", path.Confidence),
			),
		)

		// Add events in the path
		for j, event := range path.Events {
			pathSpan.AddEvent("path.event", trace.WithAttributes(
				attribute.Int("event.sequence", j+1),
				attribute.String("event.type", event.Type),
				attribute.Int64("event.timestamp", event.Timestamp.Unix()),
			))
		}

		pathSpan.End()
	}

	return ctx, span
}

// Helper structures for timeline visualization

type TimelineSegment struct {
	Start          time.Time
	End            time.Time
	EventCount     int
	DominantSource string
	SeverityScore  float64
}

type TimeBucket struct {
	Time       time.Time
	Count      int
	Intensity  float64
	EventTypes []string
	IsHotspot  bool
}

type FlowAnalysis struct {
	CriticalPathLength int
	ParallelBranches   int
	ComplexityScore    float64
	Paths              []FlowPath
}

type FlowPath struct {
	Events     []correlation.Event
	Type       string // "critical", "secondary", "error"
	Confidence float64
}

// Helper methods for timeline visualization

func (ct *CorrelationTracer) getTimelineBounds(events []correlation.Event) (time.Time, time.Time) {
	if len(events) == 0 {
		now := time.Now()
		return now, now
	}

	earliest := events[0].Timestamp
	latest := events[0].Timestamp

	for _, event := range events {
		if event.Timestamp.Before(earliest) {
			earliest = event.Timestamp
		}
		if event.Timestamp.After(latest) {
			latest = event.Timestamp
		}
	}

	return earliest, latest
}

func (ct *CorrelationTracer) classifyDensity(density float64) string {
	switch {
	case density < 0.1:
		return "sparse"
	case density < 1.0:
		return "normal"
	case density < 10.0:
		return "dense"
	default:
		return "critical"
	}
}

func (ct *CorrelationTracer) createTimelineSegments(events []correlation.Event, segmentCount int) []TimelineSegment {
	if len(events) == 0 || segmentCount <= 0 {
		return []TimelineSegment{}
	}

	earliest, latest := ct.getTimelineBounds(events)
	segmentDuration := latest.Sub(earliest) / time.Duration(segmentCount)

	segments := make([]TimelineSegment, segmentCount)
	for i := 0; i < segmentCount; i++ {
		segments[i] = TimelineSegment{
			Start: earliest.Add(time.Duration(i) * segmentDuration),
			End:   earliest.Add(time.Duration(i+1) * segmentDuration),
		}
	}

	// Populate segment data
	for _, event := range events {
		for i := range segments {
			if event.Timestamp.After(segments[i].Start) && event.Timestamp.Before(segments[i].End) {
				segments[i].EventCount++
				segments[i].SeverityScore += event.Confidence
			}
		}
	}

	return segments
}

func (ct *CorrelationTracer) createTimeBuckets(events []correlation.Event, bucketSize time.Duration) []TimeBucket {
	if len(events) == 0 {
		return []TimeBucket{}
	}

	earliest, latest := ct.getTimelineBounds(events)
	bucketCount := int(latest.Sub(earliest)/bucketSize) + 1

	buckets := make([]TimeBucket, bucketCount)
	for i := 0; i < bucketCount; i++ {
		buckets[i] = TimeBucket{
			Time:       earliest.Add(time.Duration(i) * bucketSize),
			EventTypes: make([]string, 0),
		}
	}

	// Populate buckets
	maxCount := 0
	for _, event := range events {
		bucketIndex := int(event.Timestamp.Sub(earliest) / bucketSize)
		if bucketIndex >= 0 && bucketIndex < len(buckets) {
			buckets[bucketIndex].Count++
			buckets[bucketIndex].EventTypes = append(buckets[bucketIndex].EventTypes, event.Type)
			if buckets[bucketIndex].Count > maxCount {
				maxCount = buckets[bucketIndex].Count
			}
		}
	}

	// Calculate intensity
	for i := range buckets {
		if maxCount > 0 {
			buckets[i].Intensity = float64(buckets[i].Count) / float64(maxCount)
		}
	}

	return buckets
}

func (ct *CorrelationTracer) identifyHotspots(buckets []TimeBucket) []TimeBucket {
	hotspots := make([]TimeBucket, 0)

	// Calculate average intensity
	totalIntensity := 0.0
	for _, bucket := range buckets {
		totalIntensity += bucket.Intensity
	}
	avgIntensity := totalIntensity / float64(len(buckets))

	// Identify hotspots (2x average intensity)
	for i := range buckets {
		if buckets[i].Intensity > avgIntensity*2 {
			buckets[i].IsHotspot = true
			hotspots = append(hotspots, buckets[i])
		}
	}

	return hotspots
}

func (ct *CorrelationTracer) analyzeEventFlow(events []correlation.Event, flowType string) FlowAnalysis {
	analysis := FlowAnalysis{
		Paths: make([]FlowPath, 0),
	}

	// Sort events by timestamp
	sortedEvents := make([]correlation.Event, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})

	switch flowType {
	case "sequential":
		// All events form a single critical path
		analysis.CriticalPathLength = len(sortedEvents)
		analysis.ParallelBranches = 1
		analysis.ComplexityScore = 1.0
		analysis.Paths = append(analysis.Paths, FlowPath{
			Events:     sortedEvents,
			Type:       "critical",
			Confidence: 1.0,
		})

	case "parallel":
		// Group events by source as parallel branches
		sourceGroups := make(map[correlation.SourceType][]correlation.Event)
		for _, event := range sortedEvents {
			sourceGroups[event.Source] = append(sourceGroups[event.Source], event)
		}

		analysis.ParallelBranches = len(sourceGroups)
		for source, events := range sourceGroups {
			analysis.Paths = append(analysis.Paths, FlowPath{
				Events:     events,
				Type:       string(source),
				Confidence: 0.8,
			})
			if len(events) > analysis.CriticalPathLength {
				analysis.CriticalPathLength = len(events)
			}
		}
		analysis.ComplexityScore = float64(analysis.ParallelBranches) / 10.0

	case "branching":
		// Identify branching based on event relationships
		// This is a simplified version - real implementation would use graph analysis
		analysis.CriticalPathLength = len(sortedEvents) / 2
		analysis.ParallelBranches = 3
		analysis.ComplexityScore = 0.7
	}

	return analysis
}

// Enhanced Root Cause Analysis Methods

// TraceRootCauseChain creates a detailed root cause analysis chain with confidence scoring
func (ct *CorrelationTracer) TraceRootCauseChain(
	ctx context.Context,
	findings []correlation.Finding,
	events []correlation.Event,
) (context.Context, trace.Span) {

	ctx, span := ct.tracer.Start(ctx, "tapio.rootcause.chain_analysis",
		trace.WithAttributes(
			attribute.Int("rootcause.finding_count", len(findings)),
			attribute.Int("rootcause.event_count", len(events)),
			attribute.String("rootcause.analysis_type", "chain_analysis"),
		),
	)

	// Build causality graph
	causalityGraph := ct.buildCausalityGraph(findings, events)
	span.SetAttributes(
		attribute.Int("rootcause.graph_nodes", causalityGraph.NodeCount),
		attribute.Int("rootcause.graph_edges", causalityGraph.EdgeCount),
		attribute.Int("rootcause.root_candidates", len(causalityGraph.RootCandidates)),
	)

	// Trace each root cause candidate
	for i, candidate := range causalityGraph.RootCandidates {
		_, candidateSpan := ct.tracer.Start(ctx,
			fmt.Sprintf("tapio.rootcause.candidate_%d", i+1),
			trace.WithAttributes(
				attribute.String("candidate.type", candidate.Type),
				attribute.Float64("candidate.confidence", candidate.Confidence),
				attribute.Int("candidate.impact_radius", candidate.ImpactRadius),
				attribute.String("candidate.severity", candidate.Severity),
			),
		)

		// Trace impact chain from this candidate
		for j, impact := range candidate.ImpactChain {
			candidateSpan.AddEvent("impact.event", trace.WithAttributes(
				attribute.Int("impact.sequence", j+1),
				attribute.String("impact.type", impact.Type),
				attribute.Float64("impact.probability", impact.Probability),
				attribute.String("impact.description", impact.Description),
			))
		}

		candidateSpan.End()
	}

	// Identify primary root cause
	if primaryRoot := causalityGraph.GetPrimaryRootCause(); primaryRoot != nil {
		span.SetAttributes(
			attribute.String("rootcause.primary_type", primaryRoot.Type),
			attribute.Float64("rootcause.primary_confidence", primaryRoot.Confidence),
			attribute.String("rootcause.primary_recommendation", primaryRoot.Recommendation),
		)
		span.SetStatus(codes.Ok, "Primary root cause identified")
	} else {
		span.SetStatus(codes.Error, "Unable to determine primary root cause")
	}

	return ctx, span
}

// TraceRootCausePropagation traces how root causes propagate through the system
func (ct *CorrelationTracer) TraceRootCausePropagation(
	ctx context.Context,
	rootCause RootCauseCandidate,
	systemState map[string]interface{},
) (context.Context, trace.Span) {

	ctx, span := ct.tracer.Start(ctx, "tapio.rootcause.propagation",
		trace.WithAttributes(
			attribute.String("propagation.root_type", rootCause.Type),
			attribute.Float64("propagation.initial_confidence", rootCause.Confidence),
			attribute.Int("propagation.system_components", len(systemState)),
		),
	)

	// Simulate propagation through system layers
	propagationSteps := ct.simulatePropagation(rootCause, systemState)
	span.SetAttributes(
		attribute.Int("propagation.steps", len(propagationSteps)),
		attribute.Float64("propagation.total_impact", propagationSteps[len(propagationSteps)-1].CumulativeImpact),
	)

	// Trace each propagation step
	for i, step := range propagationSteps {
		span.AddEvent("propagation.step", trace.WithAttributes(
			attribute.Int("step.sequence", i+1),
			attribute.String("step.component", step.Component),
			attribute.String("step.impact_type", step.ImpactType),
			attribute.Float64("step.impact_magnitude", step.ImpactMagnitude),
			attribute.Float64("step.cumulative_impact", step.CumulativeImpact),
			attribute.Float64("step.time_delay_seconds", step.TimeDelay.Seconds()),
		))
	}

	return ctx, span
}

// Helper structures for enhanced root cause analysis

type CausalityGraph struct {
	NodeCount      int
	EdgeCount      int
	RootCandidates []RootCauseCandidate
}

type RootCauseCandidate struct {
	Type           string
	Confidence     float64
	ImpactRadius   int
	Severity       string
	ImpactChain    []ImpactEvent
	Recommendation string
}

type ImpactEvent struct {
	Type        string
	Probability float64
	Description string
}

type PropagationStep struct {
	Component        string
	ImpactType       string
	ImpactMagnitude  float64
	CumulativeImpact float64
	TimeDelay        time.Duration
}

// Helper methods for root cause analysis

func (ct *CorrelationTracer) buildCausalityGraph(findings []correlation.Finding, events []correlation.Event) CausalityGraph {
	graph := CausalityGraph{
		RootCandidates: make([]RootCauseCandidate, 0),
	}

	// Group findings by type and confidence
	typeGroups := make(map[string][]correlation.Finding)
	for _, finding := range findings {
		findingType := finding.GetType()
		typeGroups[findingType] = append(typeGroups[findingType], finding)
	}

	graph.NodeCount = len(typeGroups)
	graph.EdgeCount = 0

	// Analyze each type group for root cause patterns
	for findingType, groupFindings := range typeGroups {
		avgConfidence := 0.0
		for _, f := range groupFindings {
			avgConfidence += f.Confidence
		}
		avgConfidence /= float64(len(groupFindings))

		// High confidence findings are root cause candidates
		if avgConfidence > 0.7 {
			candidate := RootCauseCandidate{
				Type:         findingType,
				Confidence:   avgConfidence,
				ImpactRadius: len(groupFindings),
				Severity:     ct.calculateSeverity(avgConfidence, len(groupFindings)),
				ImpactChain:  ct.buildImpactChain(findingType, findings),
			}
			candidate.Recommendation = ct.generateRecommendation(candidate)
			graph.RootCandidates = append(graph.RootCandidates, candidate)
		}

		// Count edges (simplified - based on temporal proximity)
		graph.EdgeCount += len(groupFindings) - 1
	}

	// Sort candidates by confidence
	sort.Slice(graph.RootCandidates, func(i, j int) bool {
		return graph.RootCandidates[i].Confidence > graph.RootCandidates[j].Confidence
	})

	return graph
}

func (cg *CausalityGraph) GetPrimaryRootCause() *RootCauseCandidate {
	if len(cg.RootCandidates) > 0 {
		return &cg.RootCandidates[0]
	}
	return nil
}

func (ct *CorrelationTracer) calculateSeverity(confidence float64, impactCount int) string {
	score := confidence * float64(impactCount)
	switch {
	case score > 5.0:
		return "critical"
	case score > 3.0:
		return "high"
	case score > 1.5:
		return "medium"
	default:
		return "low"
	}
}

func (ct *CorrelationTracer) buildImpactChain(rootType string, findings []correlation.Finding) []ImpactEvent {
	chain := make([]ImpactEvent, 0)

	// Simplified impact chain based on known patterns
	impactPatterns := map[string][]ImpactEvent{
		"memory_pressure": {
			{Type: "performance_degradation", Probability: 0.9, Description: "Application slowdown due to memory constraints"},
			{Type: "oom_kill", Probability: 0.7, Description: "Out of memory killer may terminate processes"},
			{Type: "pod_eviction", Probability: 0.6, Description: "Kubernetes may evict pods to reclaim memory"},
		},
		"cpu_throttling": {
			{Type: "latency_increase", Probability: 0.95, Description: "Request latency increases due to CPU limits"},
			{Type: "timeout_errors", Probability: 0.6, Description: "Downstream timeouts from slow processing"},
			{Type: "cascading_failure", Probability: 0.4, Description: "Dependent services may fail"},
		},
	}

	if impacts, exists := impactPatterns[rootType]; exists {
		chain = impacts
	} else {
		// Generic impact chain
		chain = append(chain, ImpactEvent{
			Type:        "service_degradation",
			Probability: 0.7,
			Description: fmt.Sprintf("Service degradation from %s", rootType),
		})
	}

	return chain
}

func (ct *CorrelationTracer) generateRecommendation(candidate RootCauseCandidate) string {
	recommendations := map[string]string{
		"memory_pressure":     "Increase memory limits or optimize application memory usage",
		"cpu_throttling":      "Increase CPU limits or optimize CPU-intensive operations",
		"network_error":       "Check network connectivity and service endpoints",
		"disk_pressure":       "Free up disk space or increase volume size",
		"pod_crash_loop":      "Check application logs and fix startup issues",
		"service_unavailable": "Verify service health and dependencies",
	}

	if rec, exists := recommendations[candidate.Type]; exists {
		return rec
	}
	return fmt.Sprintf("Investigate and resolve %s issues", candidate.Type)
}

func (ct *CorrelationTracer) simulatePropagation(rootCause RootCauseCandidate, systemState map[string]interface{}) []PropagationStep {
	steps := make([]PropagationStep, 0)
	cumulativeImpact := 0.0

	// Simplified propagation simulation
	propagationPath := []struct {
		component string
		delay     time.Duration
		impact    float64
	}{
		{"application_layer", 0, 0.3},
		{"service_mesh", 100 * time.Millisecond, 0.2},
		{"load_balancer", 500 * time.Millisecond, 0.15},
		{"dependent_services", 1 * time.Second, 0.25},
		{"user_experience", 2 * time.Second, 0.1},
	}

	for _, path := range propagationPath {
		cumulativeImpact += path.impact
		steps = append(steps, PropagationStep{
			Component:        path.component,
			ImpactType:       "performance_degradation",
			ImpactMagnitude:  path.impact,
			CumulativeImpact: cumulativeImpact,
			TimeDelay:        path.delay,
		})
	}

	return steps
}
