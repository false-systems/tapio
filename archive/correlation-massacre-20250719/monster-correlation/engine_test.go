package correlation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockDataSource implements DataSource for testing
type MockDataSource struct {
	mock.Mock
	sourceType SourceType
	available  bool
}

func NewMockDataSource(sourceType SourceType, available bool) *MockDataSource {
	return &MockDataSource{
		sourceType: sourceType,
		available:  available,
	}
}

func (m *MockDataSource) GetType() SourceType {
	return m.sourceType
}

func (m *MockDataSource) IsAvailable() bool {
	return m.available
}

func (m *MockDataSource) GetData(ctx context.Context, dataType string, params map[string]interface{}) (interface{}, error) {
	args := m.Called(ctx, dataType, params)
	return args.Get(0), args.Error(1)
}

// MockRule implements Rule for testing
type MockRule struct {
	mock.Mock
	metadata RuleMetadata
}

func NewMockRule(id, name string, enabled bool, requirements []RuleRequirement) *MockRule {
	return &MockRule{
		metadata: RuleMetadata{
			ID:           id,
			Name:         name,
			Description:  "Test rule",
			Version:      "1.0.0",
			Author:       "test",
			Tags:         []string{"test"},
			Requirements: requirements,
			Enabled:      enabled,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}
}

func (m *MockRule) GetMetadata() RuleMetadata {
	return m.metadata
}

func (m *MockRule) CheckRequirements(ctx context.Context, data *DataCollection) error {
	args := m.Called(ctx, data)
	return args.Error(0)
}

func (m *MockRule) Execute(ctx context.Context, ruleCtx *RuleContext) ([]Finding, error) {
	args := m.Called(ctx, ruleCtx)
	return args.Get(0).([]Finding), args.Error(1)
}

func (m *MockRule) GetConfidenceFactors() []string {
	return []string{"test_factor"}
}

func (m *MockRule) Validate() error {
	args := m.Called()
	return args.Error(0)
}

// Test helper functions
func createTestDataCollection() *DataCollection {
	sources := map[SourceType]DataSource{
		SourceKubernetes: NewMockDataSource(SourceKubernetes, true),
		SourceEBPF:       NewMockDataSource(SourceEBPF, true),
		SourceMetrics:    NewMockDataSource(SourceMetrics, true),
	}
	return NewDataCollection(sources)
}

func createTestFinding(ruleID string, severity Severity, confidence float64) Finding {
	return Finding{
		ID:          "test-finding-1",
		RuleID:      ruleID,
		Title:       "Test Finding",
		Description: "Test finding description",
		Severity:    severity,
		Confidence:  confidence,
		Evidence:    []Evidence{},
		Tags:        []string{"test"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Metadata:    map[string]interface{}{},
	}
}

func TestNewEngine(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()

	engine := NewEngine(config, registry, dataCollection)

	assert.NotNil(t, engine)
	assert.Equal(t, config, engine.config)
	assert.Equal(t, registry, engine.registry)
	assert.Equal(t, dataCollection, engine.dataCollection)
	assert.NotNil(t, engine.metrics)
	assert.NotNil(t, engine.metrics.RuleMetrics)
	assert.Empty(t, engine.findings)
	assert.Empty(t, engine.executionHistory)
}

func TestEngine_Execute_NoRules(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	ctx := context.Background()
	findings, err := engine.Execute(ctx)

	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestEngine_Execute_SuccessfulRule(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create and register a mock rule
	mockRule := NewMockRule("test-rule", "Test Rule", true, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)

	testFinding := createTestFinding("test-rule", SeverityWarning, 0.8)
	mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{testFinding}, nil)

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	findings, err := engine.Execute(ctx)

	require.NoError(t, err)
	assert.Len(t, findings, 1)
	assert.Equal(t, testFinding.ID, findings[0].ID)
	assert.Equal(t, testFinding.RuleID, findings[0].RuleID)
	assert.Equal(t, testFinding.Severity, findings[0].Severity)
	assert.Equal(t, testFinding.Confidence, findings[0].Confidence)

	// Verify metrics were updated
	metrics := engine.GetMetrics()
	assert.Equal(t, int64(1), metrics.TotalExecutions)
	assert.Equal(t, int64(1), metrics.SuccessfulExecutions)
	assert.Equal(t, int64(0), metrics.FailedExecutions)
	assert.Equal(t, int64(1), metrics.TotalFindings)

	mockRule.AssertExpectations(t)
}

func TestEngine_Execute_RuleWithError(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create and register a mock rule that returns an error
	mockRule := NewMockRule("test-rule", "Test Rule", true, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{}, errors.New("rule execution failed"))

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	findings, err := engine.Execute(ctx)

	require.NoError(t, err)
	assert.Empty(t, findings)

	// Verify metrics were updated
	metrics := engine.GetMetrics()
	assert.Equal(t, int64(1), metrics.TotalExecutions)
	assert.Equal(t, int64(0), metrics.SuccessfulExecutions)
	assert.Equal(t, int64(1), metrics.FailedExecutions)
	assert.Equal(t, int64(0), metrics.TotalFindings)

	mockRule.AssertExpectations(t)
}

func TestEngine_Execute_RuleRequirementsNotMet(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create and register a mock rule with unmet requirements
	requirements := []RuleRequirement{
		{SourceType: SourceEBPF, DataType: "memory_stats", Required: true},
	}
	mockRule := NewMockRule("test-rule", "Test Rule", true, requirements)
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(errors.New("requirements not met"))

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	findings, err := engine.Execute(ctx)

	require.NoError(t, err)
	assert.Empty(t, findings) // Rule should be filtered out

	mockRule.AssertExpectations(t)
}

func TestEngine_Execute_MultipleRules(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create and register multiple mock rules
	mockRule1 := NewMockRule("rule-1", "Rule 1", true, []RuleRequirement{})
	mockRule1.On("Validate").Return(nil)
	mockRule1.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	finding1 := createTestFinding("rule-1", SeverityCritical, 0.9)
	mockRule1.On("Execute", mock.Anything, mock.Anything).Return([]Finding{finding1}, nil)

	mockRule2 := NewMockRule("rule-2", "Rule 2", true, []RuleRequirement{})
	mockRule2.On("Validate").Return(nil)
	mockRule2.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	finding2 := createTestFinding("rule-2", SeverityWarning, 0.7)
	mockRule2.On("Execute", mock.Anything, mock.Anything).Return([]Finding{finding2}, nil)

	err := registry.RegisterRule(mockRule1)
	require.NoError(t, err)
	err = registry.RegisterRule(mockRule2)
	require.NoError(t, err)

	ctx := context.Background()
	findings, err := engine.Execute(ctx)

	require.NoError(t, err)
	assert.Len(t, findings, 2)

	// Findings should be sorted by severity (critical first) and then by confidence
	assert.Equal(t, SeverityCritical, findings[0].Severity)
	assert.Equal(t, SeverityWarning, findings[1].Severity)

	mockRule1.AssertExpectations(t)
	mockRule2.AssertExpectations(t)
}

func TestEngine_ExecuteRule(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create and register a mock rule
	mockRule := NewMockRule("test-rule", "Test Rule", true, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	testFinding := createTestFinding("test-rule", SeverityError, 0.85)
	mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{testFinding}, nil)

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	findings, err := engine.ExecuteRule(ctx, "test-rule")

	require.NoError(t, err)
	assert.Len(t, findings, 1)
	assert.Equal(t, testFinding.ID, findings[0].ID)

	mockRule.AssertExpectations(t)
}

func TestEngine_ExecuteRule_NotFound(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	ctx := context.Background()
	findings, err := engine.ExecuteRule(ctx, "non-existent-rule")

	assert.Error(t, err)
	assert.Nil(t, findings)
	assert.IsType(t, &RuleNotFoundError{}, err)
}

func TestEngine_ExecuteRule_Disabled(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create and register a disabled mock rule
	mockRule := NewMockRule("test-rule", "Test Rule", false, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	findings, err := engine.ExecuteRule(ctx, "test-rule")

	assert.Error(t, err)
	assert.Nil(t, findings)
	assert.Contains(t, err.Error(), "disabled")

	mockRule.AssertExpectations(t)
}

func TestEngine_GetFindings(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Initially should be empty
	findings := engine.GetFindings()
	assert.Empty(t, findings)

	// Execute a rule to generate findings
	mockRule := NewMockRule("test-rule", "Test Rule", true, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	testFinding := createTestFinding("test-rule", SeverityInfo, 0.6)
	mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{testFinding}, nil)

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = engine.Execute(ctx)
	require.NoError(t, err)

	// Now should have findings
	findings = engine.GetFindings()
	assert.Len(t, findings, 1)
	assert.Equal(t, testFinding.ID, findings[0].ID)

	mockRule.AssertExpectations(t)
}

func TestEngine_GetFindingsByRule(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create multiple rules with different findings
	mockRule1 := NewMockRule("rule-1", "Rule 1", true, []RuleRequirement{})
	mockRule1.On("Validate").Return(nil)
	mockRule1.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	finding1 := createTestFinding("rule-1", SeverityError, 0.8)
	mockRule1.On("Execute", mock.Anything, mock.Anything).Return([]Finding{finding1}, nil)

	mockRule2 := NewMockRule("rule-2", "Rule 2", true, []RuleRequirement{})
	mockRule2.On("Validate").Return(nil)
	mockRule2.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	finding2 := createTestFinding("rule-2", SeverityWarning, 0.7)
	mockRule2.On("Execute", mock.Anything, mock.Anything).Return([]Finding{finding2}, nil)

	err := registry.RegisterRule(mockRule1)
	require.NoError(t, err)
	err = registry.RegisterRule(mockRule2)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = engine.Execute(ctx)
	require.NoError(t, err)

	// Test getting findings for specific rules
	rule1Findings := engine.GetFindingsByRule("rule-1")
	assert.Len(t, rule1Findings, 1)
	assert.Equal(t, "rule-1", rule1Findings[0].RuleID)

	rule2Findings := engine.GetFindingsByRule("rule-2")
	assert.Len(t, rule2Findings, 1)
	assert.Equal(t, "rule-2", rule2Findings[0].RuleID)

	nonExistentFindings := engine.GetFindingsByRule("non-existent")
	assert.Empty(t, nonExistentFindings)

	mockRule1.AssertExpectations(t)
	mockRule2.AssertExpectations(t)
}

func TestEngine_GetFindingsBySeverity(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create rules with different severity findings
	mockRule1 := NewMockRule("rule-1", "Rule 1", true, []RuleRequirement{})
	mockRule1.On("Validate").Return(nil)
	mockRule1.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	criticalFinding := createTestFinding("rule-1", SeverityCritical, 0.9)
	mockRule1.On("Execute", mock.Anything, mock.Anything).Return([]Finding{criticalFinding}, nil)

	mockRule2 := NewMockRule("rule-2", "Rule 2", true, []RuleRequirement{})
	mockRule2.On("Validate").Return(nil)
	mockRule2.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	warningFinding := createTestFinding("rule-2", SeverityWarning, 0.7)
	mockRule2.On("Execute", mock.Anything, mock.Anything).Return([]Finding{warningFinding}, nil)

	err := registry.RegisterRule(mockRule1)
	require.NoError(t, err)
	err = registry.RegisterRule(mockRule2)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = engine.Execute(ctx)
	require.NoError(t, err)

	// Test getting findings by severity
	criticalFindings := engine.GetFindingsBySeverity(SeverityCritical)
	assert.Len(t, criticalFindings, 1)
	assert.Equal(t, SeverityCritical, criticalFindings[0].Severity)

	warningFindings := engine.GetFindingsBySeverity(SeverityWarning)
	assert.Len(t, warningFindings, 1)
	assert.Equal(t, SeverityWarning, warningFindings[0].Severity)

	errorFindings := engine.GetFindingsBySeverity(SeverityError)
	assert.Empty(t, errorFindings)

	mockRule1.AssertExpectations(t)
	mockRule2.AssertExpectations(t)
}

func TestEngine_ExecutionModes(t *testing.T) {
	tests := []struct {
		name string
		mode ExecutionMode
	}{
		{"Sequential", ExecutionModeSequential},
		{"Parallel", ExecutionModeParallel},
		{"Adaptive", ExecutionModeAdaptive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultEngineConfig()
			config.ExecutionMode = tt.mode
			registry := NewRuleRegistry()
			dataCollection := createTestDataCollection()
			engine := NewEngine(config, registry, dataCollection)

			// Create a simple rule
			mockRule := NewMockRule("test-rule", "Test Rule", true, []RuleRequirement{})
			mockRule.On("Validate").Return(nil)
			mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
			testFinding := createTestFinding("test-rule", SeverityInfo, 0.5)
			mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{testFinding}, nil)

			err := registry.RegisterRule(mockRule)
			require.NoError(t, err)

			ctx := context.Background()
			findings, err := engine.Execute(ctx)

			require.NoError(t, err)
			assert.Len(t, findings, 1)

			mockRule.AssertExpectations(t)
		})
	}
}

func TestEngine_ClearFindings(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Execute a rule to generate findings
	mockRule := NewMockRule("test-rule", "Test Rule", true, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	testFinding := createTestFinding("test-rule", SeverityWarning, 0.8)
	mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{testFinding}, nil)

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = engine.Execute(ctx)
	require.NoError(t, err)

	// Verify findings exist
	findings := engine.GetFindings()
	assert.Len(t, findings, 1)

	// Clear findings
	engine.ClearFindings()

	// Verify findings are cleared
	findings = engine.GetFindings()
	assert.Empty(t, findings)

	mockRule.AssertExpectations(t)
}

func TestEngine_GetExecutionHistory(t *testing.T) {
	config := DefaultEngineConfig()
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Initially should be empty
	history := engine.GetExecutionHistory()
	assert.Empty(t, history)

	// Execute a rule
	mockRule := NewMockRule("test-rule", "Test Rule", true, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	testFinding := createTestFinding("test-rule", SeverityInfo, 0.6)
	mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{testFinding}, nil)

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = engine.Execute(ctx)
	require.NoError(t, err)

	// Now should have execution history
	history = engine.GetExecutionHistory()
	assert.Len(t, history, 1)
	assert.Equal(t, mockRule, history[0].Rule)
	assert.Len(t, history[0].Findings, 1)
	assert.Nil(t, history[0].Error)
	assert.Greater(t, history[0].Duration, time.Duration(0))

	mockRule.AssertExpectations(t)
}

func TestEngine_TimeoutHandling(t *testing.T) {
	config := DefaultEngineConfig()
	config.Timeout = 100 * time.Millisecond
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create a rule that takes longer than the timeout
	mockRule := NewMockRule("slow-rule", "Slow Rule", true, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)
	mockRule.On("Execute", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		time.Sleep(200 * time.Millisecond) // Sleep longer than timeout
	}).Return([]Finding{}, nil)

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	start := time.Now()
	findings, err := engine.Execute(ctx)
	duration := time.Since(start)

	// Should complete around the timeout period (allowing some variance)
	assert.Less(t, duration, 250*time.Millisecond)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestEngine_RetryMechanism(t *testing.T) {
	config := DefaultEngineConfig()
	config.RetryAttempts = 2
	config.RetryDelay = 10 * time.Millisecond
	registry := NewRuleRegistry()
	dataCollection := createTestDataCollection()
	engine := NewEngine(config, registry, dataCollection)

	// Create a rule that fails twice then succeeds
	mockRule := NewMockRule("retry-rule", "Retry Rule", true, []RuleRequirement{})
	mockRule.On("Validate").Return(nil)
	mockRule.On("CheckRequirements", mock.Anything, mock.Anything).Return(nil)

	mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{}, errors.New("temporary failure")).Twice()
	mockRule.On("Execute", mock.Anything, mock.Anything).Return([]Finding{createTestFinding("retry-rule", SeverityInfo, 0.8)}, nil).Once()

	err := registry.RegisterRule(mockRule)
	require.NoError(t, err)

	ctx := context.Background()
	findings, err := engine.Execute(ctx)

	require.NoError(t, err)
	assert.Len(t, findings, 1)

	mockRule.AssertExpectations(t)
}
