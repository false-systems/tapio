package dataflow

import (
    "context"
    "encoding/json"
    "log"
    "net/http"
    "github.com/yairfalse/tapio/pkg/domain"
)

// ServerBridge connects dataflow to server APIs
type ServerBridge struct {
    manager *Manager
}

// NewServerBridge creates a server bridge
func NewServerBridge(manager *Manager) *ServerBridge {
    return &ServerBridge{manager: manager}
}

// Start begins forwarding findings to server endpoints
func (sb *ServerBridge) Start(ctx context.Context) {
    findings := sb.manager.Findings()
    
    go func() {
        for {
            select {
            case finding := <-findings:
                sb.forwardToServer(finding)
            case <-ctx.Done():
                return
            }
        }
    }()
}

// forwardToServer sends findings to server endpoints
func (sb *ServerBridge) forwardToServer(finding *domain.Finding) {
    // Convert to JSON for server APIs
    data, err := json.Marshal(finding)
    if err != nil {
        log.Printf("JSON marshal error: %v", err)
        return
    }
    
    // Send to REST API endpoint (example)
    // This would connect to your existing server
    log.Printf("Finding: %s", string(data))
    
    // TODO: Connect to actual server endpoints:
    // - REST API: POST /api/findings
    // - gRPC: call PublishFinding()
    // - GUI: WebSocket updates
}

// HTTPHandler provides HTTP endpoint for findings
func (sb *ServerBridge) HTTPHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        
        // In real implementation, this would stream findings
        // For now, just return status
        response := map[string]string{
            "status": "dataflow active",
            "message": "findings are being processed",
        }
        
        json.NewEncoder(w).Encode(response)
    }
}
