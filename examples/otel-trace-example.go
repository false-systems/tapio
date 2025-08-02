package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/integrations/nats"
	"github.com/yairfalse/tapio/pkg/integrations/transformer"
)

// Example: How OTEL traces flow through Tapio
func main() {
	// 1. NATS Publisher
	publisher, err := nats.NewEventPublisher(&nats.PublisherConfig{
		URL:        "nats://localhost:4222",
		StreamName: "TAPIO_EVENTS",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer publisher.Close()

	// 2. Event Transformer
	transformer := transformer.NewEventTransformer()

	// 3. Simulate a traced request flow
	traceID := "trace-abc-123-def"

	// API Gateway receives request
	gatewayEvent := collectors.RawEvent{
		Type:      "kubeapi",
		Timestamp: time.Now(),
		Data:      []byte(`{"type": "Normal", "reason": "RequestReceived", "object": {"kind": "Service", "name": "api-gateway"}}`),
		Metadata: map[string]string{
			"trace_id":  traceID,
			"span_id":   "span-gw-1",
			"namespace": "ingress",
			"component": "gateway",
		},
	}

	// Auth service processes request
	authEvent := collectors.RawEvent{
		Type:      "systemd",
		Timestamp: time.Now().Add(1 * time.Millisecond),
		Data:      []byte(`{"message": "Auth check passed for user-123", "level": "info"}`),
		Metadata: map[string]string{
			"trace_id":       traceID,
			"span_id":        "span-auth-2",
			"parent_span_id": "span-gw-1",
			"service":        "auth-service",
		},
	}

	// API server handles request
	apiEvent := collectors.RawEvent{
		Type:      "kubeapi",
		Timestamp: time.Now().Add(2 * time.Millisecond),
		Data:      []byte(`{"type": "Normal", "reason": "Created", "object": {"kind": "Pod", "name": "app-xyz", "namespace": "production"}}`),
		Metadata: map[string]string{
			"trace_id":       traceID,
			"span_id":        "span-api-3",
			"parent_span_id": "span-gw-1",
			"namespace":      "production",
		},
	}

	// etcd writes the data
	etcdEvent := collectors.RawEvent{
		Type:      "etcd",
		Timestamp: time.Now().Add(3 * time.Millisecond),
		Data:      []byte(`{"operation": "put", "key": "/registry/pods/production/app-xyz", "value_size": 2048}`),
		Metadata: map[string]string{
			"trace_id":       traceID,
			"span_id":        "span-etcd-4",
			"parent_span_id": "span-api-3",
			"cluster":        "prod-cluster",
		},
	}

	// ERROR: Webhook fails!
	webhookEvent := collectors.RawEvent{
		Type:      "systemd",
		Timestamp: time.Now().Add(4 * time.Millisecond),
		Data:      []byte(`{"message": "Webhook validation failed: insufficient resources", "level": "error"}`),
		Metadata: map[string]string{
			"trace_id":       traceID,
			"span_id":        "span-webhook-5",
			"parent_span_id": "span-api-3",
			"service":        "admission-webhook",
		},
	}

	// Process and publish all events
	events := []collectors.RawEvent{gatewayEvent, authEvent, apiEvent, etcdEvent, webhookEvent}

	fmt.Printf("=== Trace Flow for Request %s ===\n\n", traceID)

	for _, rawEvent := range events {
		// Transform to unified event
		unified, err := transformer.Transform(context.Background(), rawEvent)
		if err != nil {
			log.Printf("Transform error: %v", err)
			continue
		}

		// Publish raw event
		if err := publisher.PublishRawEvent(context.Background(), rawEvent); err != nil {
			log.Printf("Publish raw error: %v", err)
			continue
		}

		// Publish unified event
		if err := publisher.PublishUnifiedEvent(context.Background(), unified); err != nil {
			log.Printf("Publish unified error: %v", err)
			continue
		}

		// Show the flow
		indent := ""
		if unified.TraceContext.ParentSpanID != "" {
			indent = "  → "
		}

		fmt.Printf("%s[%s] %s: %s\n",
			indent,
			unified.TraceContext.SpanID,
			unified.Source,
			unified.Message,
		)
	}

	fmt.Printf("\n=== Correlation Engine would see: ===\n")
	fmt.Printf("• All events grouped by trace ID: %s\n", traceID)
	fmt.Printf("• Root cause: Webhook validation failure\n")
	fmt.Printf("• Impact: Pod creation blocked\n")
	fmt.Printf("• All events published to: traces.%s\n", traceID)
}
