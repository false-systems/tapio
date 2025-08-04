package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIServer_HandleHealth_NoDriver(t *testing.T) {
	server := &APIServer{
		router: mux.NewRouter(),
	}
	server.setupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response HealthResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "unhealthy", response.Services["neo4j"])
}

func TestAPIServer_HandleWhy_MissingPod(t *testing.T) {
	server := &APIServer{
		router: mux.NewRouter(),
	}
	server.setupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/why", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "pod parameter is required")
}

func TestAPIServer_HandleImpact_MissingService(t *testing.T) {
	server := &APIServer{
		router: mux.NewRouter(),
	}
	server.setupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/impact", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "service parameter is required")
}

func TestAPIServer_CORSMiddleware(t *testing.T) {
	router := mux.NewRouter()
	router.Use(corsMiddleware)
	server := &APIServer{
		router: router,
	}
	server.setupRoutes()

	req := httptest.NewRequest("OPTIONS", "/api/v1/health", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
}
