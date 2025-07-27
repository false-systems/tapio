# ML Integration with NATS Async Pipelines

## Overview

The async pipeline architecture makes ML integration seamless - ML models become just another async stage that can be added, removed, or updated without affecting the core system.

## ML Pipeline Architecture

```
                    NATS Event Streams
                           │
     ┌─────────────────────┼─────────────────────┐
     │                     │                     │
     ▼                     ▼                     ▼
┌──────────┐      ┌──────────────┐      ┌──────────────┐
│ Real-time│      │   Batch ML   │      │  Training    │
│    ML    │      │  Processing  │      │  Pipeline    │
└──────────┘      └──────────────┘      └──────────────┘
     │                     │                     │
     ▼                     ▼                     ▼
  Insights          Predictions           Updated Models
```

## ML Integration Patterns

### 1. Real-time ML Inference (Async)

```go
type MLInferencePipeline struct {
    nc     *nats.Conn
    js     nats.JetStreamContext
    models map[string]MLModel
}

func (p *MLInferencePipeline) Start() error {
    // Subscribe to enriched events for ML inference
    _, err := p.js.QueueSubscribe("events.enriched.>", "ml-inference-workers",
        func(msg *nats.Msg) {
            // Non-blocking ML inference
            go p.inferAsync(msg)
        },
        nats.Durable("ml-inference"),
        nats.MaxAckPending(100), // Control concurrency
    )
    return err
}

func (p *MLInferencePipeline) inferAsync(msg *nats.Msg) {
    event := &domain.UnifiedEvent{}
    json.Unmarshal(msg.Data, event)
    
    // Extract features from multi-dimensional event
    features := p.extractFeatures(event)
    
    // Run multiple models in parallel
    predictions := make(chan MLPrediction, 3)
    
    go p.anomalyDetection(features, predictions)
    go p.rootCauseAnalysis(features, predictions)
    go p.impactPrediction(features, predictions)
    
    // Collect predictions
    for i := 0; i < 3; i++ {
        pred := <-predictions
        p.enrichEventWithML(event, pred)
    }
    
    // Publish ML-enriched event
    data, _ := json.Marshal(event)
    p.js.Publish("events.ml-enriched."+p.getSubject(event), data)
    
    msg.Ack()
}
```

### 2. Feature Extraction from Multi-Dimensional Events

```go
func (p *MLInferencePipeline) extractFeatures(event *domain.UnifiedEvent) *FeatureVector {
    features := &FeatureVector{
        Timestamp: event.Timestamp,
        Features:  make(map[string]float64),
    }
    
    // 1. Semantic features (from embedding)
    if event.Semantic != nil && event.Semantic.Embedding != nil {
        for i, val := range event.Semantic.Embedding {
            features.Features[fmt.Sprintf("semantic_%d", i)] = float64(val)
        }
        features.Features["semantic_confidence"] = event.Semantic.Confidence
    }
    
    // 2. Impact features
    if event.Impact != nil {
        features.Features["business_impact"] = event.Impact.BusinessImpact
        features.Features["customer_facing"] = boolToFloat(event.Impact.CustomerFacing)
        features.Features["slo_impact"] = boolToFloat(event.Impact.SLOImpact)
    }
    
    // 3. Temporal features
    if event.Temporal != nil {
        features.Features["frequency"] = event.Temporal.Frequency
        features.Features["periodicity"] = event.Temporal.Periodicity
    }
    
    // 4. K8s features
    if event.KubernetesData != nil {
        features.Features["is_warning"] = boolToFloat(event.KubernetesData.EventType == "Warning")
        features.Features["is_pod_event"] = boolToFloat(event.KubernetesData.ObjectKind == "Pod")
    }
    
    // 5. Behavioral features
    if event.Anomaly != nil {
        features.Features["anomaly_score"] = float64(event.Anomaly.AnomalyScore)
        features.Features["z_score"] = float64(event.Anomaly.BaselineComparison.ZScore)
    }
    
    return features
}
```

### 3. ML Models as Async Services

```go
// Anomaly Detection Model (runs independently)
type AnomalyDetectionService struct {
    nc    *nats.Conn
    model *onnx.Model // ONNX for portability
}

func (s *AnomalyDetectionService) Start() error {
    // Subscribe to feature vectors
    s.nc.Subscribe("ml.features.anomaly", func(msg *nats.Msg) {
        features := &FeatureVector{}
        json.Unmarshal(msg.Data, features)
        
        // Run inference
        prediction := s.model.Predict(features.ToTensor())
        
        // Respond with prediction
        result := &AnomalyPrediction{
            Score:      prediction[0],
            Confidence: prediction[1],
            Type:       s.classifyAnomaly(prediction),
        }
        
        data, _ := json.Marshal(result)
        msg.Respond(data)
    })
    
    return nil
}
```

### 4. Batch ML Processing

```go
type BatchMLProcessor struct {
    nc *nats.Conn
    js nats.JetStreamContext
}

func (p *BatchMLProcessor) Start() error {
    // Process hourly batches for complex ML
    ticker := time.NewTicker(1 * time.Hour)
    
    go func() {
        for range ticker.C {
            p.processBatch()
        }
    }()
    
    return nil
}

func (p *BatchMLProcessor) processBatch() {
    // Fetch last hour of events
    consumer, _ := p.js.CreateConsumer("ENRICHED_EVENTS", &nats.ConsumerConfig{
        DeliverPolicy: nats.DeliverByStartTimePolicy,
        OptStartTime:  time.Now().Add(-1 * time.Hour),
    })
    
    // Collect events
    var events []*domain.UnifiedEvent
    msgs, _ := consumer.Fetch(10000)
    
    for _, msg := range msgs {
        event := &domain.UnifiedEvent{}
        json.Unmarshal(msg.Data, event)
        events = append(events, event)
        msg.Ack()
    }
    
    // Run batch ML algorithms
    patterns := p.detectComplexPatterns(events)
    clusters := p.clusterEvents(events)
    forecast := p.forecastTrends(events)
    
    // Publish batch insights
    p.publishBatchInsights(patterns, clusters, forecast)
}
```

### 5. ML Model Updates (Zero Downtime)

```go
type ModelManager struct {
    nc      *nats.Conn
    js      nats.JetStreamContext
    models  sync.Map // Thread-safe model storage
}

func (m *ModelManager) Start() error {
    // Subscribe to model updates
    m.nc.Subscribe("ml.models.update", func(msg *nats.Msg) {
        update := &ModelUpdate{}
        json.Unmarshal(msg.Data, update)
        
        // Load new model (async)
        go m.loadModelAsync(update)
    })
    
    return nil
}

func (m *ModelManager) loadModelAsync(update *ModelUpdate) {
    // Download model from object store
    modelData, _ := m.downloadModel(update.URL)
    
    // Load and validate
    newModel, err := onnx.Load(modelData)
    if err != nil {
        m.nc.Publish("ml.models.error", []byte(err.Error()))
        return
    }
    
    // A/B test new model
    if update.ABTest {
        m.startABTest(update.Name, newModel)
    } else {
        // Atomic swap
        m.models.Store(update.Name, newModel)
        m.nc.Publish("ml.models.loaded", []byte(update.Name))
    }
}
```

### 6. Training Pipeline Integration

```go
type TrainingPipeline struct {
    nc *nats.Conn
    js nats.JetStreamContext
}

func (p *TrainingPipeline) Start() error {
    // Collect training data from correlation feedback
    m.js.Subscribe("insights.feedback.>", func(msg *nats.Msg) {
        feedback := &CorrelationFeedback{}
        json.Unmarshal(msg.Data, feedback)
        
        // Store for training
        p.storeTrainingData(feedback)
        
        // Trigger retraining if needed
        if p.shouldRetrain() {
            p.triggerTraining()
        }
    })
    
    return nil
}

func (p *TrainingPipeline) triggerTraining() {
    // Publish training job
    job := &TrainingJob{
        ID:        generateJobID(),
        ModelType: "correlation-predictor",
        Dataset:   "last-7-days",
        Config: TrainingConfig{
            Epochs:     100,
            BatchSize:  256,
            LearningRate: 0.001,
        },
    }
    
    data, _ := json.Marshal(job)
    p.nc.Publish("ml.training.submit", data)
}
```

## ML Use Cases with Multi-Dimensional Events

### 1. Root Cause Analysis
```go
// Uses: Trace, Correlation, Temporal dimensions
func (m *MLModels) PredictRootCause(events []*UnifiedEvent) *RootCause {
    // Build causal graph from trace hierarchy
    graph := buildCausalGraph(events)
    
    // Apply GNN for root cause prediction
    features := extractGraphFeatures(graph)
    prediction := m.rootCauseGNN.Predict(features)
    
    return &RootCause{
        EventID:    prediction.RootEventID,
        Confidence: prediction.Confidence,
        Path:       prediction.CausalPath,
    }
}
```

### 2. Impact Prediction
```go
// Uses: Impact, Entity, Semantic dimensions
func (m *MLModels) PredictImpact(event *UnifiedEvent) *ImpactPrediction {
    // Extract multi-dimensional features
    features := []float64{
        event.Impact.BusinessImpact,
        float64(len(event.Impact.AffectedServices)),
        event.Semantic.Confidence,
        // ... more features
    }
    
    // Predict future impact
    prediction := m.impactModel.Predict(features)
    
    return &ImpactPrediction{
        FutureImpact: prediction[0],
        Duration:     time.Duration(prediction[1]) * time.Minute,
        Spread:       prediction[2], // How many services affected
    }
}
```

### 3. Pattern Discovery
```go
// Uses all dimensions for unsupervised learning
func (m *MLModels) DiscoverPatterns(events []*UnifiedEvent) []*Pattern {
    // Convert to feature matrix
    matrix := m.eventsToFeatureMatrix(events)
    
    // Apply clustering
    clusters := m.dbscan.Fit(matrix)
    
    // Extract patterns from clusters
    patterns := make([]*Pattern, 0)
    for _, cluster := range clusters {
        pattern := m.extractPattern(cluster)
        patterns = append(patterns, pattern)
    }
    
    return patterns
}
```

## Benefits of ML + NATS Architecture

1. **Plug & Play ML**:
   - Add new models by subscribing to streams
   - Remove models by unsubscribing
   - No code changes to core system

2. **Independent Scaling**:
   - Scale ML workers based on load
   - Different models can scale differently
   - GPU nodes only for ML workers

3. **A/B Testing**:
   - Run multiple models in parallel
   - Compare results in real-time
   - Gradual rollout of new models

4. **Fault Isolation**:
   - ML failures don't affect correlation
   - Can disable ML during issues
   - Fallback to rule-based correlation

5. **Real-time + Batch**:
   - Fast models for real-time
   - Complex models in batch
   - Best of both worlds

## Implementation Timeline

### Week 1: Basic ML Pipeline
- Real-time anomaly detection
- Feature extraction service
- Model serving infrastructure

### Week 2: Advanced Models
- Root cause analysis
- Impact prediction
- Pattern discovery

### Week 3: Training Pipeline
- Feedback collection
- Automated retraining
- Model versioning

### Week 4: Production Features
- A/B testing framework
- Model monitoring
- Performance optimization

The beauty is that all of this can be added incrementally without touching the core correlation system!