package transports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yairfalse/tapio/pkg/server/domain"
)

// HTTPTransport implements HTTP server transport
type HTTPTransport struct {
	server         *http.Server
	router         chi.Router
	config         *domain.EndpointConfig
	requestHandler domain.RequestHandler
	logger         domain.Logger

	mu        sync.RWMutex
	isStarted bool
	handlers  map[string]http.HandlerFunc
}

// NewHTTPTransport creates a new HTTP transport
func NewHTTPTransport(
	config *domain.EndpointConfig,
	requestHandler domain.RequestHandler,
	logger domain.Logger,
) *HTTPTransport {
	router := chi.NewRouter()

	// Setup base middleware
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(config.Timeout))

	return &HTTPTransport{
		router:         router,
		config:         config,
		requestHandler: requestHandler,
		logger:         logger,
		handlers:       make(map[string]http.HandlerFunc),
	}
}

// Start starts the HTTP server
func (t *HTTPTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.isStarted {
		return domain.ErrServerAlreadyRunning()
	}

	// Setup routes
	t.setupRoutes()

	// Create HTTP server
	t.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", t.config.Address, t.config.Port),
		Handler:      t.router,
		ReadTimeout:  t.config.Timeout,
		WriteTimeout: t.config.Timeout,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background
	go func() {
		if t.logger != nil {
			t.logger.Info(ctx, fmt.Sprintf("HTTP server starting on %s", t.server.Addr))
		}

		if err := t.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if t.logger != nil {
				t.logger.Error(ctx, fmt.Sprintf("HTTP server error: %v", err))
			}
		}
	}()

	t.isStarted = true
	return nil
}

// Stop stops the HTTP server
func (t *HTTPTransport) Stop(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.isStarted {
		return domain.NewServerError(domain.ErrorCodeServerNotStarted, "HTTP transport not started")
	}

	if t.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := t.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shutdown HTTP server: %w", err)
		}
	}

	t.isStarted = false
	return nil
}

// Name returns the transport name
func (t *HTTPTransport) Name() string {
	return "http"
}

// Protocol returns the transport protocol
func (t *HTTPTransport) Protocol() string {
	return "http"
}

// Address returns the transport address
func (t *HTTPTransport) Address() string {
	return fmt.Sprintf("%s:%d", t.config.Address, t.config.Port)
}

// HandleRequest handles an HTTP request by converting it to domain request
func (t *HTTPTransport) HandleRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	return t.requestHandler.HandleRequest(ctx, request)
}

// Configure configures the transport
func (t *HTTPTransport) Configure(ctx context.Context, config *domain.EndpointConfig) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.isStarted {
		return domain.ErrInvalidRequest("cannot configure running transport")
	}

	t.config = config
	return nil
}

// setupRoutes sets up all HTTP routes
func (t *HTTPTransport) setupRoutes() {
	// Health check endpoint
	t.router.Get("/health", t.handleHealth)

	// Metrics endpoint
	t.router.Get("/metrics", t.handleMetrics)

	// API routes
	t.router.Route("/api", func(r chi.Router) {
		r.Use(t.apiMiddleware)

		// Server endpoints
		r.Get("/status", t.handleStatus)
		r.Get("/config", t.handleConfig)
		r.Post("/config", t.handleUpdateConfig)

		// Connection endpoints
		r.Get("/connections", t.handleGetConnections)
		r.Delete("/connections/{id}", t.handleCloseConnection)

		// Event endpoints
		r.Post("/events", t.handlePublishEvent)

		// Generic request endpoint
		r.Post("/request", t.handleGenericRequest)
	})
}

// apiMiddleware adds API-specific middleware
func (t *HTTPTransport) apiMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Add request timing
		start := time.Now()

		// Create wrapped writer to capture status
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(ww, r)

		// Log request
		if t.logger != nil {
			t.logger.Info(r.Context(), fmt.Sprintf("%s %s %d %v",
				r.Method, r.URL.Path, ww.statusCode, time.Since(start)))
		}
	})
}

// Response writer wrapper to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// HTTP Handlers

func (t *HTTPTransport) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	request := &domain.Request{
		ID:        middleware.GetReqID(ctx),
		Type:      domain.RequestTypeHealth,
		Timestamp: time.Now(),
		Source:    r.RemoteAddr,
		Context:   ctx,
	}

	response, err := t.HandleRequest(ctx, request)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	t.writeJSON(w, response.Data)
}

func (t *HTTPTransport) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	request := &domain.Request{
		ID:        middleware.GetReqID(ctx),
		Type:      domain.RequestTypeMetrics,
		Timestamp: time.Now(),
		Source:    r.RemoteAddr,
		Context:   ctx,
	}

	response, err := t.HandleRequest(ctx, request)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	t.writeJSON(w, response.Data)
}

func (t *HTTPTransport) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	request := &domain.Request{
		ID:        middleware.GetReqID(ctx),
		Type:      domain.RequestTypeQuery,
		Timestamp: time.Now(),
		Source:    r.RemoteAddr,
		Data:      map[string]interface{}{"type": "server_status"},
		Context:   ctx,
	}

	response, err := t.HandleRequest(ctx, request)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	t.writeJSON(w, response.Data)
}

func (t *HTTPTransport) handleConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	request := &domain.Request{
		ID:        middleware.GetReqID(ctx),
		Type:      domain.RequestTypeQuery,
		Timestamp: time.Now(),
		Source:    r.RemoteAddr,
		Data:      map[string]interface{}{"type": "server_config"},
		Context:   ctx,
	}

	response, err := t.HandleRequest(ctx, request)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	t.writeJSON(w, response.Data)
}

func (t *HTTPTransport) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var config domain.Configuration
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		t.writeError(w, err, http.StatusBadRequest)
		return
	}

	request := &domain.Request{
		ID:        middleware.GetReqID(ctx),
		Type:      domain.RequestTypeCommand,
		Timestamp: time.Now(),
		Source:    r.RemoteAddr,
		Data: map[string]interface{}{
			"action": "update_config",
			"config": config,
		},
		Context: ctx,
	}

	response, err := t.HandleRequest(ctx, request)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	t.writeJSON(w, response.Data)
}

func (t *HTTPTransport) handleGetConnections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	request := &domain.Request{
		ID:        middleware.GetReqID(ctx),
		Type:      domain.RequestTypeQuery,
		Timestamp: time.Now(),
		Source:    r.RemoteAddr,
		Data:      map[string]interface{}{"type": "connections"},
		Context:   ctx,
	}

	response, err := t.HandleRequest(ctx, request)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	t.writeJSON(w, response.Data)
}

func (t *HTTPTransport) handleCloseConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	connectionID := chi.URLParam(r, "id")

	request := &domain.Request{
		ID:        middleware.GetReqID(ctx),
		Type:      domain.RequestTypeCommand,
		Timestamp: time.Now(),
		Source:    r.RemoteAddr,
		Data: map[string]interface{}{
			"action":        "close_connection",
			"connection_id": connectionID,
		},
		Context: ctx,
	}

	response, err := t.HandleRequest(ctx, request)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	t.writeJSON(w, response.Data)
}

func (t *HTTPTransport) handlePublishEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var event domain.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		t.writeError(w, err, http.StatusBadRequest)
		return
	}

	request := &domain.Request{
		ID:        middleware.GetReqID(ctx),
		Type:      domain.RequestTypeEvent,
		Timestamp: time.Now(),
		Source:    r.RemoteAddr,
		Data:      &event,
		Context:   ctx,
	}

	_, err := t.HandleRequest(ctx, request)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	t.writeJSON(w, map[string]interface{}{
		"status":  "accepted",
		"eventId": event.ID,
	})
}

func (t *HTTPTransport) handleGenericRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req domain.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.writeError(w, err, http.StatusBadRequest)
		return
	}

	// Override context and metadata
	req.Context = ctx
	req.Source = r.RemoteAddr
	if req.ID == "" {
		req.ID = middleware.GetReqID(ctx)
	}

	response, err := t.HandleRequest(ctx, &req)
	if err != nil {
		t.writeError(w, err, http.StatusInternalServerError)
		return
	}

	t.writeJSON(w, response)
}

// Helper methods

func (t *HTTPTransport) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		if t.logger != nil {
			t.logger.Error(context.Background(), fmt.Sprintf("failed to encode response: %v", err))
		}
	}
}

func (t *HTTPTransport) writeError(w http.ResponseWriter, err error, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResponse := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    statusCode,
			"message": err.Error(),
		},
	}

	if serverErr, ok := err.(*domain.ServerError); ok {
		errorResponse["error"].(map[string]interface{})["code"] = serverErr.Code
		errorResponse["error"].(map[string]interface{})["context"] = serverErr.Context
	}

	json.NewEncoder(w).Encode(errorResponse)
}
