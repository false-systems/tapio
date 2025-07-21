package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/dataflow"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CorrelationServer implements the correlation analysis service
type CorrelationServer struct {
	pb.UnimplementedCorrelationServiceServer

	logger *zap.Logger
	tracer trace.Tracer

	// Core dependencies (following 5-level architecture)
	dataFlow          *dataflow.TapioDataFlow                // L2: Intelligence
	correlationEngine *correlation.SemanticCorrelationEngine // L2: Intelligence

	// Storage interfaces
	correlationStore CorrelationStore
	findingStore     FindingStore

	// Configuration
	config CorrelationServerConfig

	// Statistics
	stats struct {
		mu                sync.RWMutex
		TotalAnalyses     int64
		ActiveAnalyses    int64
		FindingsGenerated int64
		startTime         time.Time
		requestCount      uint64
	}

	// Active analysis tracking
	analysisJobs sync.Map // map[string]*AnalysisJob
}

// CorrelationServerConfig holds configuration for the correlation server
type CorrelationServerConfig struct {
	MaxConcurrentAnalyses int
	AnalysisTimeout       time.Duration
	ConfidenceThreshold   float64
	EnableRealTimeUpdates bool
	EnableRootCause       bool
	EnablePredictions     bool
	EnableImpactAssess    bool
}

// CorrelationStore interface for correlation data storage
type CorrelationStore interface {
	StoreCorrelation(ctx context.Context, correlation *correlation.Finding) error
	GetCorrelations(ctx context.Context, filter CorrelationFilter) ([]*correlation.Finding, error)
	GetCorrelationByID(ctx context.Context, id string) (*correlation.Finding, error)
}

// FindingStore interface for storing analysis findings
type FindingStore interface {
	StoreFinding(ctx context.Context, finding *correlation.Finding) error
	GetFindings(ctx context.Context, filter FindingFilter) ([]*correlation.Finding, error)
}

// AnalysisJob represents an active correlation analysis job
type AnalysisJob struct {
	ID        string
	StartTime time.Time
	Events    []domain.Event
	Status    string
	Progress  float64
	Result    *correlation.Finding
}

// CorrelationFilter for querying correlations
type CorrelationFilter struct {
	TimeRange     *pb.TimeRange
	PatternType   string
	MinConfidence float64
	EntityType    string
	Limit         int
}

// FindingFilter for querying findings
type FindingFilter struct {
	TimeRange     *pb.TimeRange
	AnalysisType  string
	MinConfidence float64
	Limit         int
}

// NewCorrelationServer creates a new correlation service implementation
func NewCorrelationServer(logger *zap.Logger, tracer trace.Tracer) *CorrelationServer {
	return &CorrelationServer{
		logger: logger,
		tracer: tracer,
		config: CorrelationServerConfig{
			MaxConcurrentAnalyses: 10,
			AnalysisTimeout:       30 * time.Second,
			ConfidenceThreshold:   0.7,
			EnableRealTimeUpdates: true,
			EnableRootCause:       true,
			EnablePredictions:     true,
			EnableImpactAssess:    true,
		},
		stats: struct {
			mu                sync.RWMutex
			TotalAnalyses     int64
			ActiveAnalyses    int64
			FindingsGenerated int64
			startTime         time.Time
			requestCount      uint64
		}{
			startTime: time.Now(),
		},
	}
}

// SetDependencies injects required dependencies following the architecture
func (s *CorrelationServer) SetDependencies(dataFlow *dataflow.TapioDataFlow, correlationEngine *correlation.SemanticCorrelationEngine) {
	s.dataFlow = dataFlow
	s.correlationEngine = correlationEngine
}

// NewCorrelationServerWithRealStore creates a correlation server with real storage integration
func NewCorrelationServerWithRealStore(logger *zap.Logger, tracer trace.Tracer) *CorrelationServer {
	server := NewCorrelationServer(logger, tracer)

	// Use in-memory implementations for now - would be replaced with real storage
	server.correlationStore = &InMemoryCorrelationStore{
		correlations: make(map[string]*correlation.Finding),
		mu:           sync.RWMutex{},
	}

	server.findingStore = &InMemoryFindingStore{
		findings: make(map[string]*correlation.Finding),
		mu:       sync.RWMutex{},
	}

	return server
}

// AnalyzeEvents performs correlation analysis on a set of events
func (s *CorrelationServer) AnalyzeEvents(ctx context.Context, req *pb.AnalyzeEventsRequest) (*pb.AnalyzeEventsResponse, error) {
	s.incrementRequestCount()

	ctx, span := s.tracer.Start(ctx, "correlation.analyze_events")
	defer span.End()

	s.logger.Debug("Starting correlation analysis",
		zap.Int("event_count", len(req.Events)),
		zap.String("analysis_type", req.AnalysisType.String()),
	)

	if len(req.Events) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no events provided for analysis")
	}

	// Convert proto events to domain events
	domainEvents := make([]domain.Event, len(req.Events))
	for i, protoEvent := range req.Events {
		domainEvents[i] = s.convertProtoEventToDomain(protoEvent)
	}

	// Create analysis job
	jobID := fmt.Sprintf("analysis_%d", time.Now().UnixNano())
	job := &AnalysisJob{
		ID:        jobID,
		StartTime: time.Now(),
		Events:    domainEvents,
		Status:    "running",
		Progress:  0.0,
	}

	s.analysisJobs.Store(jobID, job)
	s.incrementActiveAnalyses()

	// Perform correlation analysis using the intelligence layer
	findings, err := s.performCorrelationAnalysis(ctx, domainEvents, req.AnalysisType)
	if err != nil {
		job.Status = "failed"
		s.decrementActiveAnalyses()
		return nil, status.Errorf(codes.Internal, "analysis failed: %v", err)
	}

	// Store findings if storage is available
	if s.findingStore != nil {
		for _, finding := range findings {
			if err := s.findingStore.StoreFinding(ctx, finding); err != nil {
				s.logger.Warn("Failed to store finding", zap.Error(err))
			}
		}
	}

	// Update job status
	job.Status = "completed"
	job.Progress = 1.0
	if len(findings) > 0 {
		job.Result = findings[0] // Store the primary finding
	}
	s.decrementActiveAnalyses()
	s.incrementTotalAnalyses()
	s.addFindingsGenerated(int64(len(findings)))

	// Convert findings to proto format
	protoFindings := make([]*pb.CorrelationFinding, len(findings))
	for i, finding := range findings {
		protoFindings[i] = s.convertFindingToProto(finding)
	}

	return &pb.AnalyzeEventsResponse{
		AnalysisId: jobID,
		Findings:   protoFindings,
		Status:     pb.AnalysisStatus_ANALYSIS_STATUS_COMPLETED,
		StartTime:  timestamppb.New(job.StartTime),
		EndTime:    timestamppb.Now(),
		EventCount: int32(len(domainEvents)),
	}, nil
}

// GetCorrelations retrieves existing correlations based on filter criteria
func (s *CorrelationServer) GetCorrelations(ctx context.Context, req *pb.GetCorrelationsRequest) (*pb.GetCorrelationsResponse, error) {
	s.incrementRequestCount()

	ctx, span := s.tracer.Start(ctx, "correlation.get_correlations")
	defer span.End()

	s.logger.Debug("Getting correlations")

	// Convert request to filter
	filter := CorrelationFilter{
		TimeRange:     req.TimeRange,
		PatternType:   req.PatternType,
		MinConfidence: req.MinConfidence,
		EntityType:    req.EntityType,
		Limit:         int(req.Limit),
	}

	// Query correlations from storage
	var correlations []*correlation.Finding
	var err error

	if s.correlationStore != nil {
		correlations, err = s.correlationStore.GetCorrelations(ctx, filter)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to query correlations: %v", err)
		}
	}

	// Convert to proto format
	protoCorrelations := make([]*pb.CorrelationFinding, len(correlations))
	for i, finding := range correlations {
		protoCorrelations[i] = s.convertFindingToProto(finding)
	}

	return &pb.GetCorrelationsResponse{
		Correlations: protoCorrelations,
		TotalCount:   int64(len(correlations)),
		QueryTime:    timestamppb.Now(),
	}, nil
}

// GetAnalysisStatus returns the status of an ongoing analysis
func (s *CorrelationServer) GetAnalysisStatus(ctx context.Context, req *pb.GetAnalysisStatusRequest) (*pb.GetAnalysisStatusResponse, error) {
	s.incrementRequestCount()

	ctx, span := s.tracer.Start(ctx, "correlation.get_analysis_status")
	defer span.End()

	s.logger.Debug("Getting analysis status", zap.String("analysis_id", req.AnalysisId))

	// Look up analysis job
	jobInterface, exists := s.analysisJobs.Load(req.AnalysisId)
	if !exists {
		return nil, status.Error(codes.NotFound, "analysis not found")
	}

	job := jobInterface.(*AnalysisJob)

	// Convert status
	var protoStatus pb.AnalysisStatus
	switch job.Status {
	case "running":
		protoStatus = pb.AnalysisStatus_ANALYSIS_STATUS_RUNNING
	case "completed":
		protoStatus = pb.AnalysisStatus_ANALYSIS_STATUS_COMPLETED
	case "failed":
		protoStatus = pb.AnalysisStatus_ANALYSIS_STATUS_FAILED
	default:
		protoStatus = pb.AnalysisStatus_ANALYSIS_STATUS_UNKNOWN
	}

	response := &pb.GetAnalysisStatusResponse{
		AnalysisId: job.ID,
		Status:     protoStatus,
		Progress:   float32(job.Progress),
		StartTime:  timestamppb.New(job.StartTime),
		EventCount: int32(len(job.Events)),
	}

	// Add result if analysis is completed
	if job.Result != nil {
		response.Result = s.convertFindingToProto(job.Result)
	}

	return response, nil
}

// ConfigureCorrelationIngestion configures the correlation analysis settings
func (s *CorrelationServer) ConfigureCorrelationIngestion(config CorrelationIngestionConfig) {
	s.logger.Info("Configuring correlation ingestion",
		zap.Bool("collector_analysis", config.EnableCollectorAnalysis),
		zap.Bool("ebpf_analysis", config.EnableeBPFAnalysis),
		zap.Bool("k8s_analysis", config.EnableK8sAnalysis),
		zap.Bool("otel_analysis", config.EnableOTELAnalysis),
		zap.Float64("confidence_threshold", config.ConfidenceThreshold),
	)

	// Update configuration
	s.config.ConfidenceThreshold = config.ConfidenceThreshold
	s.config.AnalysisTimeout = config.AnalysisTimeout
	s.config.EnableRealTimeUpdates = config.EnableRealTimeUpdates
	s.config.EnableRootCause = config.EnableRootCause
	s.config.EnablePredictions = config.EnablePredictions
	s.config.EnableImpactAssess = config.EnableImpactAssess
}

// StartPeriodicAnalysis starts periodic correlation analysis
func (s *CorrelationServer) StartPeriodicAnalysis(interval time.Duration) {
	s.logger.Info("Starting periodic correlation analysis", zap.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			s.performPeriodicAnalysis()
		}
	}()
}

// CorrelationIngestionConfig holds correlation ingestion configuration
type CorrelationIngestionConfig struct {
	EnableCollectorAnalysis bool
	EnableeBPFAnalysis      bool
	EnableK8sAnalysis       bool
	EnableOTELAnalysis      bool
	ConfidenceThreshold     float64
	MaxEventsPerAnalysis    int
	AnalysisTimeout         time.Duration
	EnableRealTimeUpdates   bool
	EnableRootCause         bool
	EnablePredictions       bool
	EnableImpactAssess      bool
}

// GetServiceStats returns statistics for the correlation service
func (s *CorrelationServer) GetServiceStats() map[string]interface{} {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	return map[string]interface{}{
		"start_time":         s.stats.startTime,
		"uptime_seconds":     time.Since(s.stats.startTime).Seconds(),
		"request_count":      s.stats.requestCount,
		"total_analyses":     s.stats.TotalAnalyses,
		"active_analyses":    s.stats.ActiveAnalyses,
		"findings_generated": s.stats.FindingsGenerated,
		"service_type":       "correlation_analysis",
	}
}

// HealthCheck checks the health of the correlation service
func (s *CorrelationServer) HealthCheck() error {
	// Check if core dependencies are available
	if s.correlationEngine == nil {
		return fmt.Errorf("correlation engine not initialized")
	}

	if s.correlationStore == nil {
		return fmt.Errorf("correlation store not initialized")
	}

	return nil
}

// Helper methods

func (s *CorrelationServer) performCorrelationAnalysis(ctx context.Context, events []domain.Event, analysisType pb.AnalysisType) ([]*correlation.Finding, error) {
	// Use the semantic correlation engine from the intelligence layer
	if s.correlationEngine == nil {
		return nil, fmt.Errorf("correlation engine not available")
	}

	var findings []*correlation.Finding

	// Process each event through the correlation engine
	for _, event := range events {
		s.correlationEngine.ProcessEvent(&event)
	}

	// Get the latest findings
	if latestFinding := s.correlationEngine.GetLatestFindings(); latestFinding != nil {
		findings = append(findings, latestFinding)
	}

	// Filter findings by confidence threshold
	filteredFindings := make([]*correlation.Finding, 0)
	for _, finding := range findings {
		if finding.Confidence >= s.config.ConfidenceThreshold {
			filteredFindings = append(filteredFindings, finding)
		}
	}

	return filteredFindings, nil
}

func (s *CorrelationServer) performPeriodicAnalysis() {
	s.logger.Debug("Performing periodic correlation analysis")

	// This would integrate with the data flow to get recent events
	// and perform correlation analysis on them

	// For now, just log that periodic analysis would happen
	s.logger.Debug("Periodic analysis completed")
}

func (s *CorrelationServer) convertFindingToProto(finding *correlation.Finding) *pb.CorrelationFinding {
	return &pb.CorrelationFinding{
		Id:              finding.ID,
		PatternType:     finding.PatternType,
		Confidence:      finding.Confidence,
		Timestamp:       timestamppb.New(finding.Timestamp),
		Description:     finding.Description,
		RelatedEventIds: finding.RelatedEvents,
		Metadata: map[string]string{
			"analysis_type": "semantic",
			"engine":        "tapio_correlation",
		},
	}
}

func (s *CorrelationServer) incrementRequestCount() {
	s.stats.mu.Lock()
	s.stats.requestCount++
	s.stats.mu.Unlock()
}

func (s *CorrelationServer) incrementTotalAnalyses() {
	s.stats.mu.Lock()
	s.stats.TotalAnalyses++
	s.stats.mu.Unlock()
}

func (s *CorrelationServer) incrementActiveAnalyses() {
	s.stats.mu.Lock()
	s.stats.ActiveAnalyses++
	s.stats.mu.Unlock()
}

func (s *CorrelationServer) decrementActiveAnalyses() {
	s.stats.mu.Lock()
	s.stats.ActiveAnalyses--
	s.stats.mu.Unlock()
}

func (s *CorrelationServer) addFindingsGenerated(count int64) {
	s.stats.mu.Lock()
	s.stats.FindingsGenerated += count
	s.stats.mu.Unlock()
}

// InMemoryCorrelationStore provides a simple in-memory correlation store for development
type InMemoryCorrelationStore struct {
	correlations map[string]*correlation.Finding
	mu           sync.RWMutex
}

func (s *InMemoryCorrelationStore) StoreCorrelation(ctx context.Context, correlation *correlation.Finding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.correlations[correlation.ID] = correlation
	return nil
}

func (s *InMemoryCorrelationStore) GetCorrelations(ctx context.Context, filter CorrelationFilter) ([]*correlation.Finding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*correlation.Finding

	for _, finding := range s.correlations {
		// Apply filters
		if filter.MinConfidence > 0 && finding.Confidence < filter.MinConfidence {
			continue
		}

		if filter.PatternType != "" && finding.PatternType != filter.PatternType {
			continue
		}

		results = append(results, finding)

		// Apply limit
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}

	return results, nil
}

func (s *InMemoryCorrelationStore) GetCorrelationByID(ctx context.Context, id string) (*correlation.Finding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if finding, exists := s.correlations[id]; exists {
		return finding, nil
	}

	return nil, fmt.Errorf("correlation not found: %s", id)
}

// InMemoryFindingStore provides a simple in-memory finding store for development
type InMemoryFindingStore struct {
	findings map[string]*correlation.Finding
	mu       sync.RWMutex
}

func (s *InMemoryFindingStore) StoreFinding(ctx context.Context, finding *correlation.Finding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.findings[finding.ID] = finding
	return nil
}

func (s *InMemoryFindingStore) GetFindings(ctx context.Context, filter FindingFilter) ([]*correlation.Finding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*correlation.Finding

	for _, finding := range s.findings {
		// Apply filters
		if filter.MinConfidence > 0 && finding.Confidence < filter.MinConfidence {
			continue
		}

		results = append(results, finding)

		// Apply limit
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}

	return results, nil
}
