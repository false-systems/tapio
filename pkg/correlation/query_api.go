package correlation

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// QueryAPI provides gRPC API for querying correlation insights
type QueryAPI struct {
	UnimplementedCorrelationQueryServer
	engine *PerfectEngine
	store  InsightStore
}

// NewQueryAPI creates a new correlation query API
func NewQueryAPI(engine *PerfectEngine) *QueryAPI {
	return &QueryAPI{
		engine: engine,
		store:  NewInMemoryInsightStore(), // TODO: Replace with persistent store
	}
}

// GetPredictions returns predictions for a specific resource
func (q *QueryAPI) GetPredictions(ctx context.Context, req *GetPredictionsRequest) (*GetPredictionsResponse, error) {
	if req.ResourceName == "" {
		return nil, status.Error(codes.InvalidArgument, "resource name is required")
	}

	// Get insights from store
	insights := q.store.GetInsights(req.ResourceName, req.Namespace)

	// Filter to only predictions
	var predictions []*Prediction
	for _, insight := range insights {
		if insight.Prediction != nil {
			predictions = append(predictions, insight.Prediction)
		}
	}

	return &GetPredictionsResponse{
		Predictions: predictions,
		Timestamp:   time.Now(),
	}, nil
}

// GetInsights returns all insights for a resource
func (q *QueryAPI) GetInsights(ctx context.Context, req *GetInsightsRequest) (*GetInsightsResponse, error) {
	insights := q.store.GetInsights(req.ResourceName, req.Namespace)

	// Convert to response format
	var responseInsights []*InsightResponse
	for _, insight := range insights {
		responseInsights = append(responseInsights, &InsightResponse{
			Id:              insight.ID,
			Title:           insight.Title,
			Description:     insight.Description,
			Severity:        insight.Severity,
			Category:        insight.Category,
			ResourceName:    insight.ResourceName,
			Namespace:       insight.Namespace,
			Timestamp:       insight.Timestamp,
			Prediction:      insight.Prediction,
			ActionableItems: insight.ActionableItems,
		})
	}

	return &GetInsightsResponse{
		Insights:  responseInsights,
		Timestamp: time.Now(),
	}, nil
}

// GetActionableItems returns fix suggestions
func (q *QueryAPI) GetActionableItems(ctx context.Context, req *GetActionableItemsRequest) (*GetActionableItemsResponse, error) {
	insights := q.store.GetInsights(req.ResourceName, req.Namespace)

	var items []*ActionableItem
	for _, insight := range insights {
		if insight.Severity == "critical" || insight.Severity == "high" {
			items = append(items, insight.ActionableItems...)
		}
	}

	return &GetActionableItemsResponse{
		Items:     items,
		Timestamp: time.Now(),
	}, nil
}

// StoreInsight stores a new insight from the correlation engine
func (q *QueryAPI) StoreInsight(insight *Insight) {
	q.store.Store(insight)
}

// InsightStore and InMemoryInsightStore are defined in interfaces.go
// These methods extend the basic implementation with byResource indexing

// IndexedInsightStore extends InMemoryInsightStore with resource indexing
type IndexedInsightStore struct {
	InMemoryInsightStore
	byResource map[string][]*domain.Insight // key: namespace/name
}

func NewIndexedInsightStore() *IndexedInsightStore {
	return &IndexedInsightStore{
		InMemoryInsightStore: InMemoryInsightStore{
			insights: make(map[string]*domain.Insight),
		},
		byResource: make(map[string][]*domain.Insight),
	}
}

func (s *IndexedInsightStore) Store(insight *domain.Insight) error {
	s.insights[insight.ID] = insight

	key := fmt.Sprintf("%s/%s", insight.Namespace, insight.ResourceName)
	s.byResource[key] = append(s.byResource[key], insight)

	// Call parent Store method
	return s.InMemoryInsightStore.Store(insight)
}

func (s *IndexedInsightStore) GetInsights(resourceName, namespace string) []*Insight {
	key := fmt.Sprintf("%s/%s", namespace, resourceName)
	return s.byResource[key]
}

func (s *IndexedInsightStore) DeleteOlderThan(cutoff time.Time) error {
	for id, insight := range s.insights {
		if insight.Timestamp.Before(cutoff) {
			delete(s.insights, id)
			// Also remove from byResource
			key := fmt.Sprintf("%s/%s", insight.Namespace, insight.ResourceName)
			var updated []*Insight
			for _, i := range s.byResource[key] {
				if i.ID != id {
					updated = append(updated, i)
				}
			}
			s.byResource[key] = updated
		}
	}

	// Also call parent DeleteOlderThan
	return s.InMemoryInsightStore.DeleteOlderThan(cutoff)
}

// Proto definitions (these would normally be in a .proto file)
type GetPredictionsRequest struct {
	ResourceName string
	Namespace    string
	TimeRange    *TimeRange
}

type GetPredictionsResponse struct {
	Predictions []*Prediction
	Timestamp   time.Time
}

type GetInsightsRequest struct {
	ResourceName string
	Namespace    string
	Severity     string // filter by severity
	Category     string // filter by category
}

type GetInsightsResponse struct {
	Insights  []*InsightResponse
	Timestamp time.Time
}

type GetActionableItemsRequest struct {
	ResourceName string
	Namespace    string
	AutoFixOnly  bool // only return items that can be auto-fixed
}

type GetActionableItemsResponse struct {
	Items     []*domain.ActionItem
	Timestamp time.Time
}

type InsightResponse struct {
	Id              string
	Title           string
	Description     string
	Severity        string
	Category        string
	ResourceName    string
	Namespace       string
	Timestamp       time.Time
	Prediction      *Prediction
	ActionableItems []*domain.ActionItem
}

// TimeRange is defined in timeline.go

// Stub for unimplemented server
type UnimplementedCorrelationQueryServer struct{}

func RegisterQueryAPI(server *grpc.Server, api *QueryAPI) {
	// Would register with generated gRPC code
	// pb.RegisterCorrelationQueryServer(server, api)
}
