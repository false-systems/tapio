# Intelligence Package Test Implementation Plan

## Critical Test Cases to Implement Immediately

### 1. Types Package Tests (PRIORITY: CRITICAL)

```go
// types_test.go - Add these tests immediately

func TestEventContext_Creation(t *testing.T) {
    tests := []struct {
        name      string
        eventType string
        source    string
        eventID   string
    }{
        {"basic event", "syscall", "kernel", "evt-001"},
        {"empty eventID", "dns_query", "dns", ""},
        {"deployment event", "deployment", "kubeapi", "dep-123"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ctx := NewEventContextFromEvent(tt.eventType, tt.source, tt.eventID)
            require.NotNil(t, ctx)
            assert.Equal(t, tt.eventType, ctx.EventType)
            assert.Equal(t, tt.source, ctx.Source)
            assert.Equal(t, tt.eventID, ctx.EventID)
        })
    }
}

func TestEventContext_Setters(t *testing.T) {
    ctx := NewEventContextFromEvent("test", "source", "id")
    
    // Test all setters
    pid := uint32(1234)
    ctx.SetPID(pid)
    assert.Equal(t, pid, *ctx.PID)
    
    containerID := "container-123"
    ctx.SetContainerID(containerID)
    assert.Equal(t, containerID, *ctx.ContainerID)
    
    podName := "test-pod"
    ctx.SetPodName(podName)
    assert.Equal(t, podName, *ctx.PodName)
    
    // Test all other setters...
}

func TestEventContext_ToMap(t *testing.T) {
    ctx := NewEventContextFromEvent("syscall", "kernel", "evt-001")
    pid := uint32(1234)
    ctx.SetPID(pid)
    ctx.SetPodName("test-pod")
    ctx.SetAction("connect")
    
    m := ctx.ToMap()
    require.NotNil(t, m)
    assert.Equal(t, "syscall", m["event_type"])
    assert.Equal(t, "kernel", m["source"])
    assert.Equal(t, uint32(1234), m["pid"])
    assert.Equal(t, "test-pod", m["pod_name"])
    assert.Equal(t, "connect", m["action"])
}

func TestConditionValue_AllTypes(t *testing.T) {
    tests := []struct {
        name     string
        create   func() *ConditionValue
        expected interface{}
        isNil    bool
    }{
        {
            name:     "string value",
            create:   func() *ConditionValue { return NewStringConditionValue("test") },
            expected: "test",
            isNil:    false,
        },
        {
            name:     "int value",
            create:   func() *ConditionValue { return NewIntConditionValue(42) },
            expected: int64(42),
            isNil:    false,
        },
        {
            name:     "float value",
            create:   func() *ConditionValue { return NewFloatConditionValue(3.14) },
            expected: 3.14,
            isNil:    false,
        },
        {
            name:     "bool value",
            create:   func() *ConditionValue { return NewBoolConditionValue(true) },
            expected: true,
            isNil:    false,
        },
        {
            name:     "nil value",
            create:   func() *ConditionValue { return NewNilConditionValue() },
            expected: nil,
            isNil:    true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cv := tt.create()
            require.NotNil(t, cv)
            assert.Equal(t, tt.isNil, cv.IsNil())
            if !tt.isNil {
                assert.Equal(t, tt.expected, cv.ToInterface())
            }
        })
    }
}

func TestConditionValue_Comparisons(t *testing.T) {
    // Test Equals
    v1 := NewStringConditionValue("test")
    v2 := NewStringConditionValue("test")
    v3 := NewStringConditionValue("other")
    
    assert.True(t, v1.Equals(v2))
    assert.False(t, v1.Equals(v3))
    
    // Test Contains
    v4 := NewStringConditionValue("hello world")
    v5 := NewStringConditionValue("world")
    assert.True(t, v4.Contains(v5))
    
    // Test numeric comparisons
    n1 := NewIntConditionValue(10)
    n2 := NewIntConditionValue(5)
    assert.False(t, n1.Equals(n2))
}
```

### 2. Pattern Matching Tests (PRIORITY: CRITICAL)

```go
// behavior/pattern_matcher_test.go - Add these tests

func TestPatternMatcher_MatchPattern(t *testing.T) {
    logger := zap.NewNop()
    matcher := NewPatternMatcher(logger)
    
    // Create test pattern
    pattern := &Pattern{
        ID:          "test-pattern",
        Name:        "Test Pattern",
        Description: "Test pattern for matching",
        Category:    "security",
        Severity:    "high",
        Conditions: []Condition{
            {
                Field:    "type",
                Operator: "equals",
                Value:    "syscall",
            },
            {
                Field:    "action",
                Operator: "equals",
                Value:    "connect",
            },
        },
    }
    
    matcher.UpdatePatterns([]*Pattern{pattern})
    
    // Test matching event
    event := &domain.ObservationEvent{
        Type:   "syscall",
        Action: stringPtr("connect"),
    }
    
    matches := matcher.Match(event)
    require.Len(t, matches, 1)
    assert.Equal(t, "test-pattern", matches[0].PatternID)
}

func TestPatternMatcher_EvaluateCondition(t *testing.T) {
    tests := []struct {
        name      string
        condition Condition
        value     interface{}
        expected  bool
    }{
        {
            name: "equals string",
            condition: Condition{
                Operator: "equals",
                Value:    "test",
            },
            value:    "test",
            expected: true,
        },
        {
            name: "contains string",
            condition: Condition{
                Operator: "contains",
                Value:    "world",
            },
            value:    "hello world",
            expected: true,
        },
        {
            name: "threshold greater",
            condition: Condition{
                Operator: "threshold",
                Value:    map[string]interface{}{"gt": 10},
            },
            value:    15,
            expected: true,
        },
        {
            name: "regex match",
            condition: Condition{
                Operator: "regex",
                Value:    "^test.*",
            },
            value:    "testing",
            expected: true,
        },
    }
    
    // Test each condition evaluation
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Implementation needed
        })
    }
}

func TestPatternMatcher_CalculateConfidence(t *testing.T) {
    tests := []struct {
        name               string
        matchedConditions  int
        totalConditions    int
        patternConfidence  float64
        expectedMin        float64
        expectedMax        float64
    }{
        {"all conditions matched", 5, 5, 0.9, 0.85, 0.95},
        {"partial match", 3, 5, 0.8, 0.4, 0.6},
        {"single condition", 1, 1, 0.7, 0.65, 0.75},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test confidence calculation
        })
    }
}
```

### 3. Deployment Processor Error Tests (PRIORITY: HIGH)

```go
// deployment_processor_test.go - Add these tests

func TestProcessRawEvent_InvalidJSON(t *testing.T) {
    logger := zap.NewNop()
    processor, err := NewDeploymentProcessor(logger)
    require.NoError(t, err)
    
    event := domain.RawEvent{
        Type:      "kubeapi",
        Timestamp: time.Now(),
        Data:      []byte("invalid json{"),
    }
    
    err = processor.ProcessRawEvent(context.Background(), event)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "unmarshal")
}

func TestProcessRawEvent_ChannelFull(t *testing.T) {
    logger := zap.NewNop()
    processor, err := NewDeploymentProcessor(logger)
    require.NoError(t, err)
    
    // Fill the channel
    for i := 0; i < 1000; i++ {
        select {
        case processor.deploymentEvents <- &domain.DeploymentEvent{}:
        default:
            // Channel full
        }
    }
    
    // Try to process with full channel
    event := createValidDeploymentEvent()
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    
    err = processor.ProcessRawEvent(ctx, event)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "context deadline")
}

func TestExtractDeployment_EdgeCases(t *testing.T) {
    tests := []struct {
        name        string
        deployment  *appsv1.Deployment
        shouldError bool
        validate    func(*testing.T, *domain.DeploymentMetadata)
    }{
        {
            name:        "nil deployment",
            deployment:  nil,
            shouldError: true,
        },
        {
            name: "deployment with nil spec",
            deployment: &appsv1.Deployment{
                ObjectMeta: metav1.ObjectMeta{Name: "test"},
            },
            shouldError: false,
            validate: func(t *testing.T, m *domain.DeploymentMetadata) {
                assert.Equal(t, int32(0), m.NewReplicas)
            },
        },
        {
            name: "deployment with zero replicas",
            deployment: &appsv1.Deployment{
                Spec: appsv1.DeploymentSpec{
                    Replicas: int32Ptr(0),
                },
            },
            shouldError: false,
            validate: func(t *testing.T, m *domain.DeploymentMetadata) {
                assert.Equal(t, int32(0), m.NewReplicas)
            },
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test extraction
        })
    }
}
```

### 4. Predictor Tests (PRIORITY: HIGH)

```go
// behavior/predictor_test.go - Create new file

func TestPredictor_GeneratePrediction(t *testing.T) {
    logger := zap.NewNop()
    predictor := NewPredictor(logger, nil, nil)
    
    tests := []struct {
        name           string
        event          *domain.ObservationEvent
        expectedType   string
        minConfidence  float64
        maxConfidence  float64
    }{
        {
            name: "high risk syscall",
            event: &domain.ObservationEvent{
                Type:   "syscall",
                Action: stringPtr("execve"),
                Target: stringPtr("/bin/sh"),
            },
            expectedType:  "security_threat",
            minConfidence: 0.7,
            maxConfidence: 1.0,
        },
        {
            name: "normal dns query",
            event: &domain.ObservationEvent{
                Type:   "dns_query",
                Action: stringPtr("query"),
                Target: stringPtr("google.com"),
            },
            expectedType:  "normal_behavior",
            minConfidence: 0.8,
            maxConfidence: 1.0,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ctx := context.Background()
            prediction, err := predictor.GeneratePrediction(ctx, tt.event, nil)
            
            if tt.expectedType == "" {
                assert.Nil(t, prediction)
            } else {
                require.NoError(t, err)
                require.NotNil(t, prediction)
                assert.True(t, prediction.Confidence >= tt.minConfidence)
                assert.True(t, prediction.Confidence <= tt.maxConfidence)
            }
        })
    }
}

func TestPredictor_BuildEvidence(t *testing.T) {
    // Test evidence collection from multiple sources
}

func TestPredictor_AdjustConfidenceFromFeedback(t *testing.T) {
    // Test feedback loop
}
```

### 5. Performance Benchmarks (PRIORITY: MEDIUM)

```go
// benchmark_test.go - Create new file

func BenchmarkPatternMatching(b *testing.B) {
    logger := zap.NewNop()
    matcher := NewPatternMatcher(logger)
    
    // Load realistic patterns
    patterns := generateTestPatterns(100)
    matcher.UpdatePatterns(patterns)
    
    // Create test event
    event := &domain.ObservationEvent{
        Type:      "syscall",
        Action:    stringPtr("connect"),
        Target:    stringPtr("10.0.0.1:443"),
        PodName:   stringPtr("test-pod"),
        Namespace: stringPtr("default"),
    }
    
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _ = matcher.Match(event)
        }
    })
    
    b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/sec")
}

func BenchmarkSafetyScoring(b *testing.B) {
    logger := zap.NewNop()
    scorer, _ := NewSafetyScorer(logger, DefaultScoringConfig())
    
    event := &domain.DeploymentEvent{
        Timestamp: time.Now(),
        Namespace: "default",
        Name:      "test-app",
        Action:    domain.DeploymentUpdated,
        Metadata: domain.DeploymentMetadata{
            NewImage:    "nginx:1.20",
            OldImage:    "nginx:1.19",
            NewReplicas: 5,
            OldReplicas: 3,
        },
    }
    
    ctx := context.Background()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = scorer.CalculateScore(ctx, event)
    }
    
    b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "scores/sec")
}

func BenchmarkDeploymentProcessing(b *testing.B) {
    logger := zap.NewNop()
    processor, _ := NewDeploymentProcessor(logger)
    
    // Create realistic K8s event
    event := createRealisticDeploymentEvent()
    
    ctx := context.Background()
    
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _ = processor.ProcessRawEvent(ctx, event)
        }
    })
    
    b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/sec")
}

func BenchmarkEventContextCreation(b *testing.B) {
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            ctx := NewEventContextFromEvent("syscall", "kernel", "evt-001")
            ctx.SetPID(1234)
            ctx.SetPodName("test-pod")
            ctx.SetAction("connect")
            _ = ctx.ToMap()
        }
    })
    
    b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "contexts/sec")
}
```

### 6. Integration & Chaos Tests (PRIORITY: MEDIUM)

```go
// chaos_test.go - Create new file

func TestChaos_PatternLoaderFailure(t *testing.T) {
    // Simulate pattern loading failures
    // Verify graceful degradation
}

func TestChaos_BackpressureOverload(t *testing.T) {
    // Simulate system overload
    // Verify backpressure mechanisms work
}

func TestChaos_CircuitBreakerCascade(t *testing.T) {
    // Simulate cascading failures
    // Verify circuit breaker prevents system collapse
}

func TestIntegration_FullPipeline(t *testing.T) {
    // Test complete event flow:
    // Raw Event -> Deployment Processor -> Safety Scorer -> Behavior Engine
}
```

## Implementation Priority Order

### Week 1 (MUST COMPLETE)
1. ✅ All types.go tests (EventContext, ConditionValue)
2. ✅ Pattern matching core tests
3. ✅ Deployment processor error tests
4. ✅ Basic predictor tests

### Week 2
1. Performance benchmarks
2. Integration tests
3. Chaos tests
4. Additional edge cases

### Week 3
1. Load testing suite
2. Memory leak tests
3. Concurrent safety tests
4. Documentation

## Test Data Fixtures Needed

```go
// testdata/fixtures.go

func CreateTestPattern() *Pattern
func CreateTestObservationEvent() *domain.ObservationEvent
func CreateTestDeploymentEvent() *domain.DeploymentEvent
func GenerateTestPatterns(count int) []*Pattern
func GenerateTestEvents(count int) []*domain.ObservationEvent
```

## Verification Commands

```bash
# Run all new tests
go test -v ./...

# Check coverage improvement
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep "total:"

# Run benchmarks
go test -bench=. -benchmem ./...

# Race condition check
go test -race ./...

# Generate coverage report
go tool cover -html=coverage.out -o coverage.html
```

## Success Criteria

- [ ] types.go coverage > 90%
- [ ] Overall package coverage > 80%
- [ ] All critical paths tested
- [ ] All error scenarios covered
- [ ] Performance benchmarks established
- [ ] No race conditions detected
- [ ] Integration tests passing
- [ ] Chaos tests implemented

---
*This plan provides concrete, implementable test cases that can be coded immediately to address the critical gaps in the intelligence package testing.*