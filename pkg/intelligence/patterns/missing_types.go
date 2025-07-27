package patterns

import (
	"math"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Missing type definitions for dynamic_correlation_engine.go
// These are stub implementations to fix compilation errors

// AnomalyCorrelator detects anomalous correlations
type AnomalyCorrelator struct {
	threshold float64
}

// NewAnomalyCorrelator creates a new anomaly correlator
func NewAnomalyCorrelator(threshold float64) *AnomalyCorrelator {
	return &AnomalyCorrelator{threshold: threshold}
}

// DetectAnomalies detects anomalous patterns in events
func (a *AnomalyCorrelator) DetectAnomalies(events []*domain.UnifiedEvent) []AnomalyResult {
	// Stub implementation
	return []AnomalyResult{}
}

// AnomalyResult represents an anomaly detection result
type AnomalyResult struct {
	EventID      string
	AnomalyScore float64
	Description  string
}

// CorrelationStats tracks statistical data about correlations
type CorrelationStats struct {
	TotalObservations  int
	CorrectPredictions int
	FalsePositives     int
	FalseNegatives     int
	Accuracy           float64
	Precision          float64
	Recall             float64
	LastUpdated        time.Time
}

// UpdateStats updates the correlation statistics
func (cs *CorrelationStats) UpdateStats() {
	total := float64(cs.CorrectPredictions + cs.FalsePositives + cs.FalseNegatives)
	if total > 0 {
		cs.Accuracy = float64(cs.CorrectPredictions) / total
		if cs.CorrectPredictions+cs.FalsePositives > 0 {
			cs.Precision = float64(cs.CorrectPredictions) / float64(cs.CorrectPredictions+cs.FalsePositives)
		}
		if cs.CorrectPredictions+cs.FalseNegatives > 0 {
			cs.Recall = float64(cs.CorrectPredictions) / float64(cs.CorrectPredictions+cs.FalseNegatives)
		}
	}
	cs.LastUpdated = time.Now()
}

// SuffixTree represents a suffix tree for sequence analysis
type SuffixTree struct {
	root *SuffixNode
}

// SuffixNode represents a node in the suffix tree
type SuffixNode struct {
	children map[string]*SuffixNode
	events   []*domain.UnifiedEvent
}

// NewSuffixTree creates a new suffix tree
func NewSuffixTree() *SuffixTree {
	return &SuffixTree{
		root: &SuffixNode{
			children: make(map[string]*SuffixNode),
		},
	}
}

// Add adds an event to the suffix tree
func (st *SuffixTree) Add(event *domain.UnifiedEvent) {
	// Stub implementation
	if st.root.children[event.Type.String()] == nil {
		st.root.children[event.Type.String()] = &SuffixNode{
			children: make(map[string]*SuffixNode),
			events:   []*domain.UnifiedEvent{event},
		}
	} else {
		st.root.children[event.Type.String()].events = append(
			st.root.children[event.Type.String()].events, event)
	}
}

// EventSequence represents a sequence of related events
type EventSequence struct {
	ID        string
	Events    []*domain.UnifiedEvent
	StartTime time.Time
	EndTime   time.Time
	Pattern   string
	Score     float64
}

// NewEventSequence creates a new event sequence
func NewEventSequence(id string) *EventSequence {
	return &EventSequence{
		ID:        id,
		Events:    make([]*domain.UnifiedEvent, 0),
		StartTime: time.Now(),
	}
}

// Add adds an event to the sequence
func (es *EventSequence) Add(event *domain.UnifiedEvent) {
	es.Events = append(es.Events, event)
	es.EndTime = event.Timestamp
}

// CouldFollow checks if an event could follow in this sequence
func (es *EventSequence) CouldFollow(event *domain.UnifiedEvent) bool {
	// Stub implementation - check if event is from same entity
	if len(es.Events) == 0 {
		return true
	}
	lastEvent := es.Events[len(es.Events)-1]
	return lastEvent.Entity != nil && event.Entity != nil &&
		lastEvent.Entity.Name == event.Entity.Name
}

// IsSignificant checks if the sequence is statistically significant
func (es *EventSequence) IsSignificant() bool {
	// Stub implementation - consider significant if has more than 2 events
	return len(es.Events) > 2
}

// TimeSeries represents time series data for temporal analysis
type TimeSeries struct {
	Timestamps []time.Time
	Values     []float64
	Metadata   map[string]interface{}
}

// NewTimeSeries creates a new time series
func NewTimeSeries() *TimeSeries {
	return &TimeSeries{
		Timestamps: make([]time.Time, 0),
		Values:     make([]float64, 0),
		Metadata:   make(map[string]interface{}),
	}
}

// AddPoint adds a data point to the time series
func (ts *TimeSeries) AddPoint(timestamp time.Time, value float64) {
	ts.Timestamps = append(ts.Timestamps, timestamp)
	ts.Values = append(ts.Values, value)
}

// PeriodicPattern represents a periodic pattern in events
type PeriodicPattern struct {
	Period      time.Duration
	Phase       time.Duration
	Amplitude   float64
	Confidence  float64
	Description string
}

// NewPeriodicPattern creates a new periodic pattern
func NewPeriodicPattern(period time.Duration) *PeriodicPattern {
	return &PeriodicPattern{
		Period:     period,
		Confidence: 0.0,
	}
}

// TimeCorrelationMatrix represents correlations over time
type TimeCorrelationMatrix struct {
	matrix     map[string]map[string]float64
	timestamps []time.Time
	updated    time.Time
}

// NewTimeCorrelationMatrix creates a new time correlation matrix
func NewTimeCorrelationMatrix() *TimeCorrelationMatrix {
	return &TimeCorrelationMatrix{
		matrix:     make(map[string]map[string]float64),
		timestamps: make([]time.Time, 0),
		updated:    time.Now(),
	}
}

// UpdateCorrelation updates correlation between two event types
func (tcm *TimeCorrelationMatrix) UpdateCorrelation(typeA, typeB string, correlation float64) {
	if tcm.matrix[typeA] == nil {
		tcm.matrix[typeA] = make(map[string]float64)
	}
	tcm.matrix[typeA][typeB] = correlation
	tcm.updated = time.Now()
}

// GetCorrelation gets correlation between two event types
func (tcm *TimeCorrelationMatrix) GetCorrelation(typeA, typeB string) float64 {
	if tcm.matrix[typeA] != nil {
		return tcm.matrix[typeA][typeB]
	}
	return 0.0
}

// DirectedGraph represents a directed graph for causal analysis
type DirectedGraph struct {
	nodes map[string]*GraphNode
	edges map[string]map[string]*GraphEdge
}

// GraphNode represents a node in the directed graph
type GraphNode struct {
	ID       string
	Type     string
	Metadata map[string]interface{}
}

// GraphEdge represents an edge in the directed graph
type GraphEdge struct {
	From   string
	To     string
	Weight float64
	Type   string
}

// NewDirectedGraph creates a new directed graph
func NewDirectedGraph() *DirectedGraph {
	return &DirectedGraph{
		nodes: make(map[string]*GraphNode),
		edges: make(map[string]map[string]*GraphEdge),
	}
}

// AddNode adds a node to the graph
func (dg *DirectedGraph) AddNode(id, nodeType string) {
	dg.nodes[id] = &GraphNode{
		ID:       id,
		Type:     nodeType,
		Metadata: make(map[string]interface{}),
	}
}

// AddEdge adds an edge to the graph
func (dg *DirectedGraph) AddEdge(from, to string, weight float64) {
	if dg.edges[from] == nil {
		dg.edges[from] = make(map[string]*GraphEdge)
	}
	dg.edges[from][to] = &GraphEdge{
		From:   from,
		To:     to,
		Weight: weight,
		Type:   "causal",
	}
}

// GrangerCausalityTest performs Granger causality testing
type GrangerCausalityTest struct {
	maxLag    int
	threshold float64
}

// NewGrangerCausalityTest creates a new Granger causality test
func NewGrangerCausalityTest(maxLag int, threshold float64) *GrangerCausalityTest {
	return &GrangerCausalityTest{
		maxLag:    maxLag,
		threshold: threshold,
	}
}

// Test performs Granger causality test between two time series
func (gct *GrangerCausalityTest) Test(x, y *TimeSeries) CausalityResult {
	// Stub implementation
	return CausalityResult{
		IsSignificant: false,
		PValue:        1.0,
		FStatistic:    0.0,
		Lag:           0,
	}
}

// CausalityResult is already defined in learning_algorithms.go

// Intervention represents an experimental intervention for causal discovery
type Intervention struct {
	ID           string
	Target       string
	Type         string
	Timestamp    time.Time
	Parameters   map[string]interface{}
	Results      map[string]float64
	IsSuccessful bool
}

// NewIntervention creates a new intervention
func NewIntervention(id, target, interventionType string) *Intervention {
	return &Intervention{
		ID:         id,
		Target:     target,
		Type:       interventionType,
		Timestamp:  time.Now(),
		Parameters: make(map[string]interface{}),
		Results:    make(map[string]float64),
	}
}

// Execute executes the intervention
func (i *Intervention) Execute() error {
	// Stub implementation
	i.IsSuccessful = true
	return nil
}

// GetResult gets intervention result for a specific metric
func (i *Intervention) GetResult(metric string) (float64, bool) {
	result, exists := i.Results[metric]
	return result, exists
}

// Additional missing types from dynamic_correlation_engine.go

// CorrelationGraph represents a graph of correlations between events
type CorrelationGraph struct {
	nodes map[string]*CorrelationNode
	edges map[string]map[string]*CorrelationEdge
}

// CorrelationNode represents a node in the correlation graph
type CorrelationNode struct {
	ID       string
	Type     string
	Events   []*domain.UnifiedEvent
	Metadata map[string]interface{}
}

// CorrelationEdge represents an edge in the correlation graph
type CorrelationEdge struct {
	From       string
	To         string
	Strength   float64
	Type       string
	Confidence float64
}

// NewCorrelationGraph creates a new correlation graph
func NewCorrelationGraph() *CorrelationGraph {
	return &CorrelationGraph{
		nodes: make(map[string]*CorrelationNode),
		edges: make(map[string]map[string]*CorrelationEdge),
	}
}

// LearningConfig configures the learning algorithm parameters
type LearningConfig struct {
	WindowSize      time.Duration
	MinSupport      float64
	MinConfidence   float64
	MaxPatterns     int
	LearningRate    float64
	DecayFactor     float64
	EnableAdaptive  bool
	EnableCausality bool
	EnableTemporal  bool
	EnableSpatial   bool
	UpdateInterval  time.Duration
}

// DefaultLearningConfig returns default learning configuration
func DefaultLearningConfig() LearningConfig {
	return LearningConfig{
		WindowSize:      5 * time.Minute,
		MinSupport:      0.1,
		MinConfidence:   0.7,
		MaxPatterns:     1000,
		LearningRate:    0.01,
		DecayFactor:     0.95,
		EnableAdaptive:  true,
		EnableCausality: true,
		EnableTemporal:  true,
		EnableSpatial:   false,
		UpdateInterval:  30 * time.Second,
	}
}

// StreamAnalyzer analyzes event streams in real-time
type StreamAnalyzer struct {
	windowSize time.Duration
	buffer     []*domain.UnifiedEvent
	analyzer   func([]*domain.UnifiedEvent) []StreamResult
}

// StreamResult represents a stream analysis result
type StreamResult struct {
	Pattern     string
	Confidence  float64
	Events      []*domain.UnifiedEvent
	Timestamp   time.Time
	Description string
}

// NewStreamAnalyzer creates a new stream analyzer
func NewStreamAnalyzer(windowSize time.Duration) *StreamAnalyzer {
	return &StreamAnalyzer{
		windowSize: windowSize,
		buffer:     make([]*domain.UnifiedEvent, 0),
	}
}

// UnsupervisedPatternMiner mines patterns without labeled data
type UnsupervisedPatternMiner struct {
	algorithms []MiningAlgorithm
	threshold  float64
	patterns   map[string]*MinedPattern
}

// MiningAlgorithm represents a pattern mining algorithm
type MiningAlgorithm interface {
	Mine(events []*domain.UnifiedEvent) []*MinedPattern
	GetName() string
}

// MinedPattern represents a pattern discovered by mining
type MinedPattern struct {
	ID          string
	Type        string
	Support     float64
	Confidence  float64
	Events      []*domain.UnifiedEvent
	Description string
	Discovered  time.Time
}

// NewUnsupervisedPatternMiner creates a new unsupervised pattern miner
func NewUnsupervisedPatternMiner(threshold float64) *UnsupervisedPatternMiner {
	return &UnsupervisedPatternMiner{
		algorithms: make([]MiningAlgorithm, 0),
		threshold:  threshold,
		patterns:   make(map[string]*MinedPattern),
	}
}

// HypothesisGenerator generates hypotheses about correlations
type HypothesisGenerator struct {
	generators []HypothesisAlgorithm
	hypotheses map[string]*Hypothesis
}

// HypothesisAlgorithm represents a hypothesis generation algorithm
type HypothesisAlgorithm interface {
	Generate(events []*domain.UnifiedEvent) []*Hypothesis
	GetName() string
}

// Hypothesis represents a testable hypothesis about event correlations
type Hypothesis struct {
	ID          string
	Description string
	Type        string
	Parameters  map[string]interface{}
	Testable    bool
	Priority    float64
	Generated   time.Time
}

// NewHypothesisGenerator creates a new hypothesis generator
func NewHypothesisGenerator() *HypothesisGenerator {
	return &HypothesisGenerator{
		generators: make([]HypothesisAlgorithm, 0),
		hypotheses: make(map[string]*Hypothesis),
	}
}

// StatisticalTester performs statistical tests on hypotheses
type StatisticalTester struct {
	tests   map[string]StatisticalTest
	results map[string]*TestResult
}

// StatisticalTest represents a statistical test
type StatisticalTest interface {
	Test(hypothesis *Hypothesis, events []*domain.UnifiedEvent) *TestResult
	GetName() string
}

// TestResult represents the result of a statistical test
type TestResult struct {
	HypothesisID  string
	TestName      string
	IsSignificant bool
	PValue        float64
	TestStatistic float64
	Confidence    float64
	Timestamp     time.Time
}

// NewStatisticalTester creates a new statistical tester
func NewStatisticalTester() *StatisticalTester {
	return &StatisticalTester{
		tests:   make(map[string]StatisticalTest),
		results: make(map[string]*TestResult),
	}
}

// CorrelationRefiner refines and improves correlations over time
type CorrelationRefiner struct {
	refiners map[string]RefinementAlgorithm
	refined  map[string]*RefinedCorrelation
}

// LiveCorrelation represents a correlation being learned (referenced by RefinementAlgorithm)
type LiveCorrelation struct {
	ID               string
	Type             string // sequence, temporal, causal, spatial
	Confidence       float64
	ObservationCount int
	FirstSeen        time.Time
	LastSeen         time.Time
}

// RefinementAlgorithm represents a correlation refinement algorithm
type RefinementAlgorithm interface {
	Refine(correlation *LiveCorrelation, events []*domain.UnifiedEvent) *RefinedCorrelation
	GetName() string
}

// RefinedCorrelation represents an improved correlation
type RefinedCorrelation struct {
	OriginalID    string
	NewID         string
	Improvements  []string
	OldConfidence float64
	NewConfidence float64
	Timestamp     time.Time
}

// NewCorrelationRefiner creates a new correlation refiner
func NewCorrelationRefiner() *CorrelationRefiner {
	return &CorrelationRefiner{
		refiners: make(map[string]RefinementAlgorithm),
		refined:  make(map[string]*RefinedCorrelation),
	}
}

// FeedbackEvent represents feedback about correlation quality
type FeedbackEvent struct {
	CorrelationID string
	EventID       string
	FeedbackType  string // positive, negative, neutral
	Confidence    float64
	Source        string
	Timestamp     time.Time
	Metadata      map[string]interface{}
}

// NewFeedbackEvent creates a new feedback event
func NewFeedbackEvent(correlationID, eventID, feedbackType string) *FeedbackEvent {
	return &FeedbackEvent{
		CorrelationID: correlationID,
		EventID:       eventID,
		FeedbackType:  feedbackType,
		Source:        "system",
		Timestamp:     time.Now(),
		Metadata:      make(map[string]interface{}),
	}
}

// PearsonCalculator calculates Pearson correlation coefficients
type PearsonCalculator struct {
	cache map[string]float64
}

// NewPearsonCalculator creates a new Pearson calculator
func NewPearsonCalculator() *PearsonCalculator {
	return &PearsonCalculator{
		cache: make(map[string]float64),
	}
}

// Calculate calculates Pearson correlation between two data series
func (pc *PearsonCalculator) Calculate(x, y []float64) float64 {
	if len(x) != len(y) || len(x) == 0 {
		return 0.0
	}

	// Calculate means
	var sumX, sumY float64
	n := float64(len(x))
	for i := 0; i < len(x); i++ {
		sumX += x[i]
		sumY += y[i]
	}
	meanX := sumX / n
	meanY := sumY / n

	// Calculate correlation
	var numerator, denomX, denomY float64
	for i := 0; i < len(x); i++ {
		dx := x[i] - meanX
		dy := y[i] - meanY
		numerator += dx * dy
		denomX += dx * dx
		denomY += dy * dy
	}

	if denomX == 0 || denomY == 0 {
		return 0.0
	}

	return numerator / (math.Sqrt(denomX) * math.Sqrt(denomY))
}

// Additional missing types from learning_core.go and other files

// ChiSquareTest performs chi-square statistical tests
type ChiSquareTest struct {
	degreesOfFreedom int
	alpha            float64
}

// NewChiSquareTest creates a new chi-square test
func NewChiSquareTest(df int, alpha float64) *ChiSquareTest {
	return &ChiSquareTest{
		degreesOfFreedom: df,
		alpha:            alpha,
	}
}

// Test performs chi-square test on observed vs expected frequencies
func (c *ChiSquareTest) Test(observed, expected []float64) bool {
	// Stub implementation
	return len(observed) == len(expected)
}

// LSTMNetwork represents a Long Short-Term Memory neural network
type LSTMNetwork struct {
	hiddenSize int
	layers     int
	weights    [][]float64
}

// NewLSTMNetwork creates a new LSTM network
func NewLSTMNetwork(hiddenSize, layers int) *LSTMNetwork {
	return &LSTMNetwork{
		hiddenSize: hiddenSize,
		layers:     layers,
		weights:    make([][]float64, layers),
	}
}

// Forward performs forward pass through the network
func (l *LSTMNetwork) Forward(input []float64) []float64 {
	// Stub implementation
	return make([]float64, l.hiddenSize)
}

// ConceptDriftDetector detects when the underlying data distribution changes
type ConceptDriftDetector struct {
	windowSize int
	threshold  float64
	history    []float64
}

// NewConceptDriftDetector creates a new concept drift detector
func NewConceptDriftDetector(windowSize int, threshold float64) *ConceptDriftDetector {
	return &ConceptDriftDetector{
		windowSize: windowSize,
		threshold:  threshold,
		history:    make([]float64, 0),
	}
}

// DetectDrift checks if concept drift has occurred
func (c *ConceptDriftDetector) DetectDrift(value float64) bool {
	c.history = append(c.history, value)
	if len(c.history) > c.windowSize {
		c.history = c.history[1:]
	}
	// Stub implementation - always return false
	return false
}

// OwnershipGraph represents ownership relationships in K8s
type OwnershipGraph struct {
	owners map[string][]string
	owned  map[string]string
}

// NewOwnershipGraph creates a new ownership graph
func NewOwnershipGraph() *OwnershipGraph {
	return &OwnershipGraph{
		owners: make(map[string][]string),
		owned:  make(map[string]string),
	}
}

// AddOwnership adds an ownership relationship
func (o *OwnershipGraph) AddOwnership(owner, owned string) {
	o.owners[owner] = append(o.owners[owner], owned)
	o.owned[owned] = owner
}

// SelectorGraph represents label selector relationships
type SelectorGraph struct {
	selectors map[string]map[string]string
	selected  map[string][]string
}

// NewSelectorGraph creates a new selector graph
func NewSelectorGraph() *SelectorGraph {
	return &SelectorGraph{
		selectors: make(map[string]map[string]string),
		selected:  make(map[string][]string),
	}
}

// AddSelector adds a selector relationship
func (s *SelectorGraph) AddSelector(selector string, labels map[string]string) {
	s.selectors[selector] = labels
}

// EventRelationGraph represents relationships between events
type EventRelationGraph struct {
	relations map[string][]string
	weights   map[string]map[string]float64
}

// NewEventRelationGraph creates a new event relation graph
func NewEventRelationGraph() *EventRelationGraph {
	return &EventRelationGraph{
		relations: make(map[string][]string),
		weights:   make(map[string]map[string]float64),
	}
}

// AddRelation adds a relationship between events
func (e *EventRelationGraph) AddRelation(from, to string, weight float64) {
	e.relations[from] = append(e.relations[from], to)
	if e.weights[from] == nil {
		e.weights[from] = make(map[string]float64)
	}
	e.weights[from][to] = weight
}

// DynamicCorrelationEngine forward declaration for self_learning_system.go
type DynamicCorrelationEngine struct {
	config LearningConfig
	logger interface{} // Using interface{} to avoid circular dependency
}

// NewDynamicCorrelationEngine creates a new dynamic correlation engine
func NewDynamicCorrelationEngine(config LearningConfig) *DynamicCorrelationEngine {
	return &DynamicCorrelationEngine{
		config: config,
	}
}

// EnvironmentProfile represents the current environment characteristics
type EnvironmentProfile struct {
	EventRate   float64
	ErrorRate   float64
	Complexity  float64
	Seasonality map[string]float64
	LastUpdated time.Time
}

// NewEnvironmentProfile creates a new environment profile
func NewEnvironmentProfile() *EnvironmentProfile {
	return &EnvironmentProfile{
		Seasonality: make(map[string]float64),
		LastUpdated: time.Now(),
	}
}

// NormalBehaviorModel represents the learned normal behavior
type NormalBehaviorModel struct {
	Patterns    map[string]*Pattern
	Thresholds  map[string]float64
	LastTrained time.Time
}

// Pattern represents a behavioral pattern
type Pattern struct {
	ID         string
	Type       string
	Frequency  float64
	Confidence float64
	LastSeen   time.Time
}

// NewNormalBehaviorModel creates a new normal behavior model
func NewNormalBehaviorModel() *NormalBehaviorModel {
	return &NormalBehaviorModel{
		Patterns:    make(map[string]*Pattern),
		Thresholds:  make(map[string]float64),
		LastTrained: time.Now(),
	}
}

// Additional missing types from self_learning_system.go and other files

// EnvironmentAdapter adapts to changing environments
type EnvironmentAdapter struct {
	profile *EnvironmentProfile
	config  AdaptationConfig
}

// AdaptationConfig configures environment adaptation
type AdaptationConfig struct {
	LearningRate    float64
	AdaptationRate  float64
	StabilityWindow time.Duration
}

// NewEnvironmentAdapter creates a new environment adapter
func NewEnvironmentAdapter(config AdaptationConfig) *EnvironmentAdapter {
	return &EnvironmentAdapter{
		profile: NewEnvironmentProfile(),
		config:  config,
	}
}

// WorkloadAnalyzer analyzes workload patterns
type WorkloadAnalyzer struct {
	patterns map[string]*WorkloadPattern
	metrics  map[string]float64
}

// WorkloadPattern represents a workload pattern
type WorkloadPattern struct {
	Name      string
	Type      string
	Frequency float64
	Intensity float64
	Duration  time.Duration
	LastSeen  time.Time
}

// NewWorkloadAnalyzer creates a new workload analyzer
func NewWorkloadAnalyzer() *WorkloadAnalyzer {
	return &WorkloadAnalyzer{
		patterns: make(map[string]*WorkloadPattern),
		metrics:  make(map[string]float64),
	}
}

// CorrelationResult represents the result of correlation analysis
type CorrelationResult struct {
	ID         string
	Type       string
	Source     *domain.UnifiedEvent
	Target     *domain.UnifiedEvent
	Strength   float64
	Confidence float64
	Latency    time.Duration
	Evidence   []string
	Timestamp  time.Time
}

// NewCorrelationResult creates a new correlation result
func NewCorrelationResult(id, correlationType string) *CorrelationResult {
	return &CorrelationResult{
		ID:        id,
		Type:      correlationType,
		Evidence:  make([]string, 0),
		Timestamp: time.Now(),
	}
}

// LearnedIntelligence represents learned intelligence from patterns
type LearnedIntelligence struct {
	Rules       map[string]*IntelligenceRule
	Predictions map[string]*Prediction
	Insights    []string
	Version     int
	UpdatedAt   time.Time
}

// IntelligenceRule represents a learned rule
type IntelligenceRule struct {
	ID           string
	Condition    string
	Action       string
	Confidence   float64
	Applications int
	CreatedAt    time.Time
}

// Prediction represents a prediction about future events
type Prediction struct {
	ID          string
	Type        string
	Target      string
	Probability float64
	Window      time.Duration
	CreatedAt   time.Time
}

// NewLearnedIntelligence creates new learned intelligence
func NewLearnedIntelligence() *LearnedIntelligence {
	return &LearnedIntelligence{
		Rules:       make(map[string]*IntelligenceRule),
		Predictions: make(map[string]*Prediction),
		Insights:    make([]string, 0),
		Version:     1,
		UpdatedAt:   time.Now(),
	}
}

// LearnedCorrelation represents a learned correlation pattern
type LearnedCorrelation struct {
	ID           string
	Pattern      string
	Type         string
	EventTypeA   string
	EventTypeB   string
	Strength     float64
	Confidence   float64
	Observations int
	TimeDelta    time.Duration
	Conditions   map[string]interface{}
	LastSeen     time.Time
	CreatedAt    time.Time
}

// NewLearnedCorrelation creates a new learned correlation
func NewLearnedCorrelation(id, pattern, correlationType string) *LearnedCorrelation {
	return &LearnedCorrelation{
		ID:         id,
		Pattern:    pattern,
		Type:       correlationType,
		Conditions: make(map[string]interface{}),
		CreatedAt:  time.Now(),
		LastSeen:   time.Now(),
	}
}

// Correlation represents a basic correlation between events
type Correlation struct {
	ID        string
	EventA    *domain.UnifiedEvent
	EventB    *domain.UnifiedEvent
	Type      string // temporal, causal, spatial, semantic
	Strength  float64
	Direction string // bidirectional, a_to_b, b_to_a
	Metadata  map[string]interface{}
	CreatedAt time.Time
}

// NewCorrelation creates a new correlation
func NewCorrelation(id, correlationType string) *Correlation {
	return &Correlation{
		ID:        id,
		Type:      correlationType,
		Metadata:  make(map[string]interface{}),
		CreatedAt: time.Now(),
	}
}

// SetEvents sets the correlated events
func (c *Correlation) SetEvents(eventA, eventB *domain.UnifiedEvent) {
	c.EventA = eventA
	c.EventB = eventB
}
