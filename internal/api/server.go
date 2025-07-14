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

// Server provides REST API for CLI and external tools
type Server struct {
	router           *gin.Engine
	correlationEngine *correlation.PerfectEngine
	insightStore     correlation.InsightStore
	logger           *zap.Logger
	config           *Config
}

// Config for API server
type Config struct {
	Port              string        `yaml:"port"`
	EnableCORS        bool          `yaml:"enable_cors"`
	RateLimitPerMin   int           `yaml:"rate_limit_per_min"`
	AuthEnabled       bool          `yaml:"auth_enabled"`
	MetricsEnabled    bool          `yaml:"metrics_enabled"`
	CacheTimeout      time.Duration `yaml:"cache_timeout"`
}

// NewServer creates a new API server
func NewServer(engine *correlation.PerfectEngine, config *Config) *Server {
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
		insightStore:      correlation.NewInMemoryInsightStore(),
		logger:            zap.NewNop(), // Should inject real logger
		config:            config,
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

// setupMiddleware configures middleware
func (s *Server) setupMiddleware() {
	// Recovery middleware
	s.router.Use(gin.Recovery())

	// Logging middleware
	s.router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return ""  // Use structured logging instead
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
func (s *Server) setupRoutes() {
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

		// Health status
		v1.GET("/health/:namespace/:resource", s.getResourceHealth)
		v1.GET("/health/cluster", s.getClusterHealth)

		// Correlation queries
		v1.POST("/correlate", s.correlateEvents)
		v1.GET("/patterns", s.listPatterns)
		v1.GET("/patterns/:patternId/matches", s.getPatternMatches)

		// Flow visibility
		v1.GET("/flows", s.listNetworkFlows)
		v1.GET("/flows/:namespace/:pod", s.getPodFlows)
		v1.GET("/flows/l7/:protocol", s.getL7Flows)

		// WebSocket for real-time updates
		v1.GET("/ws/insights", s.insightWebSocket)
		v1.GET("/ws/flows", s.flowWebSocket)
	}

	// Admin endpoints
	admin := s.router.Group("/admin")
	if s.config.AuthEnabled {
		admin.Use(authMiddleware())
	}
	{
		admin.GET("/metrics", s.getMetrics)
		admin.POST("/correlation/retrain", s.retrainModels)
		admin.DELETE("/cache", s.clearCache)
	}
}

// Handler implementations

func (s *Server) getResourceInsights(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")

	// Query parameters
	severity := c.Query("severity")     // filter by severity
	category := c.Query("category")     // filter by category
	limit := c.DefaultQuery("limit", "50")

	insights := s.insightStore.GetInsights(resource, namespace)

	// Filter insights
	var filtered []*correlation.Insight
	for _, insight := range insights {
		if severity != "" && insight.Severity != severity {
			continue
		}
		if category != "" && insight.Category != category {
			continue
		}
		filtered = append(filtered, insight)
	}

	c.JSON(http.StatusOK, gin.H{
		"resource":  resource,
		"namespace": namespace,
		"insights":  filtered,
		"count":     len(filtered),
		"timestamp": time.Now(),
	})
}

func (s *Server) getResourcePredictions(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")

	insights := s.insightStore.GetInsights(resource, namespace)
	
	var predictions []*PredictionResponse
	for _, insight := range insights {
		if insight.Prediction != nil {
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
		"resource":    resource,
		"namespace":   namespace,
		"predictions": predictions,
		"count":       len(predictions),
	})
}

func (s *Server) getResourceFixes(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	autoFixOnly := c.Query("auto_fix_only") == "true"

	insights := s.insightStore.GetInsights(resource, namespace)
	
	var fixes []*FixResponse
	for _, insight := range insights {
		for _, item := range insight.ActionableItems {
			if autoFixOnly && item.Risk != "low" {
				continue
			}
			
			fixes = append(fixes, &FixResponse{
				ID:          generateFixID(insight.ID, item),
				InsightID:   insight.ID,
				Description: item.Description,
				Command:     item.Command,
				Impact:      item.Impact,
				Risk:        item.Risk,
				AutoFixable: item.Risk == "low",
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"resource":  resource,
		"namespace": namespace,
		"fixes":     fixes,
		"count":     len(fixes),
	})
}

func (s *Server) getResourceHealth(c *gin.Context) {
	namespace := c.Param("namespace")
	resource := c.Param("resource")

	// Get health from resilience health checker
	health := resilience.GetGlobalHealth()
	
	// Combine with insights
	insights := s.insightStore.GetInsights(resource, namespace)
	
	criticalCount := 0
	warningCount := 0
	for _, insight := range insights {
		switch insight.Severity {
		case "critical":
			criticalCount++
		case "high", "medium":
			warningCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"resource":        resource,
		"namespace":       namespace,
		"status":          health.OverallStatus.String(),
		"score":           health.OverallScore,
		"criticalIssues":  criticalCount,
		"warnings":        warningCount,
		"lastChecked":     health.ComputedAt,
		"components":      health.ComponentStatuses,
	})
}

func (s *Server) listNetworkFlows(c *gin.Context) {
	// Query parameters
	limit := c.DefaultQuery("limit", "100")
	protocol := c.Query("protocol")        // tcp, udp, http, grpc
	direction := c.Query("direction")      // ingress, egress
	namespace := c.Query("namespace")
	
	// TODO: Query flow collector
	flows := []gin.H{
		{
			"id":           "flow-1",
			"source":       "frontend-7d5c4-xyz",
			"destination":  "api-service-8f9d2-abc", 
			"protocol":     "http",
			"method":       "POST",
			"path":         "/api/users",
			"status":       200,
			"latency":      "45ms",
			"bytesIn":      1024,
			"bytesOut":     2048,
			"timestamp":    time.Now(),
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"flows":     flows,
		"count":     len(flows),
		"filters":   gin.H{
			"protocol":  protocol,
			"direction": direction,
			"namespace": namespace,
		},
	})
}

func (s *Server) getL7Flows(c *gin.Context) {
	protocol := c.Param("protocol") // http, grpc, kafka
	
	switch protocol {
	case "http":
		c.JSON(http.StatusOK, gin.H{
			"protocol": "http",
			"flows": []gin.H{
				{
					"method":      "GET",
					"path":        "/api/health",
					"status":      200,
					"latency":     "2ms",
					"userAgent":   "kube-probe/1.28",
					"requests":    1523,
					"errors":      0,
					"p95Latency":  "5ms",
				},
			},
		})
	case "grpc":
		c.JSON(http.StatusOK, gin.H{
			"protocol": "grpc",
			"flows": []gin.H{
				{
					"service":     "correlation.EventCollector",
					"method":      "StreamEvents",
					"status":      "OK",
					"streams":     42,
					"messages":    165000,
					"errors":      0,
					"p95Latency":  "500µs",
				},
			},
		})
	case "kafka":
		c.JSON(http.StatusOK, gin.H{
			"protocol": "kafka",
			"flows": []gin.H{
				{
					"topic":       "events",
					"partition":   0,
					"producer":    "collector-abc",
					"consumer":    "processor-xyz",
					"messages":    85000,
					"lag":         120,
					"throughput":  "10MB/s",
				},
			},
		})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported protocol"})
	}
}

func (s *Server) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now(),
	})
}

func (s *Server) readinessCheck(c *gin.Context) {
	// Check if correlation engine is ready
	if s.correlationEngine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not ready",
			"reason": "correlation engine not initialized",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
		"time":   time.Now(),
	})
}

// Response types
type PredictionResponse struct {
	ID          string        `json:"id"`
	Type        string        `json:"type"`
	Title       string        `json:"title"`
	TimeToEvent time.Duration `json:"time_to_event,omitempty"`
	Probability float64       `json:"probability"`
	Confidence  float64       `json:"confidence"`
	Severity    string        `json:"severity"`
	CreatedAt   time.Time     `json:"created_at"`
}

type FixResponse struct {
	ID          string `json:"id"`
	InsightID   string `json:"insight_id"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Impact      string `json:"impact"`
	Risk        string `json:"risk"`
	AutoFixable bool   `json:"auto_fixable"`
}

// Helper functions
func generateFixID(insightID string, item *correlation.ActionableItem) string {
	// Simple ID generation - could use hash
	return insightID + "-fix-" + item.Description[:10]
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	}
}

func rateLimitMiddleware(perMin int) gin.HandlerFunc {
	// Simple implementation - use proper rate limiter in production
	return func(c *gin.Context) {
		c.Next()
	}
}

func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)
		
		// Record metrics
		_ = duration // Record to prometheus
	}
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		// Validate token
		c.Next()
	}
}

// WebSocket handlers would go here...
func (s *Server) insightWebSocket(c *gin.Context) {
	// Implement WebSocket for real-time insights
}

func (s *Server) flowWebSocket(c *gin.Context) {
	// Implement WebSocket for real-time flows
}

// Additional handlers...
func (s *Server) correlateEvents(c *gin.Context) {
	// POST endpoint to correlate specific events
}

func (s *Server) listPatterns(c *gin.Context) {
	// List available correlation patterns
}

func (s *Server) getPatternMatches(c *gin.Context) {
	// Get matches for a specific pattern
}

func (s *Server) listAllInsights(c *gin.Context) {
	// List all insights with pagination
}

func (s *Server) listActivePredictions(c *gin.Context) {
	// List all active predictions
}

func (s *Server) applyFix(c *gin.Context) {
	// Apply a specific fix (with safety checks)
}

func (s *Server) getClusterHealth(c *gin.Context) {
	// Get overall cluster health
}

func (s *Server) getMetrics(c *gin.Context) {
	// Admin endpoint for internal metrics
}

func (s *Server) retrainModels(c *gin.Context) {
	// Admin endpoint to retrain correlation models
}

func (s *Server) clearCache(c *gin.Context) {
	// Admin endpoint to clear caches
}

// Start starts the API server
func (s *Server) Start() error {
	return s.router.Run(":" + s.config.Port)
}

// Stop gracefully stops the server
func (s *Server) Stop(ctx context.Context) error {
	// Implement graceful shutdown
	return nil
}