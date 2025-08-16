package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

func TestAPIServer_HandleHealth_NoDriver(t *testing.T) {
	// Create test logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := &APIServer{
		router: chi.NewRouter(),
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		tracer: otel.Tracer("test"),
	}
	server.setupRouter()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response HealthResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "unhealthy", response.Status)
	assert.Equal(t, "unhealthy", response.Services["neo4j"])
}

func TestAPIServer_HandleWhy_MissingPod(t *testing.T) {
	// Create test logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := &APIServer{
		router: chi.NewRouter(),
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		tracer: otel.Tracer("test"),
	}
	server.setupRouter()

	req := httptest.NewRequest("GET", "/api/v1/why", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "pod parameter is required")
}

func TestAPIServer_HandleImpact_MissingService(t *testing.T) {
	// Create test logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := &APIServer{
		router: chi.NewRouter(),
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		tracer: otel.Tracer("test"),
	}
	server.setupRouter()

	req := httptest.NewRequest("GET", "/api/v1/impact", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "service parameter is required")
}

func TestAPIServer_CORSMiddleware(t *testing.T) {
	// Create test logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := chi.NewRouter()
	server := &APIServer{
		router: router,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		tracer: otel.Tracer("test"),
	}
	router.Use(server.corsMiddleware)
	server.setupRouter()

	req := httptest.NewRequest("OPTIONS", "/api/v1/health", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, DELETE, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type, Authorization", w.Header().Get("Access-Control-Allow-Headers"))
}
