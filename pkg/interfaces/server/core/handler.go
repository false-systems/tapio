package core

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// RequestHandler implements the request handling logic
type RequestHandler struct {
	serverService    domain.ServerService
	validator        domain.Validator
	serializer       domain.Serializer
	metricsCollector domain.MetricsCollector
	eventPublisher   domain.EventPublisher
	logger           domain.Logger
}

// NewRequestHandler creates a new request handler
func NewRequestHandler(
	serverService domain.ServerService,
	validator domain.Validator,
	serializer domain.Serializer,
	metricsCollector domain.MetricsCollector,
	eventPublisher domain.EventPublisher,
	logger domain.Logger,
) *RequestHandler {
	return &RequestHandler{
		serverService:    serverService,
		validator:        validator,
		serializer:       serializer,
		metricsCollector: metricsCollector,
		eventPublisher:   eventPublisher,
		logger:           logger,
	}
}

// HandleRequest handles a server request
func (h *RequestHandler) HandleRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	startTime := time.Now()

	// Validate request
	if err := h.ValidateRequest(ctx, request); err != nil {
		h.recordRequestError(ctx, request, err)
		return h.createErrorResponse(request, err), nil
	}

	// Log request
	if h.logger != nil {
		h.logger.WithRequest(request).Info(ctx, "processing request")
	}

	// Route and process request
	response, err := h.processRequest(ctx, request)
	if err != nil {
		h.recordRequestError(ctx, request, err)
		return h.createErrorResponse(request, err), nil
	}

	// Record metrics
	processingTime := time.Since(startTime)
	h.recordRequestSuccess(ctx, request, response, processingTime)

	// Log response
	if h.logger != nil {
		h.logger.WithRequest(request).WithResponse(response).Info(ctx, "request processed successfully")
	}

	return response, nil
}

// ValidateRequest validates a request
func (h *RequestHandler) ValidateRequest(ctx context.Context, request *domain.Request) error {
	if request == nil {
		return domain.ErrInvalidRequest("request cannot be nil")
	}

	if request.ID == "" {
		return domain.ErrInvalidRequest("request ID cannot be empty")
	}

	if request.Type == "" {
		return domain.ErrInvalidRequest("request type cannot be empty")
	}

	if request.Timestamp.IsZero() {
		return domain.ErrInvalidRequest("request timestamp cannot be zero")
	}

	if request.Context == nil {
		return domain.ErrInvalidRequest("request context cannot be nil")
	}

	// Use validator if available
	if h.validator != nil {
		return h.validator.ValidateRequest(ctx, request)
	}

	return nil
}

// RouteRequest routes a request to the appropriate handler
func (h *RequestHandler) RouteRequest(ctx context.Context, request *domain.Request) (string, error) {
	switch request.Type {
	case domain.RequestTypeHealth:
		return "health", nil
	case domain.RequestTypeMetrics:
		return "metrics", nil
	case domain.RequestTypeEvent:
		return "event", nil
	case domain.RequestTypeStream:
		return "stream", nil
	case domain.RequestTypeQuery:
		return "query", nil
	case domain.RequestTypeCommand:
		return "command", nil
	default:
		return "", domain.ErrInvalidRequest(fmt.Sprintf("unsupported request type: %s", request.Type))
	}
}

// processRequest processes a request based on its type
func (h *RequestHandler) processRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	switch request.Type {
	case domain.RequestTypeHealth:
		return h.handleHealthRequest(ctx, request)
	case domain.RequestTypeMetrics:
		return h.handleMetricsRequest(ctx, request)
	case domain.RequestTypeEvent:
		return h.handleEventRequest(ctx, request)
	case domain.RequestTypeStream:
		return h.handleStreamRequest(ctx, request)
	case domain.RequestTypeQuery:
		return h.handleQueryRequest(ctx, request)
	case domain.RequestTypeCommand:
		return h.handleCommandRequest(ctx, request)
	default:
		return nil, domain.ErrInvalidRequest(fmt.Sprintf("unsupported request type: %s", request.Type))
	}
}

// handleHealthRequest handles health check requests
func (h *RequestHandler) handleHealthRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	health, err := h.serverService.GetHealth(ctx)
	if err != nil {
		return nil, err
	}

	response := &domain.Response{
		ID:        h.generateResponseID(),
		RequestID: request.ID,
		Type:      domain.ResponseTypeData,
		Status:    domain.ResponseStatusOK,
		Timestamp: time.Now(),
		Data:      health,
	}

	return response, nil
}

// handleMetricsRequest handles metrics requests
func (h *RequestHandler) handleMetricsRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	metrics, err := h.serverService.GetMetrics(ctx)
	if err != nil {
		return nil, err
	}

	response := &domain.Response{
		ID:        h.generateResponseID(),
		RequestID: request.ID,
		Type:      domain.ResponseTypeData,
		Status:    domain.ResponseStatusOK,
		Timestamp: time.Now(),
		Data:      metrics,
	}

	return response, nil
}

// handleEventRequest handles event requests
func (h *RequestHandler) handleEventRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	event, ok := request.Data.(*domain.Event)
	if !ok {
		return nil, domain.ErrInvalidRequest("request data must be an Event")
	}

	if err := h.serverService.PublishEvent(ctx, event); err != nil {
		return nil, err
	}

	response := &domain.Response{
		ID:        h.generateResponseID(),
		RequestID: request.ID,
		Type:      domain.ResponseTypeSuccess,
		Status:    domain.ResponseStatusOK,
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"event_id": event.ID},
	}

	return response, nil
}

// handleStreamRequest handles stream requests
func (h *RequestHandler) handleStreamRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	// Stream requests need special handling through StreamHandler
	return nil, domain.ErrNotImplemented("stream handling")
}

// handleQueryRequest handles query requests
func (h *RequestHandler) handleQueryRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	// Query requests need to be routed to appropriate query handlers
	return nil, domain.ErrNotImplemented("query handling")
}

// handleCommandRequest handles command requests
func (h *RequestHandler) handleCommandRequest(ctx context.Context, request *domain.Request) (*domain.Response, error) {
	// Command requests need to be routed to appropriate command handlers
	return nil, domain.ErrNotImplemented("command handling")
}

// createErrorResponse creates an error response
func (h *RequestHandler) createErrorResponse(request *domain.Request, err error) *domain.Response {
	return &domain.Response{
		ID:        h.generateResponseID(),
		RequestID: request.ID,
		Type:      domain.ResponseTypeError,
		Status:    domain.ResponseStatusError,
		Timestamp: time.Now(),
		Error:     err,
		Data:      map[string]interface{}{"error": err.Error()},
	}
}

// recordRequestSuccess records successful request metrics
func (h *RequestHandler) recordRequestSuccess(ctx context.Context, request *domain.Request, response *domain.Response, duration time.Duration) {
	if h.metricsCollector != nil {
		h.metricsCollector.RecordRequest(ctx, request)
		h.metricsCollector.RecordResponse(ctx, response)
	}

	if h.eventPublisher != nil {
		event := &domain.Event{
			ID:        h.generateEventID(),
			Type:      domain.EventTypeRequest,
			Severity:  domain.SeverityInfo,
			Source:    "request_handler",
			Message:   fmt.Sprintf("request processed successfully: %s", request.Type),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"request_id":      request.ID,
				"request_type":    request.Type,
				"response_id":     response.ID,
				"processing_time": duration.String(),
			},
			Context: ctx,
		}

		h.eventPublisher.PublishEvent(ctx, event)
	}
}

// recordRequestError records request error metrics
func (h *RequestHandler) recordRequestError(ctx context.Context, request *domain.Request, err error) {
	if h.metricsCollector != nil {
		h.metricsCollector.RecordError(ctx, err)
	}

	if h.eventPublisher != nil {
		event := &domain.Event{
			ID:        h.generateEventID(),
			Type:      domain.EventTypeError,
			Severity:  domain.SeverityError,
			Source:    "request_handler",
			Message:   fmt.Sprintf("request processing failed: %s", err.Error()),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"request_id":   request.ID,
				"request_type": request.Type,
				"error":        err.Error(),
			},
			Context: ctx,
		}

		h.eventPublisher.PublishEvent(ctx, event)
	}

	if h.logger != nil {
		h.logger.WithRequest(request).WithError(err).Error(ctx, "request processing failed")
	}
}

// generateResponseID generates a unique response ID
func (h *RequestHandler) generateResponseID() string {
	return fmt.Sprintf("response-%d", time.Now().UnixNano())
}

// generateEventID generates a unique event ID
func (h *RequestHandler) generateEventID() string {
	return fmt.Sprintf("event-%d", time.Now().UnixNano())
}

// ResponseHandler implements the response handling logic
type ResponseHandler struct {
	serializer       domain.Serializer
	validator        domain.Validator
	metricsCollector domain.MetricsCollector
	eventPublisher   domain.EventPublisher
	logger           domain.Logger
}

// NewResponseHandler creates a new response handler
func NewResponseHandler(
	serializer domain.Serializer,
	validator domain.Validator,
	metricsCollector domain.MetricsCollector,
	eventPublisher domain.EventPublisher,
	logger domain.Logger,
) *ResponseHandler {
	return &ResponseHandler{
		serializer:       serializer,
		validator:        validator,
		metricsCollector: metricsCollector,
		eventPublisher:   eventPublisher,
		logger:           logger,
	}
}

// HandleResponse handles a server response
func (h *ResponseHandler) HandleResponse(ctx context.Context, response *domain.Response) error {
	// Validate response
	if err := h.ValidateResponse(ctx, response); err != nil {
		h.recordResponseError(ctx, response, err)
		return err
	}

	// Log response
	if h.logger != nil {
		h.logger.WithResponse(response).Info(ctx, "handling response")
	}

	// Record metrics
	h.recordResponseSuccess(ctx, response)

	return nil
}

// FormatResponse formats a response for transmission
func (h *ResponseHandler) FormatResponse(ctx context.Context, response *domain.Response) ([]byte, error) {
	if h.serializer == nil {
		return nil, domain.NewServerError(domain.ErrorCodeSerializationFailed, "serializer not configured")
	}

	return h.serializer.Serialize(ctx, response)
}

// ValidateResponse validates a response
func (h *ResponseHandler) ValidateResponse(ctx context.Context, response *domain.Response) error {
	if response == nil {
		return domain.ErrInvalidRequest("response cannot be nil")
	}

	if response.ID == "" {
		return domain.ErrInvalidRequest("response ID cannot be empty")
	}

	if response.RequestID == "" {
		return domain.ErrInvalidRequest("response request ID cannot be empty")
	}

	if response.Type == "" {
		return domain.ErrInvalidRequest("response type cannot be empty")
	}

	if response.Timestamp.IsZero() {
		return domain.ErrInvalidRequest("response timestamp cannot be zero")
	}

	// Use validator if available
	if h.validator != nil {
		return h.validator.ValidateResponse(ctx, response)
	}

	return nil
}

// recordResponseSuccess records successful response metrics
func (h *ResponseHandler) recordResponseSuccess(ctx context.Context, response *domain.Response) {
	if h.metricsCollector != nil {
		h.metricsCollector.RecordResponse(ctx, response)
	}

	if h.eventPublisher != nil {
		event := &domain.Event{
			ID:        h.generateEventID(),
			Type:      domain.EventTypeResponse,
			Severity:  domain.SeverityInfo,
			Source:    "response_handler",
			Message:   fmt.Sprintf("response handled successfully: %s", response.Type),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"response_id":   response.ID,
				"request_id":    response.RequestID,
				"response_type": response.Type,
				"status":        response.Status,
			},
			Context: ctx,
		}

		h.eventPublisher.PublishEvent(ctx, event)
	}
}

// recordResponseError records response error metrics
func (h *ResponseHandler) recordResponseError(ctx context.Context, response *domain.Response, err error) {
	if h.metricsCollector != nil {
		h.metricsCollector.RecordError(ctx, err)
	}

	if h.eventPublisher != nil {
		event := &domain.Event{
			ID:        h.generateEventID(),
			Type:      domain.EventTypeError,
			Severity:  domain.SeverityError,
			Source:    "response_handler",
			Message:   fmt.Sprintf("response handling failed: %s", err.Error()),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"response_id": response.ID,
				"request_id":  response.RequestID,
				"error":       err.Error(),
			},
			Context: ctx,
		}

		h.eventPublisher.PublishEvent(ctx, event)
	}

	if h.logger != nil {
		h.logger.WithResponse(response).WithError(err).Error(ctx, "response handling failed")
	}
}

// generateEventID generates a unique event ID
func (h *ResponseHandler) generateEventID() string {
	return fmt.Sprintf("event-%d", time.Now().UnixNano())
}
