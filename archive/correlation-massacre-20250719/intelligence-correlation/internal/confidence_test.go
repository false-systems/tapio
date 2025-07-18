package internal
import (
	"testing"
	"time"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/core"
)
func TestNewConfidenceCalculator(t *testing.T) {
	calc := NewConfidenceCalculator()
	if calc == nil {
		t.Fatal("Calculator should not be nil")
	}
}
func TestConfidenceCalculator_ComputeEventConfidence(t *testing.T) {
	calc := NewConfidenceCalculator()
	event := createTestEvent()
	confidence := calc.ComputeEventConfidence(event)
	if confidence < 0 || confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", confidence)
	}
	// Test with high confidence event
	highConfEvent := event
	highConfEvent.Confidence = 0.95
	highConfEvent.Severity = domain.SeverityCritical
	highConfEvent.Context.Host = "production-host"
	highConfEvent.Context.Labels = map[string]string{"env": "prod"}
	highConfEvent.Context.Tags = []string{"important"}
	highConfidence := calc.ComputeEventConfidence(highConfEvent)
	if highConfidence <= confidence {
		t.Errorf("High confidence event should have higher score than base event")
	}
	// Test with low confidence event
	lowConfEvent := event
	lowConfEvent.Confidence = 0.2
	lowConfEvent.Severity = domain.SeverityDebug
	lowConfEvent.Timestamp = time.Now().Add(-7 * 24 * time.Hour) // Old event
	lowConfidence := calc.ComputeEventConfidence(lowConfEvent)
	if lowConfidence >= confidence {
		t.Errorf("Low confidence event should have lower score than base event")
	}
}
func TestConfidenceCalculator_ComputePatternConfidence(t *testing.T) {
	calc := NewConfidenceCalculator()
	pattern := &testPattern{id: "test-pattern"}
	events := []domain.Event{
		createTestEvent(),
		createTestMemoryEvent(),
		createTestNetworkEvent(),
	}
	confidence := calc.ComputePatternConfidence(pattern, events)
	if confidence < 0 || confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", confidence)
	}
	// Test with empty events
	emptyConfidence := calc.ComputePatternConfidence(pattern, []domain.Event{})
	if emptyConfidence != 0.0 {
		t.Errorf("Empty events should return 0 confidence, got %f", emptyConfidence)
	}
	// Test with high quality events
	highQualityEvents := []domain.Event{
		createHighQualityEvent(),
		createHighQualityEvent(),
		createHighQualityEvent(),
	}
	highConfidence := calc.ComputePatternConfidence(pattern, highQualityEvents)
	if highConfidence <= confidence {
		t.Errorf("High quality events should yield higher confidence")
	}
}
func TestConfidenceCalculator_ComputeConfidence(t *testing.T) {
	calc := NewConfidenceCalculator()
	correlation := createTestCorrelation()
	confidence := calc.ComputeConfidence(correlation)
	if confidence < 0 || confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", confidence)
	}
	// Test with high confidence correlation
	highConfCorrelation := correlation
	highConfCorrelation.Confidence = 0.95
	highConfCorrelation.Events = []domain.Event{
		createHighQualityEvent(),
		createHighQualityEvent(),
		createHighQualityEvent(),
	}
	highConfidence := calc.ComputeConfidence(highConfCorrelation)
	if highConfidence <= confidence {
		t.Errorf("High confidence correlation should have higher score")
	}
}
func TestConfidenceCalculator_GetConfidenceFactors(t *testing.T) {
	calc := NewConfidenceCalculator()
	correlation := createTestCorrelation()
	factors := calc.GetConfidenceFactors(correlation)
	if len(factors) == 0 {
		t.Error("Should return confidence factors")
	}
	// Check that all factors have valid values
	for _, factor := range factors {
		if factor.Name == "" {
			t.Error("Factor name should not be empty")
		}
		if factor.Weight < 0 || factor.Weight > 1 {
			t.Errorf("Factor weight should be between 0 and 1, got %f for %s", factor.Weight, factor.Name)
		}
		if factor.Description == "" {
			t.Error("Factor description should not be empty")
		}
		if factor.Source == "" {
			t.Error("Factor source should not be empty")
		}
	}
	// Check that weights sum to reasonable total
	totalWeight := 0.0
	for _, factor := range factors {
		totalWeight += factor.Weight
	}
	if totalWeight < 0.8 || totalWeight > 1.2 {
		t.Errorf("Total weight should be around 1.0, got %f", totalWeight)
	}
}
func TestConfidenceCalculator_WeightFactors(t *testing.T) {
	calc := NewConfidenceCalculator()
	// Test with valid factors
	factors := []core.ConfidenceFactor{
		{Name: "factor1", Value: 0.8, Weight: 0.4},
		{Name: "factor2", Value: 0.6, Weight: 0.3},
		{Name: "factor3", Value: 0.9, Weight: 0.3},
	}
	result := calc.WeightFactors(factors)
	if result < 0 || result > 1 {
		t.Errorf("Weighted result should be between 0 and 1, got %f", result)
	}
	// Expected: (0.8*0.4 + 0.6*0.3 + 0.9*0.3) / (0.4+0.3+0.3) = 0.77
	expected := 0.77
	tolerance := 0.01
	if result < expected-tolerance || result > expected+tolerance {
		t.Errorf("Expected result around %f, got %f", expected, result)
	}
	// Test with empty factors
	emptyResult := calc.WeightFactors([]core.ConfidenceFactor{})
	if emptyResult != 0.0 {
		t.Errorf("Empty factors should return 0, got %f", emptyResult)
	}
	// Test with zero weights
	zeroWeightFactors := []core.ConfidenceFactor{
		{Name: "factor1", Value: 0.8, Weight: 0.0},
		{Name: "factor2", Value: 0.6, Weight: 0.0},
	}
	zeroResult := calc.WeightFactors(zeroWeightFactors)
	if zeroResult != 0.0 {
		t.Errorf("Zero weight factors should return 0, got %f", zeroResult)
	}
	// Test with values outside [0,1] range (should be clamped)
	clampedFactors := []core.ConfidenceFactor{
		{Name: "high", Value: 1.5, Weight: 0.5},  // Should be clamped to 1.0
		{Name: "low", Value: -0.5, Weight: 0.5},  // Should be clamped to 0.0
	}
	clampedResult := calc.WeightFactors(clampedFactors)
	expectedClamped := 0.5 // (1.0*0.5 + 0.0*0.5) / (0.5+0.5) = 0.5
	if clampedResult != expectedClamped {
		t.Errorf("Clamped result should be %f, got %f", expectedClamped, clampedResult)
	}
}
func TestSourceReliability(t *testing.T) {
	calc := &confidenceCalculator{}
	// Test known sources
	ebpfReliability := calc.getSourceReliability(domain.SourceEBPF)
	if ebpfReliability != 0.95 {
		t.Errorf("Expected eBPF reliability 0.95, got %f", ebpfReliability)
	}
	k8sReliability := calc.getSourceReliability(domain.SourceKubernetes)
	if k8sReliability != 0.90 {
		t.Errorf("Expected K8s reliability 0.90, got %f", k8sReliability)
	}
	systemdReliability := calc.getSourceReliability(domain.SourceSystemd)
	if systemdReliability != 0.85 {
		t.Errorf("Expected systemd reliability 0.85, got %f", systemdReliability)
	}
	journaldReliability := calc.getSourceReliability(domain.SourceJournald)
	if journaldReliability != 0.80 {
		t.Errorf("Expected journald reliability 0.80, got %f", journaldReliability)
	}
	// Test unknown source (should return default)
	unknownReliability := calc.getSourceReliability(domain.Source("unknown"))
	if unknownReliability != 0.70 {
		t.Errorf("Expected unknown source reliability 0.70, got %f", unknownReliability)
	}
}
func TestSeverityFactor(t *testing.T) {
	calc := &confidenceCalculator{}
	// Test different severities
	criticalFactor := calc.getSeverityFactor(domain.SeverityCritical)
	if criticalFactor != 1.0 {
		t.Errorf("Expected critical severity factor 1.0, got %f", criticalFactor)
	}
	errorFactor := calc.getSeverityFactor(domain.SeverityError)
	if errorFactor != 0.9 {
		t.Errorf("Expected error severity factor 0.9, got %f", errorFactor)
	}
	warnFactor := calc.getSeverityFactor(domain.SeverityWarn)
	if warnFactor != 0.8 {
		t.Errorf("Expected warn severity factor 0.8, got %f", warnFactor)
	}
	infoFactor := calc.getSeverityFactor(domain.SeverityInfo)
	if infoFactor != 0.7 {
		t.Errorf("Expected info severity factor 0.7, got %f", infoFactor)
	}
	debugFactor := calc.getSeverityFactor(domain.SeverityDebug)
	if debugFactor != 0.6 {
		t.Errorf("Expected debug severity factor 0.6, got %f", debugFactor)
	}
}
func TestContextCompleteness(t *testing.T) {
	calc := &confidenceCalculator{}
	// Test complete context
	completeEvent := domain.Event{
		Context: domain.EventContext{
			Host:      "test-host",
			Container: "test-container",
			PID:       &[]int{1234}[0],
			Labels:    map[string]string{"env": "prod"},
			Tags:      []string{"important"},
		},
	}
	completeness := calc.getContextCompleteness(completeEvent)
	if completeness != 1.0 {
		t.Errorf("Expected complete context to have 1.0 completeness, got %f", completeness)
	}
	// Test empty context
	emptyEvent := domain.Event{
		Context: domain.EventContext{},
	}
	emptyCompleteness := calc.getContextCompleteness(emptyEvent)
	if emptyCompleteness != 0.0 {
		t.Errorf("Expected empty context to have 0.0 completeness, got %f", emptyCompleteness)
	}
	// Test partial context
	partialEvent := domain.Event{
		Context: domain.EventContext{
			Host:   "test-host",
			Labels: map[string]string{"env": "prod"},
		},
	}
	partialCompleteness := calc.getContextCompleteness(partialEvent)
	expected := 2.0 / 5.0 // 2 out of 5 context elements
	if partialCompleteness != expected {
		t.Errorf("Expected partial context completeness %f, got %f", expected, partialCompleteness)
	}
}
func TestTemporalFreshness(t *testing.T) {
	calc := &confidenceCalculator{}
	now := time.Now()
	// Test very fresh event (< 1 minute)
	freshFreshness := calc.getTemporalFreshness(now.Add(-30 * time.Second))
	if freshFreshness != 1.0 {
		t.Errorf("Expected fresh event to have 1.0 freshness, got %f", freshFreshness)
	}
	// Test recent event (< 1 hour)
	recentFreshness := calc.getTemporalFreshness(now.Add(-30 * time.Minute))
	if recentFreshness != 0.9 {
		t.Errorf("Expected recent event to have 0.9 freshness, got %f", recentFreshness)
	}
	// Test day-old event
	dayOldFreshness := calc.getTemporalFreshness(now.Add(-12 * time.Hour))
	if dayOldFreshness != 0.8 {
		t.Errorf("Expected day-old event to have 0.8 freshness, got %f", dayOldFreshness)
	}
	// Test week-old event
	weekOldFreshness := calc.getTemporalFreshness(now.Add(-3 * 24 * time.Hour))
	if weekOldFreshness != 0.6 {
		t.Errorf("Expected week-old event to have 0.6 freshness, got %f", weekOldFreshness)
	}
	// Test very old event
	oldFreshness := calc.getTemporalFreshness(now.Add(-30 * 24 * time.Hour))
	if oldFreshness != 0.4 {
		t.Errorf("Expected old event to have 0.4 freshness, got %f", oldFreshness)
	}
}
// Helper functions
func createTestCorrelation() domain.Correlation {
	return domain.Correlation{
		ID:         "test-correlation",
		Type:       domain.CorrelationTypeTemporal,
		Events:     []domain.Event{createTestEvent(), createTestMemoryEvent()},
		Confidence: 0.8,
		Timestamp:  time.Now(),
		Metadata: domain.CorrelationMetadata{
			SchemaVersion: "1.0",
			ProcessedAt:   time.Now(),
			ProcessedBy:   "test",
			Annotations:   map[string]string{"test": "true"},
		},
	}
}
func createHighQualityEvent() domain.Event {
	event := createTestEvent()
	event.Confidence = 0.95
	event.Severity = domain.SeverityCritical
	event.Context.Host = "production-host"
	event.Context.Container = "important-service"
	event.Context.PID = &[]int{1234}[0]
	event.Context.Labels = map[string]string{
		"env":     "production",
		"service": "critical-service",
		"team":    "platform",
	}
	event.Context.Tags = []string{"critical", "monitored", "sla"}
	event.Timestamp = time.Now().Add(-1 * time.Minute) // Fresh
	return event
}