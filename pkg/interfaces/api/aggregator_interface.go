package api

import (
	"context"

	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
)

// AggregatorInterface defines the methods required by the API server
type AggregatorInterface interface {
	QueryCorrelations(ctx context.Context, query aggregator.CorrelationQuery) (*aggregator.AggregatedResult, error)
	ListCorrelations(ctx context.Context, limit, offset int) (*aggregator.CorrelationList, error)
	GetCorrelation(ctx context.Context, id string) (*aggregator.AggregatedResult, error)
	SubmitFeedback(ctx context.Context, feedback aggregator.CorrelationFeedback) error
	GetSummary(ctx context.Context) (*aggregator.CorrelationSummary, error)
	ProcessEvent(ctx context.Context, event interface{}) (*aggregator.AggregatedResult, error)
}
