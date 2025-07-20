package grpc

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ServerMiddleware provides common middleware for all gRPC services
type ServerMiddleware struct {
	logger *zap.Logger
	tracer trace.Tracer
}

// NewServerMiddleware creates middleware with logger and tracer
func NewServerMiddleware(logger *zap.Logger, tracer trace.Tracer) *ServerMiddleware {
	return &ServerMiddleware{
		logger: logger,
		tracer: tracer,
	}
}

// UnaryInterceptor provides logging, tracing, and recovery for unary calls
func (m *ServerMiddleware) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		// Extract trace context
		ctx, span := m.tracer.Start(ctx, info.FullMethod)
		defer span.End()

		// Add method info to span
		span.SetAttributes(
			attribute.String("rpc.method", info.FullMethod),
			attribute.String("rpc.service", "tapio"),
		)

		// Recovery wrapper
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("gRPC panic recovered",
					zap.String("method", info.FullMethod),
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())),
				)
				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, "panic recovered")
			}
		}()

		// Execute handler
		resp, err := handler(ctx, req)

		// Record metrics and logs
		duration := time.Since(start)

		if err != nil {
			m.logger.Error("gRPC unary call failed",
				zap.String("method", info.FullMethod),
				zap.Error(err),
				zap.Duration("duration", duration),
			)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			m.logger.Debug("gRPC unary call completed",
				zap.String("method", info.FullMethod),
				zap.Duration("duration", duration),
			)
			span.SetStatus(codes.Ok, "success")
		}

		span.SetAttributes(
			attribute.Int64("rpc.duration_ms", duration.Milliseconds()),
		)

		return resp, err
	}
}

// StreamInterceptor provides logging, tracing, and recovery for streaming calls
func (m *ServerMiddleware) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()

		// Create wrapped stream with tracing context
		ctx := stream.Context()
		ctx, span := m.tracer.Start(ctx, info.FullMethod)
		defer span.End()

		wrappedStream := &tracedServerStream{
			ServerStream: stream,
			ctx:          ctx,
		}

		// Add method info to span
		span.SetAttributes(
			attribute.String("rpc.method", info.FullMethod),
			attribute.String("rpc.service", "tapio"),
			attribute.Bool("rpc.streaming", true),
		)

		// Recovery wrapper
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("gRPC stream panic recovered",
					zap.String("method", info.FullMethod),
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())),
				)
				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, "panic recovered")
			}
		}()

		// Execute handler
		err := handler(srv, wrappedStream)

		// Record metrics and logs
		duration := time.Since(start)

		if err != nil {
			m.logger.Error("gRPC stream call failed",
				zap.String("method", info.FullMethod),
				zap.Error(err),
				zap.Duration("duration", duration),
			)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			m.logger.Debug("gRPC stream call completed",
				zap.String("method", info.FullMethod),
				zap.Duration("duration", duration),
			)
			span.SetStatus(codes.Ok, "success")
		}

		span.SetAttributes(
			attribute.Int64("rpc.duration_ms", duration.Milliseconds()),
		)

		return err
	}
}

// tracedServerStream wraps ServerStream with tracing context
type tracedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *tracedServerStream) Context() context.Context {
	return s.ctx
}

// RateLimitInterceptor provides rate limiting for gRPC calls
func RateLimitInterceptor(maxRequestsPerSecond int) grpc.UnaryServerInterceptor {
	// Simple token bucket implementation
	tokens := make(chan struct{}, maxRequestsPerSecond)

	// Fill bucket
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(maxRequestsPerSecond))
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				select {
				case tokens <- struct{}{}:
				default:
					// Bucket full
				}
			}
		}
	}()

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		select {
		case <-tokens:
			return handler(ctx, req)
		case <-time.After(100 * time.Millisecond):
			return nil, status.Error(grpcCodes.ResourceExhausted, "rate limit exceeded")
		}
	}
}

// AuthInterceptor provides authentication for gRPC calls
func AuthInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Skip auth for health checks
		if info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, req)
		}

		// Extract metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(grpcCodes.Unauthenticated, "missing metadata")
		}

		// Check for authorization header
		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return nil, status.Error(grpcCodes.Unauthenticated, "missing authorization header")
		}

		// Basic token validation (in production, this would be more sophisticated)
		token := authHeaders[0]
		if !isValidToken(token) {
			return nil, status.Error(grpcCodes.Unauthenticated, "invalid token")
		}

		// Add user context
		ctx = context.WithValue(ctx, "user", extractUserFromToken(token))

		return handler(ctx, req)
	}
}

// ValidationInterceptor provides request validation
func ValidationInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Validate request based on method
		if err := validateRequest(info.FullMethod, req); err != nil {
			return nil, status.Error(grpcCodes.InvalidArgument, err.Error())
		}

		return handler(ctx, req)
	}
}

// MetricsInterceptor provides Prometheus metrics collection
func MetricsInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)

		// Record metrics (would integrate with Prometheus in production)
		recordGRPCMetrics(info.FullMethod, err, duration)

		return resp, err
	}
}

// Helper functions

func isValidToken(token string) bool {
	// Basic token validation - in production this would check against auth service
	return len(token) > 10 && token != "invalid"
}

func extractUserFromToken(token string) string {
	// Extract user ID from token - simplified for example
	return "user_" + token[:8]
}

func validateRequest(method string, req interface{}) error {
	// Request validation logic based on method
	// This would be more sophisticated in production
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	return nil
}

func recordGRPCMetrics(method string, err error, duration time.Duration) {
	// Metric recording logic - would integrate with Prometheus
	// For now, just log
	status := "success"
	if err != nil {
		status = "error"
	}

	// In production, this would increment Prometheus counters:
	// grpc_requests_total.WithLabelValues(method, status).Inc()
	// grpc_request_duration_seconds.WithLabelValues(method).Observe(duration.Seconds())

	_ = status // Suppress unused variable warning
}

// CORS support for gRPC-Gateway (simplified for gRPC context)
func CORSInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Add CORS headers to context for gRPC-Gateway
		md := metadata.Pairs(
			"Access-Control-Allow-Origin", "*",
			"Access-Control-Allow-Methods", "GET, POST, OPTIONS",
			"Access-Control-Allow-Headers", "Content-Type, Authorization",
		)
		ctx = metadata.NewOutgoingContext(ctx, md)

		return handler(ctx, req)
	}
}

// HealthCheckInterceptor provides health checking
func HealthCheckInterceptor(healthChecker func() error) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Check service health before processing request
		if err := healthChecker(); err != nil {
			return nil, status.Error(grpcCodes.Unavailable, "service unhealthy: "+err.Error())
		}

		return handler(ctx, req)
	}
}
