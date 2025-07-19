package internal

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestGRPCClient_NewClient(t *testing.T) {
	client, err := NewGRPCClient("localhost:50051")
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}

	if client.serverAddr != "localhost:50051" {
		t.Errorf("Expected server address localhost:50051, got %s", client.serverAddr)
	}

	if client.collectorType != "systemd" {
		t.Errorf("Expected collector type systemd, got %s", client.collectorType)
	}

	if client.collectorID == "" {
		t.Error("Expected non-empty collector ID")
	}
}

func TestGRPCClient_ConvertSeverity(t *testing.T) {
	client := &GRPCClient{}

	tests := []struct {
		name     string
		input    domain.EventSeverity
		expected string
	}{
		{"Debug", domain.EventSeverityDebug, "EVENT_SEVERITY_DEBUG"},
		{"Info", domain.EventSeverityInfo, "EVENT_SEVERITY_INFO"},
		{"Warning", domain.EventSeverityWarning, "EVENT_SEVERITY_WARNING"},
		{"Error", domain.EventSeverityError, "EVENT_SEVERITY_ERROR"},
		{"Critical", domain.EventSeverityCritical, "EVENT_SEVERITY_CRITICAL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.domainSeverityToProto(tt.input)
			if result.String() != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

func TestGRPCClient_ConvertSource(t *testing.T) {
	client := &GRPCClient{}

	tests := []struct {
		name     string
		input    domain.SourceType
		expected string
	}{
		{"Systemd", domain.SourceSystemd, "SOURCE_TYPE_SYSTEMD"},
		{"EBPF", domain.SourceEBPF, "SOURCE_TYPE_EBPF"},
		{"K8s", domain.SourceK8s, "SOURCE_TYPE_KUBERNETES"},
		{"Journald", domain.SourceJournald, "SOURCE_TYPE_JOURNALD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.domainSourceToProto(tt.input)
			if result.String() != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

func TestGRPCClient_IsConnected(t *testing.T) {
	client := &GRPCClient{}

	if client.IsConnected() {
		t.Error("Expected IsConnected to return false for unconnected client")
	}

	client.isActive = true
	if !client.IsConnected() {
		t.Error("Expected IsConnected to return true when isActive is true")
	}
}

func TestGRPCClient_SendEventNotConnected(t *testing.T) {
	client := &GRPCClient{}

	event := domain.Event{
		ID:        "test-1",
		Type:      domain.EventTypeService,
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"test": "data"},
	}

	err := client.SendEvent(context.Background(), event)
	if err == nil {
		t.Error("Expected error when sending to unconnected client")
	}

	if err.Error() != "gRPC client not connected" {
		t.Errorf("Expected 'gRPC client not connected' error, got: %v", err)
	}
}