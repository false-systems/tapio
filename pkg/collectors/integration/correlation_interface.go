package collector

import "context"

type CorrelationEngineInterface interface {
    RegisterCollector(c Collector) error
    Start(ctx context.Context) error
    Stop()
    Insights() <-chan Insight
    GetStats() map[string]interface{}
}
