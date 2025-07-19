package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/server/adapters/correlation"
	serverDomain "github.com/yairfalse/tapio/pkg/server/domain"
)

// RESTServer provides REST API endpoints for tapio-server
type RESTServer struct {
	correlationAdapter *correlation.CorrelationAdapter
	router             *mux.Router
	server             *http.Server
}

// NewRESTServer creates a new REST API server
func NewRESTServer(port int, correlationAdapter *correlation.CorrelationAdapter) *RESTServer {
	rs := &RESTServer{
		correlationAdapter: correlationAdapter,
		router:             mux.NewRouter(),
	}

	// Setup routes
	rs.setupRoutes()

	// Create HTTP server
	rs.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      rs.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return rs
}

// setupRoutes configures all REST API routes
func (rs *RESTServer) setupRoutes() {
	// API v1 routes
	api := rs.router.PathPrefix("/api/v1").Subrouter()

	// Health check
	rs.router.HandleFunc("/health", rs.handleHealth).Methods("GET")

	// Check endpoints
	api.HandleFunc("/check", rs.handleClusterCheck).Methods("GET")
	api.HandleFunc("/check/{namespace}", rs.handleNamespaceCheck).Methods("GET")
	api.HandleFunc("/check/{namespace}/{resource}", rs.handleResourceCheck).Methods("GET")

	// Findings endpoints
	api.HandleFunc("/findings", rs.handleGetFindings).Methods("GET")
	api.HandleFunc("/findings", rs.handleSubmitFinding).Methods("POST")

	// Correlation endpoint
	api.HandleFunc("/correlate", rs.handleCorrelate).Methods("POST")

	// Status endpoint
	api.HandleFunc("/status", rs.handleStatus).Methods("GET")

	// Add middleware
	rs.router.Use(loggingMiddleware)
	rs.router.Use(corsMiddleware)
}

// Start starts the REST server
func (rs *RESTServer) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rs.server.Shutdown(shutdownCtx)
	}()

	return rs.server.ListenAndServe()
}

// Stop gracefully stops the REST server
func (rs *RESTServer) Stop(ctx context.Context) error {
	return rs.server.Shutdown(ctx)
}

// handleHealth handles health check requests
func (rs *RESTServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleClusterCheck handles cluster-wide checks
func (rs *RESTServer) handleClusterCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get insights from correlation adapter
	insights, err := rs.correlationAdapter.GetInsights(ctx, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"status":   "ok",
		"insights": insights,
		"checked":  time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleNamespaceCheck handles namespace-specific checks
func (rs *RESTServer) handleNamespaceCheck(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	namespace := vars["namespace"]
	ctx := r.Context()

	// Get insights for specific namespace
	insights, err := rs.correlationAdapter.GetInsights(ctx, namespace, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"namespace": namespace,
		"status":    "ok",
		"insights":  insights,
		"checked":   time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleResourceCheck handles resource-specific checks
func (rs *RESTServer) handleResourceCheck(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	namespace := vars["namespace"]
	resource := vars["resource"]
	ctx := r.Context()

	// Get insights for specific resource
	insights, err := rs.correlationAdapter.GetInsights(ctx, namespace, resource)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get predictions if available
	predictions, _ := rs.correlationAdapter.GetPredictions(ctx, namespace, resource)

	response := map[string]interface{}{
		"namespace":   namespace,
		"resource":    resource,
		"status":      "ok",
		"insights":    insights,
		"predictions": predictions,
		"checked":     time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetFindings handles GET requests for findings
func (rs *RESTServer) handleGetFindings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	namespace := r.URL.Query().Get("namespace")
	resource := r.URL.Query().Get("resource")

	// Get insights as findings from correlation adapter
	insights, err := rs.correlationAdapter.GetInsights(ctx, resource, namespace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert insights to findings format
	findings := make([]domain.Finding, len(insights))
	for i, insight := range insights {
		findings[i] = domain.Finding{
			ID:          domain.FindingID(insight.ID),
			Type:        domain.FindingType(insight.Category),
			Severity:    domain.Severity(insight.Severity),
			Title:       insight.Title,
			Description: insight.Description,
			Timestamp:   insight.Timestamp,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(findings)
}

// handleSubmitFinding handles POST requests to submit findings
func (rs *RESTServer) handleSubmitFinding(w http.ResponseWriter, r *http.Request) {
	// Since the adapter doesn't have ProcessFinding, we'll convert to an event
	var finding domain.Finding
	if err := json.NewDecoder(r.Body).Decode(&finding); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// For now, just acknowledge receipt
	// In a real implementation, this would be processed through the correlation engine

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "created",
		"id":     string(finding.ID),
	})
}

// handleCorrelate handles correlation requests
func (rs *RESTServer) handleCorrelate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var request struct {
		Events []map[string]interface{} `json:"events"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Convert to server domain events
	serverEvents := make([]*serverDomain.Event, len(request.Events))
	for i, eventData := range request.Events {
		serverEvents[i] = &serverDomain.Event{
			ID:        fmt.Sprintf("event-%d", i),
			Type:      serverDomain.EventType(getStringField(eventData, "type", "unknown")),
			Severity:  serverDomain.EventSeverity(getStringField(eventData, "severity", "info")),
			Source:    getStringField(eventData, "source", "api"),
			Message:   getStringField(eventData, "message", ""),
			Timestamp: time.Now(),
			Data:      eventData,
		}
	}

	// Correlate events
	result, err := rs.correlationAdapter.CorrelateEvents(ctx, serverEvents)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"processed":    len(request.Events),
		"correlations": result.Correlations,
		"timestamp":    result.Timestamp,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleStatus handles server status requests
func (rs *RESTServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := rs.correlationAdapter.GetStats(ctx)
	if err != nil {
		stats = &correlation.Stats{} // Empty stats on error
	}

	response := map[string]interface{}{
		"status":      "running",
		"correlation": rs.correlationAdapter.IsEnabled(),
		"stats":       stats,
		"uptime":      time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// Simple logging - can be enhanced with structured logging
		fmt.Printf("[%s] %s %s - %v\n", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

// corsMiddleware adds CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getStringField safely extracts a string field from a map
func getStringField(data map[string]interface{}, field, defaultValue string) string {
	if val, ok := data[field]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}
