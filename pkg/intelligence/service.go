package intelligence

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/graph"
	"github.com/yairfalse/tapio/pkg/intelligence/patterns"
	"github.com/yairfalse/tapio/pkg/intelligence/queries"
	"go.uber.org/zap"
)

// Service provides the main intelligence engine
type Service struct {
	graphClient *graph.Client
	queries     *queries.CorrelationQuery
	detector    *patterns.Detector
	logger      *zap.Logger
}

// Config holds intelligence service configuration
type Config struct {
	Neo4jURI      string
	Neo4jUsername string
	Neo4jPassword string
	Neo4jDatabase string
}

// NewService creates a new intelligence service
func NewService(config Config, logger *zap.Logger) (*Service, error) {
	// Create Neo4j client
	graphConfig := graph.Config{
		URI:      config.Neo4jURI,
		Username: config.Neo4jUsername,
		Password: config.Neo4jPassword,
		Database: config.Neo4jDatabase,
	}

	graphClient, err := graph.NewClient(graphConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph client: %w", err)
	}

	// Create indexes
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := graphClient.CreateIndexes(ctx); err != nil {
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	return &Service{
		graphClient: graphClient,
		queries:     queries.NewCorrelationQuery(graphClient),
		detector:    patterns.NewDetector(graphClient, logger),
		logger:      logger,
	}, nil
}

// ProcessEvent processes a unified event
func (s *Service) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	// 1. Store entity if present
	if event.Entity != nil {
		if err := s.graphClient.CreateOrUpdateNode(ctx, event); err != nil {
			s.logger.Error("Failed to create/update node",
				zap.String("entity", event.Entity.Name),
				zap.Error(err))
		}
	}

	// 2. Store event
	if err := s.graphClient.CreateEvent(ctx, event); err != nil {
		s.logger.Error("Failed to create event",
			zap.String("event_id", event.ID),
			zap.Error(err))
	}

	// 3. Create relationships
	if event.Entity != nil {
		// Link event to entity
		if err := s.graphClient.CreateEventRelationship(ctx, event.ID, event.Entity.UID, graph.RelAffects); err != nil {
			s.logger.Error("Failed to create event relationship",
				zap.String("event_id", event.ID),
				zap.Error(err))
		}

		// Create ownership relationships
		if event.K8sContext != nil {
			for _, owner := range event.K8sContext.OwnerReferences {
				if err := s.graphClient.CreateRelationship(ctx,
					event.Entity.UID,
					owner.UID,
					graph.RelOwnedBy,
					map[string]interface{}{
						"controller": owner.Controller,
					}); err != nil {
					s.logger.Error("Failed to create ownership relationship", zap.Error(err))
				}
			}
		}
	}

	// 4. Detect patterns
	detections, err := s.detector.DetectPatterns(ctx, event)
	if err != nil {
		s.logger.Error("Pattern detection failed", zap.Error(err))
	}

	// 5. Process detections
	for _, detection := range detections {
		s.logger.Info("Pattern detected",
			zap.String("pattern", detection.PatternName),
			zap.Float64("confidence", detection.Confidence),
			zap.String("message", detection.Message))

		// Could create alert events or trigger actions here
	}

	return nil
}

// WhyDidThisFail performs root cause analysis
func (s *Service) WhyDidThisFail(ctx context.Context, resourceType, namespace, name string) (*queries.RootCauseAnalysis, error) {
	switch resourceType {
	case "pod":
		return s.queries.WhyDidPodFail(ctx, namespace, name, 1*time.Hour)
	default:
		return nil, fmt.Errorf("root cause analysis not implemented for %s", resourceType)
	}
}

// WhatDoesThisImpact performs impact analysis
func (s *Service) WhatDoesThisImpact(ctx context.Context, resourceType, namespace, name string) (*queries.ImpactAnalysis, error) {
	switch resourceType {
	case "service":
		return s.queries.WhatImpactsService(ctx, namespace, name)
	default:
		return nil, fmt.Errorf("impact analysis not implemented for %s", resourceType)
	}
}

// GetCascadingFailures finds recent cascade patterns
func (s *Service) GetCascadingFailures(ctx context.Context, duration time.Duration) ([]*queries.CascadePattern, error) {
	startTime := time.Now().Add(-duration)
	return s.queries.FindCascadingFailures(ctx, startTime)
}

// GetServiceMap returns service dependency map
func (s *Service) GetServiceMap(ctx context.Context, namespace string) (*queries.ServiceDependencyMap, error) {
	return s.queries.GetServiceDependencies(ctx, namespace)
}

// Health checks if the service is healthy
func (s *Service) Health(ctx context.Context) error {
	// Test Neo4j connectivity
	return s.graphClient.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		_, err := tx.Run(ctx, "RETURN 1", nil)
		return err
	})
}

// Close closes the service
func (s *Service) Close(ctx context.Context) error {
	return s.graphClient.Close(ctx)
}
