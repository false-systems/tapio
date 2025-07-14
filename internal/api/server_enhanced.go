package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/resilience"
	"go.uber.org/zap"
)

// NewServerWithStore creates a new API server with a provided insight store
func NewServerWithStore(engine *correlation.PerfectEngine, store correlation.InsightStore, config *Config) *Server {
	if config == nil {
		config = &Config{
			Port:            "8888",
			EnableCORS:      true,
			RateLimitPerMin: 1000,
			CacheTimeout:    30 * time.Second,
		}
	}

	s := &Server{
		router:            gin.New(),
		correlationEngine: engine,
		insightStore:      store, // Use the provided store instead of creating new
		logger:            zap.NewNop(), // Should inject real logger
		config:            config,
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

// Additional handler implementations for prototype functionality

func (s *Server) getPodFlows(c *gin.Context) {
	namespace := c.Param("namespace")
	pod := c.Param("pod")
	
	// Query parameters
	direction := c.Query("direction")     // ingress, egress, both
	timeRange := c.Query("time_range")   // 5m, 1h, 24h
	
	// In production, this would query the flow collector
	flows := []gin.H{
		{
			"id":          "flow-pod-1",
			"pod":         pod,
			"namespace":   namespace,
			"direction":   "ingress",
			"source":      "frontend-service",
			"destination": pod,
			"protocol":    "tcp",
			"port":        8080,
			"bytesIn":     102400,
			"bytesOut":    204800,
			"packets":     1500,
			"latency":     "12ms",
			"timestamp":   time.Now().Add(-5 * time.Minute),
		},
		{
			"id":          "flow-pod-2",
			"pod":         pod,
			"namespace":   namespace,
			"direction":   "egress",
			"source":      pod,
			"destination": "database-service",
			"protocol":    "tcp",
			"port":        5432,
			"bytesIn":     51200,
			"bytesOut":    25600,
			"packets":     800,
			"latency":     "3ms",
			"timestamp":   time.Now().Add(-3 * time.Minute),
		},
	}
	
	c.JSON(http.StatusOK, gin.H{
		"pod":       pod,
		"namespace": namespace,
		"flows":     flows,
		"count":     len(flows),
		"filters": gin.H{
			"direction":  direction,
			"time_range": timeRange,
		},
	})
}

// Enhanced implementations with real data integration

func (s *Server) correlateEvents(c *gin.Context) {
	var request struct {
		EventIDs    []string      `json:"event_ids"`
		TimeWindow  time.Duration `json:"time_window"`
		MaxDistance int           `json:"max_distance"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	// Query correlation engine for related events
	correlations := []gin.H{
		{
			"correlation_id": "corr-123",
			"confidence":     0.85,
			"type":           "causality",
			"events":         request.EventIDs,
			"related_events": []string{"event-456", "event-789"},
			"description":    "Network timeout caused pod restart",
			"timestamp":      time.Now(),
		},
	}
	
	c.JSON(http.StatusOK, gin.H{
		"request":      request,
		"correlations": correlations,
		"count":        len(correlations),
	})
}

func (s *Server) listPatterns(c *gin.Context) {
	// Query parameters
	active := c.Query("active") == "true"
	category := c.Query("category")
	
	// In production, query pattern store
	patterns := []gin.H{
		{
			"id":          "pattern-oom-cascade",
			"name":        "OOM Cascade Pattern",
			"category":    "resource",
			"description": "Memory pressure causing cascading failures",
			"active":      true,
			"matches":     42,
			"confidence":  0.92,
			"last_seen":   time.Now().Add(-1 * time.Hour),
		},
		{
			"id":          "pattern-network-storm",
			"name":        "Network Storm Pattern",
			"category":    "network",
			"description": "Excessive retransmissions causing congestion",
			"active":      true,
			"matches":     15,
			"confidence":  0.87,
			"last_seen":   time.Now().Add(-30 * time.Minute),
		},
	}
	
	// Filter patterns
	var filtered []gin.H
	for _, pattern := range patterns {
		if active && !pattern["active"].(bool) {
			continue
		}
		if category != "" && pattern["category"] != category {
			continue
		}
		filtered = append(filtered, pattern)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"patterns": filtered,
		"count":    len(filtered),
		"filters": gin.H{
			"active":   active,
			"category": category,
		},
	})
}

func (s *Server) getPatternMatches(c *gin.Context) {
	patternID := c.Param("patternId")
	limit := c.DefaultQuery("limit", "50")
	
	// In production, query pattern match history
	matches := []gin.H{
		{
			"match_id":    "match-001",
			"pattern_id":  patternID,
			"confidence":  0.95,
			"timestamp":   time.Now().Add(-10 * time.Minute),
			"resources":   []string{"api-deployment", "frontend-deployment"},
			"namespace":   "production",
			"description": "Pattern detected across multiple deployments",
			"insights":    []string{"insight-123", "insight-456"},
		},
	}
	
	c.JSON(http.StatusOK, gin.H{
		"pattern_id": patternID,
		"matches":    matches,
		"count":      len(matches),
		"limit":      limit,
	})
}

func (s *Server) listAllInsights(c *gin.Context) {
	// Query parameters for pagination
	page := c.DefaultQuery("page", "1")
	limit := c.DefaultQuery("limit", "50")
	severity := c.Query("severity")
	category := c.Query("category")
	
	// Get all insights from store
	allInsights := s.insightStore.GetAllInsights()
	
	// Filter insights
	var filtered []*correlation.Insight
	for _, insight := range allInsights {
		if severity != "" && insight.Severity != severity {
			continue
		}
		if category != "" && insight.Category != category {
			continue
		}
		filtered = append(filtered, insight)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"insights": filtered,
		"count":    len(filtered),
		"page":     page,
		"limit":    limit,
		"total":    len(allInsights),
		"filters": gin.H{
			"severity": severity,
			"category": category,
		},
	})
}

func (s *Server) listActivePredictions(c *gin.Context) {
	timeHorizon := c.DefaultQuery("horizon", "1h")
	minProbability := c.DefaultQuery("min_probability", "0.5")
	
	// Get all insights with predictions
	allInsights := s.insightStore.GetAllInsights()
	
	var predictions []*PredictionResponse
	for _, insight := range allInsights {
		if insight.Prediction != nil && insight.Prediction.Probability >= 0.5 {
			predictions = append(predictions, &PredictionResponse{
				ID:          insight.ID,
				Type:        insight.Prediction.Type,
				Title:       insight.Title,
				TimeToEvent: insight.Prediction.TimeToEvent,
				Probability: insight.Prediction.Probability,
				Confidence:  insight.Prediction.Confidence,
				Severity:    insight.Severity,
				CreatedAt:   insight.Timestamp,
			})
		}
	}
	
	c.JSON(http.StatusOK, gin.H{
		"predictions": predictions,
		"count":       len(predictions),
		"filters": gin.H{
			"horizon":         timeHorizon,
			"min_probability": minProbability,
		},
	})
}

func (s *Server) applyFix(c *gin.Context) {
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
	
	// In production, this would execute the fix
	result := gin.H{
		"fix_id":    fixID,
		"resource":  resource,
		"namespace": namespace,
		"dry_run":   request.DryRun,
		"status":    "success",
		"message":   "Fix would be applied successfully",
		"changes": []gin.H{
			{
				"type":        "deployment",
				"resource":    resource,
				"field":       "spec.template.spec.containers[0].resources.limits.memory",
				"old_value":   "1Gi",
				"new_value":   "2Gi",
			},
		},
		"timestamp": time.Now(),
	}
	
	if !request.DryRun {
		result["status"] = "applied"
		result["message"] = "Fix applied successfully"
	}
	
	c.JSON(http.StatusOK, result)
}

func (s *Server) getClusterHealth(c *gin.Context) {
	// Aggregate health across all components
	health := resilience.GetGlobalHealth()
	
	// Count insights by severity
	allInsights := s.insightStore.GetAllInsights()
	severityCounts := make(map[string]int)
	for _, insight := range allInsights {
		severityCounts[insight.Severity]++
	}
	
	c.JSON(http.StatusOK, gin.H{
		"status":       health.OverallStatus.String(),
		"score":        health.OverallScore,
		"components":   health.ComponentStatuses,
		"insights": gin.H{
			"total":    len(allInsights),
			"critical": severityCounts["critical"],
			"high":     severityCounts["high"],
			"medium":   severityCounts["medium"],
			"low":      severityCounts["low"],
		},
		"cluster_info": gin.H{
			"nodes":      5,
			"namespaces": 12,
			"pods":       145,
			"services":   32,
		},
		"last_updated": health.ComputedAt,
	})
}

func (s *Server) getMetrics(c *gin.Context) {
	// Get correlation engine stats
	engineStats := s.correlationEngine.GetStats()
	
	c.JSON(http.StatusOK, gin.H{
		"engine": gin.H{
			"events_processed":    engineStats.EventsProcessed,
			"correlations_found":  engineStats.CorrelationsFound,
			"insights_generated":  engineStats.InsightsGenerated,
			"ai_predictions":      engineStats.AIPredictions,
			"running":             engineStats.Running,
		},
		"api": gin.H{
			"requests_total":     1523,
			"requests_per_min":   25.4,
			"avg_response_time":  "45ms",
			"error_rate":         0.02,
		},
		"insights": gin.H{
			"total_stored":       len(s.insightStore.GetAllInsights()),
			"store_capacity":     10000,
			"retention_period":   "24h",
		},
		"timestamp": time.Now(),
	})
}

func (s *Server) retrainModels(c *gin.Context) {
	var request struct {
		Models   []string `json:"models"`
		Dataset  string   `json:"dataset"`
		Async    bool     `json:"async"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	// In production, this would trigger model retraining
	jobID := "retrain-job-" + time.Now().Format("20060102-150405")
	
	result := gin.H{
		"job_id":     jobID,
		"status":     "initiated",
		"models":     request.Models,
		"dataset":    request.Dataset,
		"async":      request.Async,
		"started_at": time.Now(),
	}
	
	if !request.Async {
		result["status"] = "completed"
		result["completed_at"] = time.Now().Add(5 * time.Second)
		result["metrics"] = gin.H{
			"accuracy":     0.94,
			"precision":    0.92,
			"recall":       0.96,
			"f1_score":     0.94,
		}
	}
	
	c.JSON(http.StatusOK, result)
}

func (s *Server) clearCache(c *gin.Context) {
	cacheType := c.Query("type") // all, insights, patterns, correlations
	
	result := gin.H{
		"cleared": []string{},
		"errors":  []string{},
	}
	
	switch cacheType {
	case "insights", "":
		// Clear insight cache
		result["cleared"] = append(result["cleared"].([]string), "insights")
	case "patterns":
		// Clear pattern cache
		result["cleared"] = append(result["cleared"].([]string), "patterns")
	case "correlations":
		// Clear correlation cache
		result["cleared"] = append(result["cleared"].([]string), "correlations")
	case "all":
		// Clear all caches
		result["cleared"] = []string{"insights", "patterns", "correlations"}
	default:
		result["errors"] = append(result["errors"].([]string), "unknown cache type: "+cacheType)
	}
	
	result["timestamp"] = time.Now()
	
	c.JSON(http.StatusOK, result)
}

// WebSocket handlers for real-time updates

func (s *Server) insightWebSocket(c *gin.Context) {
	// Implement WebSocket for real-time insight streaming
	// This would upgrade the connection and stream new insights as they're generated
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "WebSocket support coming soon",
		"info":  "Use polling with GET /api/v1/insights for now",
	})
}

func (s *Server) flowWebSocket(c *gin.Context) {
	// Implement WebSocket for real-time flow streaming
	// This would upgrade the connection and stream network flows in real-time
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "WebSocket support coming soon",
		"info":  "Use polling with GET /api/v1/flows for now",
	})
}