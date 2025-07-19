package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	correlationAdapter "github.com/yairfalse/tapio/pkg/server/adapters/correlation"
	"github.com/yairfalse/tapio/pkg/server/domain"
	"go.uber.org/zap"
)

// ServerWithAdapter provides REST API using the correlation adapter
type ServerWithAdapter struct {
	router             *gin.Engine
	correlationAdapter *correlationAdapter.CorrelationAdapter
	logger             *zap.Logger
	config             *Config
}

// NewServerWithAdapter creates a new API server with correlation adapter
func NewServerWithAdapter(adapter *correlationAdapter.CorrelationAdapter, config *Config) *ServerWithAdapter {
	if config == nil {
		config = &Config{
			Port:            "8888",
			EnableCORS:      true,
			RateLimitPerMin: 1000,
			CacheTimeout:    30 * time.Second,
		}
	}

	s := &ServerWithAdapter{
		router:             gin.New(),
		correlationAdapter: adapter,
		logger:             zap.NewNop(), // Should inject real logger
		config:             config,
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

// setupMiddleware configures middleware
func (s *ServerWithAdapter) setupMiddleware() {
	// Recovery middleware
	s.router.Use(gin.Recovery())

	// Logging middleware
	s.router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return "" // Use structured logging instead
	}))

	// CORS if enabled
	if s.config.EnableCORS {
		s.router.Use(corsMiddleware())
	}

	// Rate limiting
	if s.config.RateLimitPerMin > 0 {
		s.router.Use(rateLimitMiddleware(s.config.RateLimitPerMin))
	}

	// Metrics collection
	if s.config.MetricsEnabled {
		s.router.Use(metricsMiddleware())
	}
}

// setupRoutes configures API routes
func (s *ServerWithAdapter) setupRoutes() {
	// Health check
	s.router.GET("/health", s.healthCheck)
	s.router.GET("/ready", s.readinessCheck)

	// API v1
	v1 := s.router.Group("/api/v1")
	{
		// Resource insights
		v1.GET("/insights/:namespace/:resource", s.getResourceInsights)
		v1.GET("/insights", s.listAllInsights)

		// Predictions
		v1.GET("/predictions/:namespace/:resource", s.getResourcePredictions)
		v1.GET("/predictions", s.listActivePredictions)

		// Actionable items (fixes)
		v1.GET("/fixes/:namespace/:resource", s.getResourceFixes)
		v1.POST("/fixes/:namespace/:resource/:fixId/apply", s.applyFix)

		// Correlation queries
		v1.POST("/correlate", s.correlateEvents)
		v1.GET("/patterns", s.listPatterns)
		v1.GET("/patterns/:patternId/matches", s.getPatternMatches)

		// Statistics
		v1.GET("/stats", s.getStats)

		// Process event endpoint
		v1.POST("/events", s.processEvent)
	}

	// Admin endpoints
	admin := s.router.Group("/admin")
	if s.config.AuthEnabled {
		admin.Use(authMiddleware())
	}
	{
		admin.GET("/status", s.getAdapterStatus)
		admin.POST("/correlation/enable", s.enableCorrelation)
		admin.POST("/correlation/disable", s.disableCorrelation)
	}
}

// Handler implementations using correlation adapter

func (s *ServerWithAdapter) getResourceInsights(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	ctx := c.Request.Context()

	insights, err := s.correlationAdapter.GetInsights(ctx, resource, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to get insights: %v", err),
		})
		return
	}

	// Convert adapter insights to API response
	var responseInsights []gin.H
	for _, insight := range insights {
		responseInsights = append(responseInsights, gin.H{
			"id":               insight.ID,
			"title":            insight.Title,
			"description":      insight.Description,
			"severity":         insight.Severity,
			"category":         insight.Category,
			"resource":         insight.Resource,
			"namespace":        insight.Namespace,
			"timestamp":        insight.Timestamp,
			"prediction":       insight.Prediction,
			"actionable_items": insight.ActionableItems,
			"metadata":         insight.Metadata,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"resource":  resource,
		"namespace": namespace,
		"insights":  responseInsights,
		"count":     len(responseInsights),
		"timestamp": time.Now(),
	})
}

func (s *ServerWithAdapter) getResourcePredictions(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	ctx := c.Request.Context()

	predictions, err := s.correlationAdapter.GetPredictions(ctx, resource, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to get predictions: %v", err),
		})
		return
	}

	// Convert adapter predictions to API response
	var responsePredictions []gin.H
	for _, prediction := range predictions {
		responsePredictions = append(responsePredictions, gin.H{
			"type":          prediction.Type,
			"time_to_event": prediction.TimeToEvent,
			"probability":   prediction.Probability,
			"confidence":    prediction.Confidence,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"resource":    resource,
		"namespace":   namespace,
		"predictions": responsePredictions,
		"count":       len(responsePredictions),
		"timestamp":   time.Now(),
	})
}

func (s *ServerWithAdapter) getResourceFixes(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	ctx := c.Request.Context()

	actionableItems, err := s.correlationAdapter.GetActionableItems(ctx, resource, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to get actionable items: %v", err),
		})
		return
	}

	// Convert actionable items to fixes
	var fixes []gin.H
	for i, item := range actionableItems {
		fixes = append(fixes, gin.H{
			"id":           fmt.Sprintf("fix-%d", i),
			"description":  item.Description,
			"command":      item.Command,
			"impact":       item.Impact,
			"risk":         item.Risk,
			"auto_fixable": item.Risk == "low",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"resource":  resource,
		"namespace": namespace,
		"fixes":     fixes,
		"count":     len(fixes),
		"timestamp": time.Now(),
	})
}

func (s *ServerWithAdapter) correlateEvents(c *gin.Context) {
	ctx := c.Request.Context()

	var request struct {
		Events []domain.Event `json:"events"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Convert to pointer slice for adapter
	eventPtrs := make([]*domain.Event, len(request.Events))
	for i := range request.Events {
		eventPtrs[i] = &request.Events[i]
	}

	result, err := s.correlationAdapter.CorrelateEvents(ctx, eventPtrs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to correlate events: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"correlation_id": result.ID,
		"timestamp":      result.Timestamp,
		"event_count":    result.EventCount,
		"correlations":   result.Correlations,
	})
}

func (s *ServerWithAdapter) listPatterns(c *gin.Context) {
	ctx := c.Request.Context()

	patterns, err := s.correlationAdapter.GetPatterns(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to get patterns: %v", err),
		})
		return
	}

	// Convert patterns to response
	var responsePatterns []gin.H
	for _, pattern := range patterns {
		responsePatterns = append(responsePatterns, gin.H{
			"id":          pattern.ID,
			"name":        pattern.Name,
			"description": pattern.Description,
			"type":        pattern.Type,
			"enabled":     pattern.Enabled,
			"metadata":    pattern.Metadata,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"patterns": responsePatterns,
		"count":    len(responsePatterns),
	})
}

func (s *ServerWithAdapter) getPatternMatches(c *gin.Context) {
	patternID := c.Param("patternId")
	ctx := c.Request.Context()

	matches, err := s.correlationAdapter.GetPatternMatches(ctx, patternID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to get pattern matches: %v", err),
		})
		return
	}

	// Convert matches to response
	var responseMatches []gin.H
	for _, match := range matches {
		responseMatches = append(responseMatches, gin.H{
			"id":         match.ID,
			"pattern_id": match.PatternID,
			"event_ids":  match.EventIDs,
			"confidence": match.Confidence,
			"timestamp":  match.Timestamp,
			"metadata":   match.Metadata,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"pattern_id": patternID,
		"matches":    responseMatches,
		"count":      len(responseMatches),
	})
}

func (s *ServerWithAdapter) getStats(c *gin.Context) {
	ctx := c.Request.Context()

	stats, err := s.correlationAdapter.GetStats(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to get stats: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":               stats.Enabled,
		"events_processed":      stats.EventsProcessed,
		"insights_generated":    stats.InsightsGenerated,
		"predictions_generated": stats.PredictionsGenerated,
		"correlations_found":    stats.CorrelationsFound,
		"last_processed_at":     stats.LastProcessedAt,
		"timestamp":             time.Now(),
	})
}

func (s *ServerWithAdapter) processEvent(c *gin.Context) {
	ctx := c.Request.Context()

	var event domain.Event
	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set defaults if not provided
	if event.ID == "" {
		event.ID = fmt.Sprintf("event-%d", time.Now().UnixNano())
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	err := s.correlationAdapter.ProcessEvent(ctx, &event)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to process event: %v", err),
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":   "accepted",
		"event_id": event.ID,
		"message":  "Event processed successfully",
	})
}

func (s *ServerWithAdapter) listAllInsights(c *gin.Context) {
	// Query parameters for pagination
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	severity := c.Query("severity")
	category := c.Query("category")

	// For now, return empty as we need to implement a method to get all insights
	// This would require extending the adapter to support listing all insights
	c.JSON(http.StatusOK, gin.H{
		"insights": []gin.H{},
		"count":    0,
		"page":     page,
		"limit":    limit,
		"filters": gin.H{
			"severity": severity,
			"category": category,
		},
		"message": "List all insights requires adapter extension",
	})
}

func (s *ServerWithAdapter) listActivePredictions(c *gin.Context) {
	// This would require extending the adapter to support listing all active predictions
	c.JSON(http.StatusOK, gin.H{
		"predictions": []gin.H{},
		"count":       0,
		"message":     "List active predictions requires adapter extension",
	})
}

func (s *ServerWithAdapter) applyFix(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	fixID := c.Param("fixId")

	var request struct {
		DryRun bool `json:"dry_run"`
		Force  bool `json:"force"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		request.DryRun = true // Default to dry run for safety
	}

	// This would require implementation in the correlation adapter
	result := gin.H{
		"fix_id":    fixID,
		"resource":  resource,
		"namespace": namespace,
		"dry_run":   request.DryRun,
		"status":    "not_implemented",
		"message":   "Fix application requires adapter extension",
		"timestamp": time.Now(),
	}

	c.JSON(http.StatusOK, result)
}

// Admin endpoints

func (s *ServerWithAdapter) getAdapterStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"enabled":   s.correlationAdapter.IsEnabled(),
		"timestamp": time.Now(),
	})
}

func (s *ServerWithAdapter) enableCorrelation(c *gin.Context) {
	s.correlationAdapter.Enable()
	c.JSON(http.StatusOK, gin.H{
		"status":    "enabled",
		"timestamp": time.Now(),
	})
}

func (s *ServerWithAdapter) disableCorrelation(c *gin.Context) {
	s.correlationAdapter.Disable()
	c.JSON(http.StatusOK, gin.H{
		"status":    "disabled",
		"timestamp": time.Now(),
	})
}

// Health checks

func (s *ServerWithAdapter) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now(),
	})
}

func (s *ServerWithAdapter) readinessCheck(c *gin.Context) {
	// Check if correlation adapter is ready
	if s.correlationAdapter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not ready",
			"reason": "correlation adapter not initialized",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ready",
		"adapter": s.correlationAdapter.IsEnabled(),
		"time":    time.Now(),
	})
}

// Start starts the API server
func (s *ServerWithAdapter) Start() error {
	return s.router.Run(":" + s.config.Port)
}

// Stop gracefully stops the server
func (s *ServerWithAdapter) Stop(ctx context.Context) error {
	// Implement graceful shutdown
	return nil
}
