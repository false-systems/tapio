package correlation

import (
    "context"
    "time"
    "github.com/falseyair/tapio/pkg/domain"
)

// Concrete engine implementation
type PerfectEngine struct {
    events   chan *domain.Event
    insights chan *domain.Insight
    stats    *EngineStats
    running  bool
}

func (e *PerfectEngine) Start(ctx context.Context) error {
    e.running = true
    return nil
}

func (e *PerfectEngine) Stop() error {
    e.running = false
    return nil
}

func (e *PerfectEngine) ProcessEvent(ctx context.Context, event *domain.Event) error {
    e.stats.EventsProcessed++
    e.stats.LastProcessed = time.Now()
    return nil
}

func (e *PerfectEngine) ProcessOpinionatedEvent(ctx context.Context, event *domain.Event) error {
    return e.ProcessEvent(ctx, event)
}

func (e *PerfectEngine) GetInsights(ctx context.Context) ([]domain.Insight, error) {
    return nil, nil
}

func (e *PerfectEngine) GetStats() *EngineStats {
    return e.stats
}

// Updated EngineStats
type EngineStats struct {
    EventsProcessed     int64     `json:"events_processed"`
    InsightsGenerated   int64     `json:"insights_generated"`
    CorrelationsFound   int64     `json:"correlations_found"`
    ErrorCount          int64     `json:"error_count"`
    LastProcessed       time.Time `json:"last_processed"`
    LastProcessedTime   time.Time `json:"last_processed_time"`
    ProcessingTime      time.Duration `json:"processing_time"`
}

type InsightStore interface {
    Store(ctx context.Context, insight domain.Insight) error
    Get(ctx context.Context, id string) (*domain.Insight, error)
    List(ctx context.Context) ([]domain.Insight, error)
}

type IntentClassifier interface {
    ClassifyIntent(event *domain.Event) string
    GetConfidence(event *domain.Event) float64
}

type TrendAnalyzer interface {
    AnalyzeTrend(events []domain.Event) *TrendAnalysis
    PredictNext(events []domain.Event) *domain.Prediction
}

type TrendAnalysis struct {
    Direction   string    `json:"direction"`
    Confidence  float64   `json:"confidence"`
    Slope       float64   `json:"slope"`
    Prediction  string    `json:"prediction"`
}

type Prediction struct {
    ID          string                 `json:"id"`
    Type        string                 `json:"type"`
    Scenario    string                 `json:"scenario"`
    Probability float64                `json:"probability"`
    Confidence  float64                `json:"confidence"`
    TimeToEvent time.Duration          `json:"time_to_event"`
    Metadata    map[string]interface{} `json:"metadata"`
}

type TrendModel struct {
    Name        string    `json:"name"`
    Type        string    `json:"type"`
    Accuracy    float64   `json:"accuracy"`
    LastTrained time.Time `json:"last_trained"`
}

type Engine = PerfectEngine

// Helper functions
func generateInsightID() string {
    return "insight-" + time.Now().Format("20060102-150405")
}
