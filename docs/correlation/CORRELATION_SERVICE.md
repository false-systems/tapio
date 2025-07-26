# CorrelationService API Documentation

## Overview

The CorrelationService provides advanced correlation analysis, pattern detection, and AI-powered insights for events in the Tapio observability platform. It offers both gRPC and REST interfaces for real-time and batch correlation analysis.

## Architecture

```
┌─────────────────────┐     ┌──────────────────────┐     ┌─────────────────────┐
│   gRPC/REST Client  │────▶│  CorrelationService  │────▶│ Correlation Engine  │
└─────────────────────┘     │  (Port 50051/8080)   │     │ (Pattern Detection) │
                            └──────────────────────┘     └─────────────────────┘
                                      │                            │
                                      ▼                            ▼
                            ┌──────────────────────┐     ┌─────────────────────┐
                            │   Event Pipeline     │     │   ML/AI Analysis    │
                            │ (Real-time Stream)   │     │ (Root Cause/Impact) │
                            └──────────────────────┘     └─────────────────────┘
```

## API Endpoints

### gRPC Methods

#### 1. GetCorrelations
Retrieves correlations based on IDs or query filters.

```protobuf
rpc GetCorrelations(GetCorrelationsRequest) returns (GetCorrelationsResponse)
```

**Request:**
- `correlation_ids`: List of specific correlation IDs to retrieve
- `query`: CorrelationQuery with filters, confidence thresholds, and sorting

**Response:**
- `correlations`: List of Correlation objects
- `total_count`: Total number of correlations found
- `next_page_token`: Token for pagination
- `metadata`: Additional response metadata

#### 2. GetSemanticGroups
Retrieves semantic groups of related events.

```protobuf
rpc GetSemanticGroups(GetSemanticGroupsRequest) returns (GetSemanticGroupsResponse)
```

**Request:**
- `group_ids`: Specific group IDs to retrieve
- `filter`: Filter criteria for groups
- `include_events`: Include full event details
- `include_analysis`: Include analysis results

**Response:**
- `groups`: List of SemanticGroup objects
- `total_count`: Total number of groups
- `next_page_token`: Pagination token

#### 3. AnalyzeEvents
Performs on-demand correlation analysis on specified events.

```protobuf
rpc AnalyzeEvents(AnalyzeEventsRequest) returns (AnalyzeEventsResponse)
```

**Request:**
- `event_ids`: Events to analyze
- `enable_root_cause`: Enable root cause analysis
- `enable_predictions`: Enable predictive analysis
- `enable_impact_assessment`: Enable business impact assessment

**Response:**
- `correlations`: Discovered correlations
- `semantic_groups`: Semantic groupings
- `root_cause`: Root cause analysis results
- `predictions`: Predicted outcomes
- `overall_impact`: Business impact assessment

#### 4. SubscribeToCorrelations
Real-time streaming of correlation updates.

```protobuf
rpc SubscribeToCorrelations(SubscribeToCorrelationsRequest) returns (stream CorrelationUpdate)
```

**Request:**
- `filter`: Filter for relevant correlations
- `correlation_types`: Types of correlations to monitor
- `min_confidence`: Minimum confidence threshold
- `include_predictions`: Include predictive updates

**Stream Response:**
- Continuous stream of CorrelationUpdate messages
- Update types: NEW_CORRELATION, CORRELATION_STRENGTHENED, NEW_EVENTS_ADDED, etc.

#### 5. GetRecommendedActions
Gets AI-powered remediation recommendations.

```protobuf
rpc GetRecommendedActions(GetCorrelationsRequest) returns (GetRecommendedActionsResponse)
```

**Request:**
- Same as GetCorrelations request

**Response:**
- `actions`: List of recommended actions
- `correlation_id`: Related correlation ID
- `metadata`: Action metadata

### REST Endpoints

All gRPC methods are also available via REST through grpc-gateway:

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/correlations` | Get correlations |
| GET | `/api/v1/correlations/semantic-groups` | Get semantic groups |
| POST | `/api/v1/correlations/analyze` | Analyze events |
| GET | `/api/v1/correlations/recommended-actions` | Get recommendations |

## Data Models

### Correlation
```go
type Correlation struct {
    Id               string
    Type             CorrelationType
    Title            string
    Description      string
    CorrelationScore float64  // -1.0 to 1.0
    Confidence       float64  // 0.0 to 1.0
    EventIds         []string
    EventCount       int32
    DiscoveredAt     timestamp
    Statistics       map[string]float64
    Actions          []RecommendedAction
}
```

### SemanticGroup
```go
type SemanticGroup struct {
    Id              string
    Name            string
    Description     string
    SemanticType    string  // e.g., "deployment", "failure_cascade"
    Intent          string
    ConfidenceScore float64
    Events          []Event
    StartTime       timestamp
    EndTime         timestamp
    Impact          ImpactAssessment
    RootCause       RootCauseAnalysis
}
```

### RecommendedAction
```go
type RecommendedAction struct {
    Id                string
    Title             string
    Description       string
    Type              ActionType  // INVESTIGATE, MITIGATE, PREVENT, ESCALATE
    Priority          Priority    // LOW, MEDIUM, HIGH, CRITICAL
    Commands          []string
    ExpectedResult    string
    EstimatedDuration duration
    RiskLevel         string
}
```

## Usage Examples

### gRPC Example (Go)

```go
// Connect to CorrelationService
conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
client := pb.NewCorrelationServiceClient(conn)

// Analyze events
req := &pb.AnalyzeEventsRequest{
    EventIds: []string{"event-1", "event-2", "event-3"},
    EnableRootCause: true,
    EnablePredictions: true,
}

resp, err := client.AnalyzeEvents(context.Background(), req)
if err != nil {
    log.Fatal(err)
}

// Process correlations
for _, corr := range resp.Correlations {
    fmt.Printf("Found correlation: %s (confidence: %.2f)\n", 
        corr.Title, corr.Confidence)
}

// Subscribe to real-time updates
stream, err := client.SubscribeToCorrelations(context.Background(), 
    &pb.SubscribeToCorrelationsRequest{
        MinConfidence: 0.8,
    })

for {
    update, err := stream.Recv()
    if err != nil {
        break
    }
    fmt.Printf("New correlation: %s\n", update.Correlation.Title)
}
```

### REST Example (cURL)

```bash
# Get all correlations
curl http://localhost:8080/api/v1/correlations

# Analyze specific events
curl -X POST http://localhost:8080/api/v1/correlations/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "event_ids": ["event-1", "event-2"],
    "enable_root_cause": true,
    "enable_predictions": true
  }'

# Get semantic groups
curl http://localhost:8080/api/v1/correlations/semantic-groups

# Get recommended actions
curl http://localhost:8080/api/v1/correlations/recommended-actions
```

## Configuration

The CorrelationService can be configured through the following parameters:

```yaml
correlation_service:
  max_subscriptions: 1000
  max_events_per_analysis: 10000
  default_confidence: 0.7
  max_concurrent_analysis: 10
  subscription_timeout: 5m
```

## Performance Characteristics

- **Throughput**: Handles up to 10,000 events per analysis request
- **Latency**: Sub-second response for correlation queries
- **Streaming**: Real-time updates with <100ms latency
- **Concurrency**: Supports up to 1,000 concurrent subscriptions
- **Scalability**: Horizontally scalable with load balancing

## Integration Points

1. **Event Pipeline**: Integrates with the real-time event processing pipeline
2. **Storage Layer**: Queries historical events from event storage
3. **ML/AI Engine**: Leverages machine learning for pattern detection
4. **Metrics Collection**: Exports metrics to Prometheus/OpenTelemetry
5. **Tracing**: Full distributed tracing support

## Monitoring

Key metrics exposed:

- `correlation_service_correlations_queried_total`: Total correlations queried
- `correlation_service_subscriptions_active`: Active streaming subscriptions
- `correlation_service_events_analyzed_total`: Total events analyzed
- `correlation_service_recommendations_served_total`: Recommendations generated
- `correlation_service_analysis_duration_seconds`: Analysis latency histogram

## Security Considerations

1. **Authentication**: Supports mTLS for gRPC connections
2. **Authorization**: Role-based access control for sensitive operations
3. **Rate Limiting**: Configurable limits per client
4. **Data Privacy**: Event data sanitization options
5. **Audit Logging**: All operations are audited

## Future Enhancements

1. **Custom Pattern Definition**: Allow users to define custom correlation patterns
2. **ML Model Management**: Version and manage correlation models
3. **Webhook Integration**: Push correlation alerts to external systems
4. **Correlation Playground**: Interactive UI for exploring correlations
5. **Export/Import**: Correlation rules and patterns portability