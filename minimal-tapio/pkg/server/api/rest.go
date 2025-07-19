package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/yairfalse/tapio/pkg/domain"
)

// ServerInterface defines what the REST API needs from the server
type ServerInterface interface {
	GetHealth() domain.HealthStatus
	ProcessEvent(ctx context.Context, event domain.Event) error
	GetEvents(ctx context.Context, limit int) []domain.Event
	GetFindings(ctx context.Context) []domain.Finding
	CorrelateEvents(ctx context.Context, events []domain.Event) (*domain.Correlation, error)
}

// RESTServer provides REST API endpoints
type RESTServer struct {
	server ServerInterface
	router *mux.Router
}

// NewRESTServer creates a new REST API server
func NewRESTServer(server ServerInterface) *RESTServer {
	rs := &RESTServer{
		server: server,
		router: mux.NewRouter(),
	}

	rs.setupRoutes()
	return rs
}

// Router returns the HTTP router
func (rs *RESTServer) Router() *mux.Router {
	return rs.router
}

// setupRoutes configures all REST API routes
func (rs *RESTServer) setupRoutes() {
	// Health check
	rs.router.HandleFunc("/health", rs.handleHealth).Methods("GET")

	// API v1 routes
	api := rs.router.PathPrefix("/api/v1").Subrouter()

	// Event endpoints
	api.HandleFunc("/events", rs.handleSubmitEvent).Methods("POST")
	api.HandleFunc("/events", rs.handleGetEvents).Methods("GET")

	// Findings endpoints
	api.HandleFunc("/findings", rs.handleGetFindings).Methods("GET")

	// Correlation endpoint
	api.HandleFunc("/correlate", rs.handleCorrelate).Methods("POST")

	// Status endpoint
	api.HandleFunc("/status", rs.handleStatus).Methods("GET")

	// Add middleware
	rs.router.Use(loggingMiddleware)
	rs.router.Use(corsMiddleware)
}

// handleHealth handles health check requests
func (rs *RESTServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := rs.server.GetHealth()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// handleSubmitEvent handles event submission
func (rs *RESTServer) handleSubmitEvent(w http.ResponseWriter, r *http.Request) {
	var event domain.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Generate ID if missing
	if event.ID == "" {
		event.ID = domain.EventID(fmt.Sprintf("event-%d", time.Now().UnixNano()))
	}

	// Process event
	if err := rs.server.ProcessEvent(r.Context(), event); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"status": "accepted",
		"id":     event.ID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// handleGetEvents handles event retrieval
func (rs *RESTServer) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	// Get limit from query param
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events := rs.server.GetEvents(r.Context(), limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// handleGetFindings handles findings retrieval
func (rs *RESTServer) handleGetFindings(w http.ResponseWriter, r *http.Request) {
	findings := rs.server.GetFindings(r.Context())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(findings)
}

// handleCorrelate handles correlation requests
func (rs *RESTServer) handleCorrelate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Events []domain.Event `json:"events"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	correlation, err := rs.server.CorrelateEvents(r.Context(), request.Events)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(correlation)
}

// handleStatus handles status requests
func (rs *RESTServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":    "running",
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s %s - %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
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
