package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ObservabilityServer implements the ObservabilityService
type ObservabilityServer struct {
	pb.UnimplementedObservabilityServiceServer

	// Core dependencies
	logger       *zap.Logger
	tracer       trace.Tracer
	metricStore  MetricStore
	traceStore   TraceStore
	logStore     LogStore
	profileStore ProfileStore

	// Data management
	aggregator    *MetricAggregator
	traceAnalyzer *TraceAnalyzer
	profiler      *ProfileAnalyzer

	// Configuration
	config ObservabilityConfig

	// State tracking
	mu             sync.RWMutex
	activeQueries  map[string]*QueryContext
	streamRegistry map[string]*StreamContext
}

// ObservabilityConfig configures the observability service
type ObservabilityConfig struct {
	MaxMetricsPerQuery  int
	MaxTracesPerQuery   int
	MaxLogsPerQuery     int
	MaxProfilesPerQuery int
	DefaultResolution   time.Duration
	MaxQueryDuration    time.Duration
	StreamBufferSize    int
	EnableRealTimeQuery bool
	EnableProfiling     bool
	EnableTraceAnalysis bool
}

// QueryContext tracks active queries
type QueryContext struct {
	ID        string
	StartTime time.Time
	UserID    string
	Query     interface{}
	Cancel    context.CancelFunc
}

// StreamContext tracks active streams
type StreamContext struct {
	ID         string
	Type       string
	Filter     *pb.Filter
	StartTime  time.Time
	LastSent   time.Time
	EventsSent int64
}

// MetricStore interface for metric storage
type MetricStore interface {
	QueryMetrics(ctx context.Context, query *pb.MetricQuery) ([]*pb.Metric, error)
	StoreMetrics(ctx context.Context, metrics []*pb.Metric) error
	AggregateMetrics(ctx context.Context, query *pb.MetricQuery) ([]*pb.Metric, error)
	GetMetricSchema(ctx context.Context) (map[string]*pb.Metric, error)
}

// TraceStore interface for trace storage
type TraceStore interface {
	QueryTraces(ctx context.Context, query *pb.TraceQuery) ([]*pb.Trace, error)
	GetTrace(ctx context.Context, traceID string) (*pb.Trace, error)
	GetTraceTimeline(ctx context.Context, query *pb.TraceQuery) (*pb.TraceTimeline, error)
	StoreTrace(ctx context.Context, trace *pb.Trace) error
}

// LogStore interface for log storage
type LogStore interface {
	QueryLogs(ctx context.Context, filter *pb.Filter) ([]*pb.Log, error)
	StoreLogs(ctx context.Context, logs []*pb.Log) error
	StreamLogs(ctx context.Context, filter *pb.Filter, callback func(*pb.Log)) error
}

// ProfileStore interface for profile storage
type ProfileStore interface {
	QueryProfiles(ctx context.Context, filter *pb.Filter, profileType string) ([]*pb.Profile, error)
	StoreProfile(ctx context.Context, profile *pb.Profile) error
	AnalyzeProfile(ctx context.Context, profile *pb.Profile) (*pb.ProfileAnalysis, error)
}

// MetricAggregator performs metric aggregations
type MetricAggregator struct {
	mu                   sync.RWMutex
	aggregationFunctions map[pb.MetricQuery_AggregationType]func([]*pb.DataPoint) *pb.DataPoint
}

// TraceAnalyzer analyzes trace data
type TraceAnalyzer struct {
	mu           sync.RWMutex
	traceMatcher *TraceMatcher
}

// ProfileAnalyzer analyzes profile data
type ProfileAnalyzer struct {
	mu               sync.RWMutex
	enabledAnalyzers map[string]bool
}

// TraceMatcher matches traces with events and metrics
type TraceMatcher struct {
	// Implementation for correlating traces with other signals
}

// NewObservabilityServer creates a new observability server
func NewObservabilityServer(
	logger *zap.Logger,
	tracer trace.Tracer,
	metricStore MetricStore,
	traceStore TraceStore,
	logStore LogStore,
	profileStore ProfileStore,
) *ObservabilityServer {
	config := ObservabilityConfig{
		MaxMetricsPerQuery:  10000,
		MaxTracesPerQuery:   1000,
		MaxLogsPerQuery:     50000,
		MaxProfilesPerQuery: 100,
		DefaultResolution:   time.Minute,
		MaxQueryDuration:    5 * time.Minute,
		StreamBufferSize:    1000,
		EnableRealTimeQuery: true,
		EnableProfiling:     true,
		EnableTraceAnalysis: true,
	}

	return &ObservabilityServer{
		logger:         logger,
		tracer:         tracer,
		metricStore:    metricStore,
		traceStore:     traceStore,
		logStore:       logStore,
		profileStore:   profileStore,
		config:         config,
		activeQueries:  make(map[string]*QueryContext),
		streamRegistry: make(map[string]*StreamContext),
		aggregator:     NewMetricAggregator(),
		traceAnalyzer:  NewTraceAnalyzer(),
		profiler:       NewProfileAnalyzer(),
	}
}

// GetMetrics retrieves metrics based on query
func (s *ObservabilityServer) GetMetrics(ctx context.Context, req *pb.GetMetricsRequest) (*pb.GetMetricsResponse, error) {
	// Start tracing
	ctx, span := s.tracer.Start(ctx, "observability.get_metrics")
	defer span.End()

	// Validate request
	if req.Query == nil {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}

	// Apply query limits
	if req.Query.Filter != nil && req.Query.Filter.Limit > int32(s.config.MaxMetricsPerQuery) {
		req.Query.Filter.Limit = int32(s.config.MaxMetricsPerQuery)
	}

	// Query metrics
	metrics, err := s.metricStore.QueryMetrics(ctx, req.Query)
	if err != nil {
		s.logger.Error("Failed to query metrics", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to query metrics")
	}

	// Apply aggregation if requested
	if req.Query.Aggregation != pb.MetricQuery_AGGREGATION_TYPE_UNSPECIFIED {
		metrics, err = s.aggregator.AggregateMetrics(metrics, req.Query.Aggregation, req.Query.GroupBy)
		if err != nil {
			s.logger.Error("Failed to aggregate metrics", zap.Error(err))
			return nil, status.Error(codes.Internal, "failed to aggregate metrics")
		}
	}

	// Enrich with metadata
	metadata := map[string]string{
		"query_duration_ms": fmt.Sprintf("%.2f", time.Since(time.Now()).Seconds()*1000),
		"aggregation_type":  req.Query.Aggregation.String(),
		"result_count":      fmt.Sprintf("%d", len(metrics)),
	}

	response := &pb.GetMetricsResponse{
		Metrics:       metrics,
		TotalCount:    int64(len(metrics)),
		NextPageToken: "", // TODO: Implement pagination
		Metadata:      metadata,
	}

	s.logger.Debug("Retrieved metrics",
		zap.Int("count", len(metrics)),
		zap.String("aggregation", req.Query.Aggregation.String()),
	)

	return response, nil
}

// ExportMetrics accepts push-based metrics
func (s *ObservabilityServer) ExportMetrics(ctx context.Context, req *pb.ExportMetricsRequest) (*pb.ExportMetricsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "observability.export_metrics")
	defer span.End()

	// Validate request
	if len(req.Metrics) == 0 {
		return nil, status.Error(codes.InvalidArgument, "metrics are required")
	}

	// Process metrics
	accepted := 0
	rejected := 0
	var errors []*pb.Error

	for _, metric := range req.Metrics {
		// Validate metric
		if err := s.validateMetric(metric); err != nil {
			rejected++
			errors = append(errors, &pb.Error{
				Code:      codes.InvalidArgument.String(),
				Message:   err.Error(),
				Details:   map[string]string{"metric_id": metric.Id},
				Timestamp: timestamppb.Now(),
			})
			continue
		}

		// Enrich metric with collector context
		enrichedMetric := s.enrichMetric(metric, req.CollectorId)

		// Store metric
		if err := s.metricStore.StoreMetrics(ctx, []*pb.Metric{enrichedMetric}); err != nil {
			rejected++
			errors = append(errors, &pb.Error{
				Code:      codes.Internal.String(),
				Message:   fmt.Sprintf("storage error: %v", err),
				Details:   map[string]string{"metric_id": metric.Id},
				Timestamp: timestamppb.Now(),
			})
			continue
		}

		accepted++
	}

	s.logger.Info("Exported metrics",
		zap.String("collector_id", req.CollectorId),
		zap.Int("accepted", accepted),
		zap.Int("rejected", rejected),
	)

	return &pb.ExportMetricsResponse{
		AcceptedMetrics: int32(accepted),
		RejectedMetrics: int32(rejected),
		Errors:          errors,
	}, nil
}

// StreamMetrics provides real-time metric streaming
func (s *ObservabilityServer) StreamMetrics(req *pb.MetricQuery, stream pb.ObservabilityService_StreamMetricsServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "observability.stream_metrics")
	defer span.End()

	// Create stream context
	streamID := fmt.Sprintf("metrics_%d", time.Now().UnixNano())
	streamCtx := &StreamContext{
		ID:        streamID,
		Type:      "metrics",
		StartTime: time.Now(),
	}

	s.mu.Lock()
	s.streamRegistry[streamID] = streamCtx
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.streamRegistry, streamID)
		s.mu.Unlock()
	}()

	// Set up real-time metric subscription
	metricChan := make(chan *pb.Metric, s.config.StreamBufferSize)
	defer close(metricChan)

	// Start metric subscription
	go s.subscribeToMetrics(ctx, req, metricChan)

	// Stream metrics to client
	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("Metric stream cancelled", zap.String("stream_id", streamID))
			return nil
		case metric := <-metricChan:
			if metric == nil {
				continue
			}

			if err := stream.Send(metric); err != nil {
				s.logger.Error("Failed to send metric", zap.Error(err))
				return err
			}

			// Update stream stats
			s.mu.Lock()
			streamCtx.EventsSent++
			streamCtx.LastSent = time.Now()
			s.mu.Unlock()
		}
	}
}

// GetTraces retrieves traces based on query
func (s *ObservabilityServer) GetTraces(ctx context.Context, req *pb.GetTracesRequest) (*pb.GetTracesResponse, error) {
	ctx, span := s.tracer.Start(ctx, "observability.get_traces")
	defer span.End()

	var traces []*pb.Trace
	var err error

	// Handle specific trace IDs
	if len(req.TraceIds) > 0 {
		traces = make([]*pb.Trace, 0, len(req.TraceIds))
		for _, traceID := range req.TraceIds {
			trace, err := s.traceStore.GetTrace(ctx, traceID)
			if err != nil {
				s.logger.Warn("Failed to get trace", zap.String("trace_id", traceID), zap.Error(err))
				continue
			}
			if trace != nil {
				traces = append(traces, trace)
			}
		}
	} else if req.Query != nil {
		// Query-based retrieval
		traces, err = s.traceStore.QueryTraces(ctx, req.Query)
		if err != nil {
			s.logger.Error("Failed to query traces", zap.Error(err))
			return nil, status.Error(codes.Internal, "failed to query traces")
		}
	} else {
		return nil, status.Error(codes.InvalidArgument, "either trace_ids or query must be provided")
	}

	// Enrich traces with analysis if enabled
	if s.config.EnableTraceAnalysis {
		for _, trace := range traces {
			s.enrichTraceWithAnalysis(trace)
		}
	}

	metadata := map[string]string{
		"result_count":     fmt.Sprintf("%d", len(traces)),
		"analysis_enabled": fmt.Sprintf("%t", s.config.EnableTraceAnalysis),
	}

	return &pb.GetTracesResponse{
		Traces:        traces,
		TotalCount:    int64(len(traces)),
		NextPageToken: "", // TODO: Implement pagination
		Metadata:      metadata,
	}, nil
}

// GetTraceTimeline provides a timeline view of traces
func (s *ObservabilityServer) GetTraceTimeline(ctx context.Context, req *pb.GetTracesRequest) (*pb.TraceTimeline, error) {
	ctx, span := s.tracer.Start(ctx, "observability.get_trace_timeline")
	defer span.End()

	if req.Query == nil {
		return nil, status.Error(codes.InvalidArgument, "query is required for timeline")
	}

	timeline, err := s.traceStore.GetTraceTimeline(ctx, req.Query)
	if err != nil {
		s.logger.Error("Failed to get trace timeline", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to get trace timeline")
	}

	return timeline, nil
}

// GetLogs retrieves logs based on filter
func (s *ObservabilityServer) GetLogs(ctx context.Context, req *pb.GetLogsRequest) (*pb.GetLogsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "observability.get_logs")
	defer span.End()

	if req.Filter == nil {
		return nil, status.Error(codes.InvalidArgument, "filter is required")
	}

	// Apply query limits
	if req.Filter.Limit > int32(s.config.MaxLogsPerQuery) {
		req.Filter.Limit = int32(s.config.MaxLogsPerQuery)
	}

	logs, err := s.logStore.QueryLogs(ctx, req.Filter)
	if err != nil {
		s.logger.Error("Failed to query logs", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to query logs")
	}

	// Enrich with trace context if requested
	if req.IncludeTraceContext {
		s.enrichLogsWithTraceContext(logs)
	}

	return &pb.GetLogsResponse{
		Logs:          logs,
		TotalCount:    int64(len(logs)),
		NextPageToken: "", // TODO: Implement pagination
	}, nil
}

// StreamLogs provides real-time log streaming
func (s *ObservabilityServer) StreamLogs(filter *pb.Filter, stream pb.ObservabilityService_StreamLogsServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "observability.stream_logs")
	defer span.End()

	// Create stream context
	streamID := fmt.Sprintf("logs_%d", time.Now().UnixNano())
	streamCtx := &StreamContext{
		ID:        streamID,
		Type:      "logs",
		Filter:    filter,
		StartTime: time.Now(),
	}

	s.mu.Lock()
	s.streamRegistry[streamID] = streamCtx
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.streamRegistry, streamID)
		s.mu.Unlock()
	}()

	// Stream logs to client
	err := s.logStore.StreamLogs(ctx, filter, func(log *pb.Log) {
		if err := stream.Send(log); err != nil {
			s.logger.Error("Failed to send log", zap.Error(err))
			return
		}

		// Update stream stats
		s.mu.Lock()
		streamCtx.EventsSent++
		streamCtx.LastSent = time.Now()
		s.mu.Unlock()
	})

	if err != nil {
		s.logger.Error("Log streaming failed", zap.Error(err))
		return status.Error(codes.Internal, "log streaming failed")
	}

	return nil
}

// GetProfiles retrieves profiling data
func (s *ObservabilityServer) GetProfiles(ctx context.Context, req *pb.GetProfilesRequest) (*pb.GetProfilesResponse, error) {
	ctx, span := s.tracer.Start(ctx, "observability.get_profiles")
	defer span.End()

	if !s.config.EnableProfiling {
		return nil, status.Error(codes.Unimplemented, "profiling is disabled")
	}

	if req.Filter == nil {
		return nil, status.Error(codes.InvalidArgument, "filter is required")
	}

	// Apply query limits
	if req.Filter.Limit > int32(s.config.MaxProfilesPerQuery) {
		req.Filter.Limit = int32(s.config.MaxProfilesPerQuery)
	}

	profiles, err := s.profileStore.QueryProfiles(ctx, req.Filter, req.ProfileType)
	if err != nil {
		s.logger.Error("Failed to query profiles", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to query profiles")
	}

	// Include analysis if requested
	if req.IncludeAnalysis {
		for _, profile := range profiles {
			s.enrichProfileWithAnalysis(profile)
		}
	}

	return &pb.GetProfilesResponse{
		Profiles:      profiles,
		TotalCount:    int64(len(profiles)),
		NextPageToken: "", // TODO: Implement pagination
	}, nil
}

// AnalyzeProfile performs detailed profile analysis
func (s *ObservabilityServer) AnalyzeProfile(ctx context.Context, profile *pb.Profile) (*pb.ProfileAnalysis, error) {
	ctx, span := s.tracer.Start(ctx, "observability.analyze_profile")
	defer span.End()

	if !s.config.EnableProfiling {
		return nil, status.Error(codes.Unimplemented, "profiling is disabled")
	}

	analysis, err := s.profileStore.AnalyzeProfile(ctx, profile)
	if err != nil {
		s.logger.Error("Failed to analyze profile", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to analyze profile")
	}

	return analysis, nil
}

// Helper methods

func (s *ObservabilityServer) validateMetric(metric *pb.Metric) error {
	if metric.Id == "" {
		return fmt.Errorf("metric ID is required")
	}
	if metric.Name == "" {
		return fmt.Errorf("metric name is required")
	}
	if len(metric.DataPoints) == 0 {
		return fmt.Errorf("metric must have at least one data point")
	}
	return nil
}

func (s *ObservabilityServer) enrichMetric(metric *pb.Metric, collectorID string) *pb.Metric {
	// Add collector information
	if metric.Labels == nil {
		metric.Labels = make(map[string]string)
	}
	metric.Labels["collector_id"] = collectorID
	metric.Labels["ingested_at"] = time.Now().Format(time.RFC3339)

	// Add instrumentation scope if missing
	if metric.Scope == nil {
		metric.Scope = &pb.InstrumentationScope{
			Name:    "tapio.observability",
			Version: "v1.0.0",
		}
	}

	return metric
}

func (s *ObservabilityServer) subscribeToMetrics(ctx context.Context, query *pb.MetricQuery, metricChan chan<- *pb.Metric) {
	// Real-time metric subscription implementation
	// This would integrate with the metric store's real-time capabilities
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Query for new metrics
			metrics, err := s.metricStore.QueryMetrics(ctx, query)
			if err != nil {
				s.logger.Error("Failed to query metrics for stream", zap.Error(err))
				continue
			}

			// Send new metrics
			for _, metric := range metrics {
				select {
				case metricChan <- metric:
				case <-ctx.Done():
					return
				default:
					// Channel full, drop metric
					s.logger.Warn("Metric stream channel full, dropping metric")
				}
			}
		}
	}
}

func (s *ObservabilityServer) enrichTraceWithAnalysis(trace *pb.Trace) {
	// Add trace analysis metadata
	if trace == nil {
		return
	}

	// Calculate trace statistics
	errorCount := 0
	totalSpans := len(trace.Spans)

	for _, span := range trace.Spans {
		if span.Status != nil && span.Status.Code == pb.SpanStatus_STATUS_CODE_ERROR {
			errorCount++
		}
	}

	// Update trace status
	if trace.Status == nil {
		trace.Status = &pb.TraceStatus{}
	}
	trace.Status.ErrorCount = int32(errorCount)
	trace.Status.TotalSpans = int32(totalSpans)

	// Determine overall health
	if errorCount == 0 {
		trace.Status.Health = pb.TraceStatus_HEALTH_STATUS_HEALTHY
	} else if float64(errorCount)/float64(totalSpans) > 0.1 {
		trace.Status.Health = pb.TraceStatus_HEALTH_STATUS_ERROR
	} else {
		trace.Status.Health = pb.TraceStatus_HEALTH_STATUS_DEGRADED
	}
}

func (s *ObservabilityServer) enrichLogsWithTraceContext(logs []*pb.Log) {
	// Correlate logs with traces if trace context is available
	for _, log := range logs {
		if log.TraceId != "" {
			// Would fetch and attach trace context
			// This is a placeholder for actual trace correlation
		}
	}
}

func (s *ObservabilityServer) enrichProfileWithAnalysis(profile *pb.Profile) {
	// Add profile analysis
	if profile.Metadata == nil {
		profile.Metadata = &pb.ProfileMetadata{}
	}

	// Basic analysis (would be more sophisticated in production)
	profile.Metadata.SampleCount = int64(len(profile.PprofData))
	profile.Metadata.SampleUnit = "samples"
}

// Factory functions for dependencies

func NewMetricAggregator() *MetricAggregator {
	return &MetricAggregator{
		aggregationFunctions: make(map[pb.MetricQuery_AggregationType]func([]*pb.DataPoint) *pb.DataPoint),
	}
}

func (ma *MetricAggregator) AggregateMetrics(metrics []*pb.Metric, aggType pb.MetricQuery_AggregationType, groupBy []string) ([]*pb.Metric, error) {
	// Metric aggregation implementation
	// This would group metrics by the specified dimensions and apply aggregation functions
	return metrics, nil // Placeholder
}

func NewTraceAnalyzer() *TraceAnalyzer {
	return &TraceAnalyzer{
		traceMatcher: &TraceMatcher{},
	}
}

func NewProfileAnalyzer() *ProfileAnalyzer {
	return &ProfileAnalyzer{
		enabledAnalyzers: map[string]bool{
			"cpu":    true,
			"memory": true,
			"block":  true,
			"mutex":  true,
		},
	}
}
