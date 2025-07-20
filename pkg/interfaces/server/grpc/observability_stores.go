package grpc

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// InMemoryMetricStore implements MetricStore using in-memory storage
type InMemoryMetricStore struct {
	mu      sync.RWMutex
	metrics map[string]*pb.Metric
	logger  *zap.Logger

	// Time-based indexing for efficient queries
	timeIndex  map[int64][]string             // Unix timestamp bucket -> metric IDs
	nameIndex  map[string][]string            // Metric name -> metric IDs
	labelIndex map[string]map[string][]string // Label key -> label value -> metric IDs
}

// NewInMemoryMetricStore creates a new metric store
func NewInMemoryMetricStore(logger *zap.Logger) *InMemoryMetricStore {
	return &InMemoryMetricStore{
		metrics:    make(map[string]*pb.Metric),
		timeIndex:  make(map[int64][]string),
		nameIndex:  make(map[string][]string),
		labelIndex: make(map[string]map[string][]string),
		logger:     logger,
	}
}

func (s *InMemoryMetricStore) QueryMetrics(ctx context.Context, query *pb.MetricQuery) ([]*pb.Metric, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var candidates []string

	// Filter by metric names if specified
	if len(query.MetricNames) > 0 {
		candidateSet := make(map[string]bool)
		for _, name := range query.MetricNames {
			for _, id := range s.nameIndex[name] {
				candidateSet[id] = true
			}
		}
		for id := range candidateSet {
			candidates = append(candidates, id)
		}
	} else {
		// Get all metrics
		for id := range s.metrics {
			candidates = append(candidates, id)
		}
	}

	// Apply filter criteria
	var results []*pb.Metric
	for _, id := range candidates {
		metric := s.metrics[id]
		if metric != nil && s.matchesFilter(metric, query.Filter) {
			results = append(results, metric)
		}
	}

	return results, nil
}

func (s *InMemoryMetricStore) StoreMetrics(ctx context.Context, metrics []*pb.Metric) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, metric := range metrics {
		// Store metric
		s.metrics[metric.Id] = metric

		// Update name index
		s.nameIndex[metric.Name] = append(s.nameIndex[metric.Name], metric.Id)

		// Update label index
		for key, value := range metric.Labels {
			if s.labelIndex[key] == nil {
				s.labelIndex[key] = make(map[string][]string)
			}
			s.labelIndex[key][value] = append(s.labelIndex[key][value], metric.Id)
		}

		// Update time index for each data point
		for _, dp := range metric.DataPoints {
			timeBucket := dp.Timestamp.AsTime().Unix() / 60 // Minute buckets
			s.timeIndex[timeBucket] = append(s.timeIndex[timeBucket], metric.Id)
		}
	}

	return nil
}

func (s *InMemoryMetricStore) AggregateMetrics(ctx context.Context, query *pb.MetricQuery) ([]*pb.Metric, error) {
	// Get base metrics
	metrics, err := s.QueryMetrics(ctx, query)
	if err != nil {
		return nil, err
	}

	// Group by specified dimensions
	groups := s.groupMetrics(metrics, query.GroupBy)

	// Apply aggregation
	var aggregated []*pb.Metric
	for groupKey, groupMetrics := range groups {
		aggMetric := s.aggregateGroup(groupMetrics, query.Aggregation, groupKey)
		if aggMetric != nil {
			aggregated = append(aggregated, aggMetric)
		}
	}

	return aggregated, nil
}

func (s *InMemoryMetricStore) GetMetricSchema(ctx context.Context) (map[string]*pb.Metric, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	schema := make(map[string]*pb.Metric)
	for name := range s.nameIndex {
		// Get a representative metric for each name
		if ids := s.nameIndex[name]; len(ids) > 0 {
			if metric := s.metrics[ids[0]]; metric != nil {
				// Create schema entry (metric without data points)
				schemMetric := &pb.Metric{
					Name:        metric.Name,
					Description: metric.Description,
					Type:        metric.Type,
					Unit:        metric.Unit,
					Labels:      metric.Labels,
					Scope:       metric.Scope,
				}
				schema[name] = schemMetric
			}
		}
	}

	return schema, nil
}

// InMemoryTraceStore implements TraceStore using in-memory storage
type InMemoryTraceStore struct {
	mu     sync.RWMutex
	traces map[string]*pb.Trace
	logger *zap.Logger

	// Indexing
	serviceIndex  map[string][]string // Service name -> trace IDs
	durationIndex map[string][]string // Duration bucket -> trace IDs
}

func NewInMemoryTraceStore(logger *zap.Logger) *InMemoryTraceStore {
	return &InMemoryTraceStore{
		traces:        make(map[string]*pb.Trace),
		serviceIndex:  make(map[string][]string),
		durationIndex: make(map[string][]string),
		logger:        logger,
	}
}

func (s *InMemoryTraceStore) QueryTraces(ctx context.Context, query *pb.TraceQuery) ([]*pb.Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*pb.Trace
	for _, trace := range s.traces {
		if s.matchesTraceQuery(trace, query) {
			results = append(results, trace)
		}
	}

	// Sort by start time (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].StartTime.AsTime().After(results[j].StartTime.AsTime())
	})

	return results, nil
}

func (s *InMemoryTraceStore) GetTrace(ctx context.Context, traceID string) (*pb.Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	trace := s.traces[traceID]
	if trace == nil {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}

	return trace, nil
}

func (s *InMemoryTraceStore) GetTraceTimeline(ctx context.Context, query *pb.TraceQuery) (*pb.TraceTimeline, error) {
	traces, err := s.QueryTraces(ctx, query)
	if err != nil {
		return nil, err
	}

	var entries []*pb.TraceTimelineEntry
	services := make(map[string]*pb.ServiceInfo)
	var startTime, endTime time.Time

	for _, trace := range traces {
		entry := &pb.TraceTimelineEntry{
			TraceId:          trace.TraceId,
			StartTime:        trace.StartTime,
			Duration:         trace.Duration,
			Status:           trace.Status,
			InvolvedServices: []string{},
		}

		// Extract root operation and involved services
		for _, service := range trace.Services {
			entry.InvolvedServices = append(entry.InvolvedServices, service.Name)
			services[service.Name] = service
		}

		if len(trace.Spans) > 0 {
			entry.RootOperation = trace.Spans[0].OperationName
		}

		entries = append(entries, entry)

		// Update timeline bounds
		traceStart := trace.StartTime.AsTime()
		traceEnd := trace.EndTime.AsTime()
		if startTime.IsZero() || traceStart.Before(startTime) {
			startTime = traceStart
		}
		if endTime.IsZero() || traceEnd.After(endTime) {
			endTime = traceEnd
		}
	}

	return &pb.TraceTimeline{
		Entries:   entries,
		StartTime: timestamppb.New(startTime),
		EndTime:   timestamppb.New(endTime),
		Services:  services,
	}, nil
}

func (s *InMemoryTraceStore) StoreTrace(ctx context.Context, trace *pb.Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.traces[trace.TraceId] = trace

	// Update service index
	for _, service := range trace.Services {
		s.serviceIndex[service.Name] = append(s.serviceIndex[service.Name], trace.TraceId)
	}

	// Update duration index
	durationMs := trace.Duration.AsDuration().Milliseconds()
	var bucket string
	switch {
	case durationMs < 100:
		bucket = "fast"
	case durationMs < 1000:
		bucket = "medium"
	case durationMs < 5000:
		bucket = "slow"
	default:
		bucket = "very_slow"
	}
	s.durationIndex[bucket] = append(s.durationIndex[bucket], trace.TraceId)

	return nil
}

// InMemoryLogStore implements LogStore using in-memory storage
type InMemoryLogStore struct {
	mu     sync.RWMutex
	logs   []*pb.Log
	logger *zap.Logger

	// Indexing
	severityIndex map[pb.Log_LogSeverity][]int
	traceIndex    map[string][]int // Trace ID -> log indices
}

func NewInMemoryLogStore(logger *zap.Logger) *InMemoryLogStore {
	return &InMemoryLogStore{
		logs:          make([]*pb.Log, 0),
		severityIndex: make(map[pb.Log_LogSeverity][]int),
		traceIndex:    make(map[string][]int),
		logger:        logger,
	}
}

func (s *InMemoryLogStore) QueryLogs(ctx context.Context, filter *pb.Filter) ([]*pb.Log, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*pb.Log
	for i, log := range s.logs {
		if s.matchesLogFilter(log, filter) {
			results = append(results, s.logs[i])
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.AsTime().After(results[j].Timestamp.AsTime())
	})

	// Apply limit
	if filter.Limit > 0 && len(results) > int(filter.Limit) {
		results = results[:filter.Limit]
	}

	return results, nil
}

func (s *InMemoryLogStore) StoreLogs(ctx context.Context, logs []*pb.Log) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, log := range logs {
		index := len(s.logs)
		s.logs = append(s.logs, log)

		// Update severity index
		s.severityIndex[log.Severity] = append(s.severityIndex[log.Severity], index)

		// Update trace index
		if log.TraceId != "" {
			s.traceIndex[log.TraceId] = append(s.traceIndex[log.TraceId], index)
		}
	}

	return nil
}

func (s *InMemoryLogStore) StreamLogs(ctx context.Context, filter *pb.Filter, callback func(*pb.Log)) error {
	// For real-time streaming, we'd set up watchers
	// For now, return existing logs and simulate real-time updates

	logs, err := s.QueryLogs(ctx, filter)
	if err != nil {
		return err
	}

	// Send existing logs
	for _, log := range logs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			callback(log)
		}
	}

	// Simulate real-time updates (in production, this would be event-driven)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Check for new logs that match the filter
			newLogs, err := s.QueryLogs(ctx, filter)
			if err != nil {
				s.logger.Error("Failed to query logs for streaming", zap.Error(err))
				continue
			}

			// Send new logs (in production, we'd track which logs we've already sent)
			for _, log := range newLogs {
				callback(log)
			}
		}
	}
}

// InMemoryProfileStore implements ProfileStore using in-memory storage
type InMemoryProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]*pb.Profile
	logger   *zap.Logger

	// Indexing
	typeIndex map[string][]string // Profile type -> profile IDs
}

func NewInMemoryProfileStore(logger *zap.Logger) *InMemoryProfileStore {
	return &InMemoryProfileStore{
		profiles:  make(map[string]*pb.Profile),
		typeIndex: make(map[string][]string),
		logger:    logger,
	}
}

func (s *InMemoryProfileStore) QueryProfiles(ctx context.Context, filter *pb.Filter, profileType string) ([]*pb.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var candidates []string
	if profileType != "" {
		candidates = s.typeIndex[profileType]
	} else {
		for id := range s.profiles {
			candidates = append(candidates, id)
		}
	}

	var results []*pb.Profile
	for _, id := range candidates {
		profile := s.profiles[id]
		if profile != nil && s.matchesProfileFilter(profile, filter) {
			results = append(results, profile)
		}
	}

	// Sort by start time (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].StartTime.AsTime().After(results[j].StartTime.AsTime())
	})

	return results, nil
}

func (s *InMemoryProfileStore) StoreProfile(ctx context.Context, profile *pb.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.profiles[profile.Id] = profile
	s.typeIndex[profile.ProfileType] = append(s.typeIndex[profile.ProfileType], profile.Id)

	return nil
}

func (s *InMemoryProfileStore) AnalyzeProfile(ctx context.Context, profile *pb.Profile) (*pb.ProfileAnalysis, error) {
	// Basic profile analysis
	analysis := &pb.ProfileAnalysis{
		ProfileId:     profile.Id,
		HotSpots:      []*pb.ProfileFunction{},
		Bottlenecks:   []string{},
		Optimizations: []*pb.ProfileOptimization{},
		Statistics:    make(map[string]float64),
	}

	// Extract basic statistics from metadata
	if profile.Metadata != nil {
		analysis.Statistics["sample_count"] = float64(profile.Metadata.SampleCount)
		analysis.Statistics["total_value"] = profile.Metadata.TotalValue

		// Copy top functions as hot spots
		analysis.HotSpots = profile.Metadata.TopFunctions

		// Identify potential bottlenecks
		for _, fn := range profile.Metadata.TopFunctions {
			if fn.Percentage > 10.0 { // Functions taking >10% of time
				analysis.Bottlenecks = append(analysis.Bottlenecks,
					fmt.Sprintf("%s (%.1f%%)", fn.Name, fn.Percentage))
			}

			// Generate optimization suggestions
			if fn.Percentage > 20.0 {
				analysis.Optimizations = append(analysis.Optimizations, &pb.ProfileOptimization{
					Function:             fn.Name,
					Issue:                "High CPU usage",
					Suggestion:           "Consider optimization or caching",
					PotentialImprovement: fn.Percentage * 0.5, // Assume 50% improvement possible
					CodeLocation:         fmt.Sprintf("%s:%d", fn.File, fn.Line),
				})
			}
		}
	}

	return analysis, nil
}

// Helper methods for filtering

func (s *InMemoryMetricStore) matchesFilter(metric *pb.Metric, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	// Time range filtering
	if filter.TimeRange != nil {
		hasMatchingDataPoint := false
		for _, dp := range metric.DataPoints {
			ts := dp.Timestamp.AsTime()
			if (filter.TimeRange.Start == nil || ts.After(filter.TimeRange.Start.AsTime())) &&
				(filter.TimeRange.End == nil || ts.Before(filter.TimeRange.End.AsTime())) {
				hasMatchingDataPoint = true
				break
			}
		}
		if !hasMatchingDataPoint {
			return false
		}
	}

	// Label filtering using label selectors
	for _, selector := range filter.LabelSelectors {
		if !s.matchesLabelSelector(metric.Labels, selector) {
			return false
		}
	}

	return true
}

func (s *InMemoryMetricStore) matchesLabelSelector(labels map[string]string, selector *pb.Filter_LabelSelector) bool {
	key := selector.Key
	operator := selector.Operator
	values := selector.Values

	actualValue, exists := labels[key]

	switch operator {
	case "=", "==":
		return exists && len(values) > 0 && actualValue == values[0]
	case "!=":
		return !exists || len(values) == 0 || actualValue != values[0]
	case "in":
		if !exists {
			return false
		}
		for _, v := range values {
			if actualValue == v {
				return true
			}
		}
		return false
	case "notin":
		if !exists {
			return true
		}
		for _, v := range values {
			if actualValue == v {
				return false
			}
		}
		return true
	case "exists":
		return exists
	case "!exists":
		return !exists
	default:
		// Default to equality
		return exists && len(values) > 0 && actualValue == values[0]
	}
}

func (s *InMemoryTraceStore) matchesTraceQuery(trace *pb.Trace, query *pb.TraceQuery) bool {
	// Duration filtering
	if query.MinDuration != nil && trace.Duration.AsDuration() < query.MinDuration.AsDuration() {
		return false
	}
	if query.MaxDuration != nil && trace.Duration.AsDuration() > query.MaxDuration.AsDuration() {
		return false
	}

	// Error filtering
	if query.ErrorsOnly && (trace.Status == nil || trace.Status.ErrorCount == 0) {
		return false
	}

	// Service filtering
	if query.RootService != "" {
		found := false
		for _, service := range trace.Services {
			if service.Name == query.RootService {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Apply base filter
	return s.matchesBaseFilter(trace, query.Filter)
}

func (s *InMemoryTraceStore) matchesBaseFilter(trace *pb.Trace, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	// Time range filtering
	if filter.TimeRange != nil {
		ts := trace.StartTime.AsTime()
		if (filter.TimeRange.Start != nil && ts.Before(filter.TimeRange.Start.AsTime())) ||
			(filter.TimeRange.End != nil && ts.After(filter.TimeRange.End.AsTime())) {
			return false
		}
	}

	return true
}

func (s *InMemoryLogStore) matchesLogFilter(log *pb.Log, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	// Time range filtering
	if filter.TimeRange != nil {
		ts := log.Timestamp.AsTime()
		if (filter.TimeRange.Start != nil && ts.Before(filter.TimeRange.Start.AsTime())) ||
			(filter.TimeRange.End != nil && ts.After(filter.TimeRange.End.AsTime())) {
			return false
		}
	}

	// Severity filtering (map string to enum if needed)
	if len(filter.Severities) > 0 {
		severityMatch := false
		for _, severity := range filter.Severities {
			if pb.Log_LogSeverity(severity) == log.Severity {
				severityMatch = true
				break
			}
		}
		if !severityMatch {
			return false
		}
	}

	return true
}

func (s *InMemoryProfileStore) matchesProfileFilter(profile *pb.Profile, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	// Time range filtering
	if filter.TimeRange != nil {
		ts := profile.StartTime.AsTime()
		if (filter.TimeRange.Start != nil && ts.Before(filter.TimeRange.Start.AsTime())) ||
			(filter.TimeRange.End != nil && ts.After(filter.TimeRange.End.AsTime())) {
			return false
		}
	}

	return true
}

// Helper methods for aggregation

func (s *InMemoryMetricStore) groupMetrics(metrics []*pb.Metric, groupBy []string) map[string][]*pb.Metric {
	groups := make(map[string][]*pb.Metric)

	for _, metric := range metrics {
		key := s.buildGroupKey(metric, groupBy)
		groups[key] = append(groups[key], metric)
	}

	return groups
}

func (s *InMemoryMetricStore) buildGroupKey(metric *pb.Metric, groupBy []string) string {
	if len(groupBy) == 0 {
		return "all"
	}

	var keyParts []string
	for _, field := range groupBy {
		switch field {
		case "name":
			keyParts = append(keyParts, metric.Name)
		case "type":
			keyParts = append(keyParts, metric.Type.String())
		default:
			// Check if it's a label
			if value, exists := metric.Labels[field]; exists {
				keyParts = append(keyParts, value)
			} else {
				keyParts = append(keyParts, "")
			}
		}
	}

	return fmt.Sprintf("%v", keyParts)
}

func (s *InMemoryMetricStore) aggregateGroup(metrics []*pb.Metric, aggType pb.MetricQuery_AggregationType, groupKey string) *pb.Metric {
	if len(metrics) == 0 {
		return nil
	}

	// Use first metric as template
	result := &pb.Metric{
		Id:          fmt.Sprintf("agg_%s_%d", groupKey, time.Now().UnixNano()),
		Name:        metrics[0].Name,
		Description: fmt.Sprintf("Aggregated %s", metrics[0].Description),
		Type:        metrics[0].Type,
		Unit:        metrics[0].Unit,
		Labels:      make(map[string]string),
		DataPoints:  []*pb.DataPoint{},
	}

	// Copy common labels
	for key, value := range metrics[0].Labels {
		allHaveSameValue := true
		for _, metric := range metrics[1:] {
			if metric.Labels[key] != value {
				allHaveSameValue = false
				break
			}
		}
		if allHaveSameValue {
			result.Labels[key] = value
		}
	}

	// Aggregate data points based on type
	switch aggType {
	case pb.MetricQuery_AGGREGATION_TYPE_AVG:
		result.DataPoints = s.aggregateAverage(metrics)
	case pb.MetricQuery_AGGREGATION_TYPE_SUM:
		result.DataPoints = s.aggregateSum(metrics)
	case pb.MetricQuery_AGGREGATION_TYPE_MAX:
		result.DataPoints = s.aggregateMax(metrics)
	case pb.MetricQuery_AGGREGATION_TYPE_MIN:
		result.DataPoints = s.aggregateMin(metrics)
	case pb.MetricQuery_AGGREGATION_TYPE_COUNT:
		result.DataPoints = s.aggregateCount(metrics)
	default:
		// Return first metric's data points
		result.DataPoints = metrics[0].DataPoints
	}

	return result
}

func (s *InMemoryMetricStore) aggregateAverage(metrics []*pb.Metric) []*pb.DataPoint {
	// Simple average - in production would align time buckets
	if len(metrics) == 0 {
		return nil
	}

	var sum float64
	var count int
	for _, metric := range metrics {
		for _, dp := range metric.DataPoints {
			switch value := dp.Value.(type) {
			case *pb.DataPoint_GaugeValue:
				sum += value.GaugeValue
				count++
			case *pb.DataPoint_CounterValue:
				sum += float64(value.CounterValue)
				count++
			}
		}
	}

	if count == 0 {
		return nil
	}

	return []*pb.DataPoint{
		{
			Timestamp: timestamppb.Now(),
			Value:     &pb.DataPoint_GaugeValue{GaugeValue: sum / float64(count)},
		},
	}
}

func (s *InMemoryMetricStore) aggregateSum(metrics []*pb.Metric) []*pb.DataPoint {
	var sum float64
	for _, metric := range metrics {
		for _, dp := range metric.DataPoints {
			switch value := dp.Value.(type) {
			case *pb.DataPoint_GaugeValue:
				sum += value.GaugeValue
			case *pb.DataPoint_CounterValue:
				sum += float64(value.CounterValue)
			}
		}
	}

	return []*pb.DataPoint{
		{
			Timestamp: timestamppb.Now(),
			Value:     &pb.DataPoint_GaugeValue{GaugeValue: sum},
		},
	}
}

func (s *InMemoryMetricStore) aggregateMax(metrics []*pb.Metric) []*pb.DataPoint {
	var max float64
	hasValue := false

	for _, metric := range metrics {
		for _, dp := range metric.DataPoints {
			var value float64
			switch v := dp.Value.(type) {
			case *pb.DataPoint_GaugeValue:
				value = v.GaugeValue
			case *pb.DataPoint_CounterValue:
				value = float64(v.CounterValue)
			default:
				continue
			}

			if !hasValue || value > max {
				max = value
				hasValue = true
			}
		}
	}

	if !hasValue {
		return nil
	}

	return []*pb.DataPoint{
		{
			Timestamp: timestamppb.Now(),
			Value:     &pb.DataPoint_GaugeValue{GaugeValue: max},
		},
	}
}

func (s *InMemoryMetricStore) aggregateMin(metrics []*pb.Metric) []*pb.DataPoint {
	var min float64
	hasValue := false

	for _, metric := range metrics {
		for _, dp := range metric.DataPoints {
			var value float64
			switch v := dp.Value.(type) {
			case *pb.DataPoint_GaugeValue:
				value = v.GaugeValue
			case *pb.DataPoint_CounterValue:
				value = float64(v.CounterValue)
			default:
				continue
			}

			if !hasValue || value < min {
				min = value
				hasValue = true
			}
		}
	}

	if !hasValue {
		return nil
	}

	return []*pb.DataPoint{
		{
			Timestamp: timestamppb.Now(),
			Value:     &pb.DataPoint_GaugeValue{GaugeValue: min},
		},
	}
}

func (s *InMemoryMetricStore) aggregateCount(metrics []*pb.Metric) []*pb.DataPoint {
	var count int64
	for _, metric := range metrics {
		count += int64(len(metric.DataPoints))
	}

	return []*pb.DataPoint{
		{
			Timestamp: timestamppb.Now(),
			Value:     &pb.DataPoint_CounterValue{CounterValue: count},
		},
	}
}
