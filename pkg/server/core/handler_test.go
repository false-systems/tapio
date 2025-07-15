package core

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

type mockValidator struct{}

func (m *mockValidator) ValidateRequest(ctx context.Context, request *domain.Request) error {
	return nil
}

func (m *mockValidator) ValidateResponse(ctx context.Context, response *domain.Response) error {
	return nil
}

func (m *mockValidator) ValidateConfiguration(ctx context.Context, config *domain.Configuration) error {
	return nil
}

func (m *mockValidator) ValidateData(ctx context.Context, data interface{}, schema string) error {
	return nil
}

type mockSerializer struct{}

func (m *mockSerializer) Serialize(ctx context.Context, data interface{}) ([]byte, error) {
	return []byte("serialized"), nil
}

func (m *mockSerializer) Deserialize(ctx context.Context, data []byte, target interface{}) error {
	return nil
}

func (m *mockSerializer) ContentType() string {
	return "application/json"
}

func (m *mockSerializer) SupportsCompression() bool {
	return false
}

func (m *mockSerializer) Compress(ctx context.Context, data []byte) ([]byte, error) {
	return data, nil
}

func (m *mockSerializer) Decompress(ctx context.Context, data []byte) ([]byte, error) {
	return data, nil
}

func createTestRequestHandler() *RequestHandler {
	service := createTestService()
	validator := &mockValidator{}
	serializer := &mockSerializer{}
	metricsCollector := &mockMetricsCollector{}
	eventPublisher := &mockEventPublisher{events: make([]*domain.Event, 0)}
	logger := &mockLogger{messages: make([]string, 0)}

	return NewRequestHandler(
		service,
		validator,
		serializer,
		metricsCollector,
		eventPublisher,
		logger,
	)
}

func TestNewRequestHandlerFixed(t *testing.T) {
	handler := createTestRequestHandler()
	
	if handler == nil {
		t.Error("Expected request handler to be created, got nil")
	}
	
	if handler.serverService == nil {
		t.Error("Expected request handler service to be set, got nil")
	}
	
	if handler.logger == nil {
		t.Error("Expected request handler logger to be set, got nil")
	}
}

func TestRequestHandlerValidateRequestFixed(t *testing.T) {
	handler := createTestRequestHandler()
	ctx := context.Background()
	
	// Test valid request
	validRequest := &domain.Request{
		ID:        "test-request-1",
		Type:      domain.RequestTypeHealth,
		Timestamp: time.Now(),
		Source:    "test-client",
		Data:      map[string]interface{}{"key": "value"},
		Context:   ctx,
	}
	
	err := handler.ValidateRequest(ctx, validRequest)
	if err != nil {
		t.Errorf("Expected no error for valid request, got: %v", err)
	}
	
	// Test request with empty ID
	invalidRequest := &domain.Request{
		ID:        "", // Invalid empty ID
		Type:      domain.RequestTypeHealth,
		Timestamp: time.Now(),
		Source:    "test-client",
		Data:      map[string]interface{}{"key": "value"},
		Context:   ctx,
	}
	
	err = handler.ValidateRequest(ctx, invalidRequest)
	if err == nil {
		t.Error("Expected error for request with empty ID")
	}
}

func TestRequestHandlerHandleHealthRequestFixed(t *testing.T) {
	handler := createTestRequestHandler()
	ctx := context.Background()
	
	request := &domain.Request{
		ID:        "health-request-1",
		Type:      domain.RequestTypeHealth,
		Timestamp: time.Now(),
		Source:    "test-client",
		Context:   ctx,
	}
	
	response, err := handler.HandleRequest(ctx, request)
	if err != nil {
		t.Errorf("Expected no error for health request, got: %v", err)
	}
	
	if response == nil {
		t.Error("Expected response, got nil")
	}
	
	if response.RequestID != request.ID {
		t.Errorf("Expected response request ID '%s', got '%s'", request.ID, response.RequestID)
	}
}