package api

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code"`
}

// CheckRequest represents a check request
type CheckRequest struct {
	Namespace string `json:"namespace,omitempty"`
	Resource  string `json:"resource,omitempty"`
	Severity  string `json:"severity,omitempty"`
}

// CheckResponse represents a check response
type CheckResponse struct {
	Status      string      `json:"status"`
	Namespace   string      `json:"namespace,omitempty"`
	Resource    string      `json:"resource,omitempty"`
	Insights    interface{} `json:"insights"`
	Predictions interface{} `json:"predictions,omitempty"`
	Checked     int64       `json:"checked"`
}

// CorrelationRequest represents a correlation request
type CorrelationRequest struct {
	Events    []interface{} `json:"events"`
	TimeRange struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	} `json:"time_range,omitempty"`
}

// CorrelationResponse represents a correlation response
type CorrelationResponse struct {
	Processed    int           `json:"processed"`
	Correlations []interface{} `json:"correlations"`
}

// StatusResponse represents a server status response
type StatusResponse struct {
	Status      string                 `json:"status"`
	Version     string                 `json:"version"`
	Correlation bool                   `json:"correlation"`
	Stats       map[string]interface{} `json:"stats"`
	Uptime      int64                  `json:"uptime"`
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes an error response
func writeError(w http.ResponseWriter, status int, err error) {
	resp := ErrorResponse{
		Error: http.StatusText(status),
		Code:  status,
	}
	if err != nil {
		resp.Message = err.Error()
	}
	writeJSON(w, status, resp)
}
