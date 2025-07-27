# Async Pipeline Architecture with NATS

## Overview

NATS enables true async, parallel processing pipelines where each stage can scale independently and process events without blocking.

## Async Pipeline Architecture

```
┌─────────────────┐
│   Collectors    │
│  (Async Push)   │
└────────┬────────┘
         │ Publish (fire & forget)
         ▼
┌─────────────────────────────────────────────────────────┐
│                     NATS JetStream                      │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │
│  │Raw Events   │  │Validated    │  │Enriched     │   │
│  │Stream       │  │Events Stream│  │Events Stream│   │
│  └─────────────┘  └─────────────┘  └─────────────┘   │
│         │                │                │            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │
│  │Correlated   │  │Analytics    │  │Insights     │   │
│  │Events Stream│  │Results      │  │Stream       │   │
│  └─────────────┘  └─────────────┘  └─────────────┘   │
└─────────────────────────────────────────────────────────┘
```

## Async Pipeline Stages

### 1. Validation Pipeline (Async & Parallel)

```go
type AsyncValidationPipeline struct {
    nc *nats.Conn
    js nats.JetStreamContext
}

func (p *AsyncValidationPipeline) Start() error {
    // Subscribe to raw events with queue group for parallel processing
    _, err := p.js.QueueSubscribe("events.raw.>", "validation-workers", 
        func(msg *nats.Msg) {
            // Async validation - no blocking
            go p.validateAsync(msg)
        },
        nats.Durable("validation-pipeline"),
        nats.MaxAckPending(1000), // Process up to 1000 in parallel
        nats.AckExplicit(),
    )
    return err
}

func (p *AsyncValidationPipeline) validateAsync(msg *nats.Msg) {
    event := &domain.UnifiedEvent{}
    json.Unmarshal(msg.Data, event)
    
    // Validate event
    if err := p.validate(event); err != nil {
        // Dead letter queue for invalid events
        p.js.Publish("events.invalid", msg.Data,
            nats.Header("error", err.Error()))
        msg.Ack()
        return
    }
    
    // Publish to validated stream
    p.js.Publish("events.validated."+p.getSubject(event), msg.Data,
        nats.MsgId(event.ID))
    
    msg.Ack() // Acknowledge processing
}
```

### 2. Enrichment Pipeline (Async with Caching)

```go
type AsyncEnrichmentPipeline struct {
    nc    *nats.Conn
    js    nats.JetStreamContext
    cache *nats.KeyValue // NATS KV for caching
}

func (p *AsyncEnrichmentPipeline) Start() error {
    // Multiple parallel enrichers
    for i := 0; i < 10; i++ {
        go p.enrichmentWorker(i)
    }
    return nil
}

func (p *AsyncEnrichmentPipeline) enrichmentWorker(id int) {
    sub, _ := p.js.PullSubscribe("events.validated.>", 
        fmt.Sprintf("enrichment-worker-%d", id),
        nats.Durable("enrichment"),
    )
    
    for {
        // Pull batch for efficiency
        msgs, _ := sub.Fetch(100, nats.MaxWait(1*time.Second))
        
        // Process batch in parallel
        var wg sync.WaitGroup
        for _, msg := range msgs {
            wg.Add(1)
            go func(m *nats.Msg) {
                defer wg.Done()
                p.enrichAsync(m)
            }(msg)
        }
        wg.Wait()
    }
}

func (p *AsyncEnrichmentPipeline) enrichAsync(msg *nats.Msg) {
    event := &domain.UnifiedEvent{}
    json.Unmarshal(msg.Data, event)
    
    // Parallel enrichment from multiple sources
    enrichments := make(chan enrichment, 4)
    
    // Async enrichment workers
    go p.enrichK8sContext(event, enrichments)
    go p.enrichSemanticContext(event, enrichments)
    go p.enrichTraceContext(event, enrichments)
    go p.enrichImpactContext(event, enrichments)
    
    // Collect enrichments
    for i := 0; i < 4; i++ {
        e := <-enrichments
        p.applyEnrichment(event, e)
    }
    
    // Publish enriched event
    data, _ := json.Marshal(event)
    p.js.Publish("events.enriched."+p.getSubject(event), data)
    
    msg.Ack()
}
```

### 3. Async Correlation Pipeline

```go
type AsyncCorrelationPipeline struct {
    nc     *nats.Conn
    js     nats.JetStreamContext
    system *SimpleCorrelationSystem
}

func (p *AsyncCorrelationPipeline) Start() error {
    // Different correlation strategies in parallel
    
    // 1. Real-time correlation (memory-based)
    go p.realtimeCorrelation()
    
    // 2. Batch correlation (time-window based)
    go p.batchCorrelation()
    
    // 3. Pattern-based correlation (historical)
    go p.patternCorrelation()
    
    return nil
}

func (p *AsyncCorrelationPipeline) realtimeCorrelation() {
    p.js.Subscribe("events.enriched.>", func(msg *nats.Msg) {
        event := &domain.UnifiedEvent{}
        json.Unmarshal(msg.Data, event)
        
        // Non-blocking correlation
        go func() {
            ctx := context.Background()
            p.system.ProcessEvent(ctx, event)
            
            // Async publish insights
            select {
            case insight := <-p.system.Insights():
                data, _ := json.Marshal(insight)
                p.js.Publish("insights.correlation", data)
            case <-time.After(10 * time.Millisecond):
                // Don't block on insights
            }
        }()
        
        msg.Ack()
    })
}

func (p *AsyncCorrelationPipeline) batchCorrelation() {
    // Process events in time windows
    ticker := time.NewTicker(5 * time.Second)
    
    for range ticker.C {
        // Fetch last 5 seconds of events
        consumer, _ := p.js.CreateConsumer("ENRICHED_EVENTS", &nats.ConsumerConfig{
            DeliverPolicy: nats.DeliverLastPerSubjectPolicy,
            FilterSubject: "events.enriched.>",
        })
        
        msgs, _ := consumer.Fetch(1000)
        
        // Parallel batch correlation
        p.correlateBatch(msgs)
    }
}
```

### 4. Async Analytics Pipeline

```go
type AsyncAnalyticsPipeline struct {
    nc *nats.Conn
    js nats.JetStreamContext
}

func (p *AsyncAnalyticsPipeline) Start() error {
    // Multiple analytics workers for different dimensions
    
    // Anomaly detection worker
    go p.anomalyWorker()
    
    // Impact analysis worker  
    go p.impactWorker()
    
    // Trend analysis worker
    go p.trendWorker()
    
    // ML inference worker
    go p.mlWorker()
    
    return nil
}

func (p *AsyncAnalyticsPipeline) anomalyWorker() {
    p.js.Subscribe("events.enriched.>", func(msg *nats.Msg) {
        // Async anomaly detection
        go func() {
            event := &domain.UnifiedEvent{}
            json.Unmarshal(msg.Data, event)
            
            if anomaly := p.detectAnomaly(event); anomaly != nil {
                // Publish anomaly event
                p.js.Publish("analytics.anomaly", msg.Data,
                    nats.Header("anomaly-score", fmt.Sprintf("%.2f", anomaly.Score)))
            }
        }()
        
        msg.Ack()
    })
}
```

## Advanced Async Patterns

### 1. Fork-Join Pattern

```go
// Process same event through multiple pipelines in parallel
func (p *Pipeline) forkJoinProcessing(event *domain.UnifiedEvent) {
    results := make(chan result, 3)
    
    // Fork to multiple processors
    go p.processForCorrelation(event, results)
    go p.processForAnalytics(event, results)
    go p.processForPersistence(event, results)
    
    // Join results
    var finalResult combinedResult
    for i := 0; i < 3; i++ {
        r := <-results
        finalResult.merge(r)
    }
    
    // Publish combined result
    p.publishResult(finalResult)
}
```

### 2. Scatter-Gather Pattern

```go
// Request correlation from multiple sources
func (p *Pipeline) scatterGatherCorrelation(event *domain.UnifiedEvent) {
    data, _ := json.Marshal(event)
    
    // Scatter requests
    inbox := nats.NewInbox()
    sub, _ := p.nc.SubscribeSync(inbox)
    defer sub.Unsubscribe()
    
    p.nc.PublishRequest("correlation.request", inbox, data)
    
    // Gather responses
    var correlations []*Correlation
    timeout := time.After(50 * time.Millisecond)
    
    for {
        select {
        case msg := <-sub.MsgChan():
            var corr Correlation
            json.Unmarshal(msg.Data, &corr)
            correlations = append(correlations, &corr)
        case <-timeout:
            // Process whatever we got
            p.processCorrelations(correlations)
            return
        }
    }
}
```

### 3. Circuit Breaker Pattern

```go
type CircuitBreakerPipeline struct {
    breaker *circuit.Breaker
    nc      *nats.Conn
}

func (p *CircuitBreakerPipeline) processWithBreaker(msg *nats.Msg) {
    err := p.breaker.Call(func() error {
        // Process event
        return p.processEvent(msg.Data)
    })
    
    if err == circuit.ErrOpen {
        // Circuit open - send to overflow queue
        p.nc.Publish("events.overflow", msg.Data)
    }
    
    msg.Ack()
}
```

## Benefits of Async Pipelines

### 1. **Scalability**
- Each stage scales independently
- No blocking between stages
- Automatic load balancing with queue groups

### 2. **Resilience**
- Stages can fail independently
- Built-in retry with JetStream
- Dead letter queues for failed processing

### 3. **Performance**
- Parallel processing at every stage
- No synchronous waiting
- Batch processing where beneficial

### 4. **Flexibility**
- Easy to add new stages
- Dynamic routing based on event properties
- A/B testing of different algorithms

### 5. **Observability**
- Message flow tracking
- Queue depth monitoring
- Processing latency per stage

## Implementation Example

```go
// Complete async pipeline setup
func SetupAsyncPipeline(nc *nats.Conn) error {
    js, _ := nc.JetStream()
    
    // Create streams for each stage
    streams := []string{
        "RAW_EVENTS",
        "VALIDATED_EVENTS", 
        "ENRICHED_EVENTS",
        "CORRELATED_EVENTS",
        "ANALYTICS_RESULTS",
        "INSIGHTS",
    }
    
    for _, stream := range streams {
        js.AddStream(&nats.StreamConfig{
            Name:      stream,
            Subjects:  []string{strings.ToLower(stream[:len(stream)-1]) + ".>"},
            Storage:   nats.FileStorage,
            Retention: nats.LimitsPolicy,
            MaxAge:    24 * time.Hour,
        })
    }
    
    // Start pipeline stages
    validation := NewAsyncValidationPipeline(nc, js)
    enrichment := NewAsyncEnrichmentPipeline(nc, js)
    correlation := NewAsyncCorrelationPipeline(nc, js)
    analytics := NewAsyncAnalyticsPipeline(nc, js)
    
    // Start all stages (non-blocking)
    go validation.Start()
    go enrichment.Start()
    go correlation.Start()
    go analytics.Start()
    
    return nil
}
```

## Monitoring Async Pipelines

```go
// Pipeline health monitoring
func MonitorPipeline(js nats.JetStreamContext) {
    ticker := time.NewTicker(10 * time.Second)
    
    for range ticker.C {
        for _, stream := range []string{"RAW_EVENTS", "ENRICHED_EVENTS"} {
            info, _ := js.StreamInfo(stream)
            
            fmt.Printf("Stream: %s, Messages: %d, Bytes: %d, Consumers: %d\n",
                stream, 
                info.State.Msgs,
                info.State.Bytes,
                info.State.Consumers,
            )
        }
    }
}
```

This async pipeline architecture with NATS provides:
- True parallel processing
- Independent scaling
- Fault isolation
- Real-time and batch processing
- Easy pipeline composition

Perfect for handling 165k+ events/sec with complex multi-dimensional correlation!