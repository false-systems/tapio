package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

func TestNewK8sGrapherInstrumentation(t *testing.T) {
	logger := zap.NewNop()

	instr, err := NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)
	require.NotNil(t, instr)

	// Verify all metrics are initialized
	assert.NotNil(t, instr.ServiceInstrumentation)
	assert.NotNil(t, instr.RelationshipsDiscovered)
	assert.NotNil(t, instr.GraphUpdateDuration)
	assert.NotNil(t, instr.K8sWatchEvents)
	assert.NotNil(t, instr.GraphQueryDuration)
	assert.NotNil(t, instr.ActiveRelationships)
	assert.NotNil(t, instr.ServicePodMappings)
	assert.NotNil(t, instr.ConfigMapMounts)
	assert.NotNil(t, instr.SecretReferences)
	assert.NotNil(t, instr.PVCBindings)
	assert.NotNil(t, instr.OwnershipChains)
}

func TestK8sGrapherInstrumentation_Metrics(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	// Test relationships discovered counter
	instr.RelationshipsDiscovered.Add(ctx, 5, metric.WithAttributes(
		attribute.String("type", "service_pod"),
	))

	// Test graph update duration
	startTime := time.Now()
	time.Sleep(10 * time.Millisecond)
	duration := time.Since(startTime).Seconds()
	instr.GraphUpdateDuration.Record(ctx, duration, metric.WithAttributes(
		attribute.String("operation", "full_sync"),
	))

	// Test K8s watch events
	instr.K8sWatchEvents.Add(ctx, 1, metric.WithAttributes(
		attribute.String("resource", "services"),
		attribute.String("event_type", "ADDED"),
	))

	// Test active relationships counter
	instr.ActiveRelationships.Add(ctx, 10)
	instr.ActiveRelationships.Add(ctx, -2) // Remove some

	// Test specific relationship counters
	instr.ServicePodMappings.Add(ctx, 3)
	instr.ConfigMapMounts.Add(ctx, 2)
	instr.SecretReferences.Add(ctx, 1)
	instr.PVCBindings.Add(ctx, 1)
	instr.OwnershipChains.Add(ctx, 5)
}

func TestK8sGrapherInstrumentation_TraceIntegration(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	// Test span creation
	ctx, span := instr.StartSpan(ctx, "discover_relationships")
	assert.NotNil(t, span)

	// Simulate some work
	time.Sleep(5 * time.Millisecond)

	// End span
	start := time.Now().Add(-5 * time.Millisecond)
	instr.EndSpan(span, start, nil, "discover_relationships")
}

func TestK8sGrapherInstrumentation_ErrorHandling(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	// Test error recording
	ctx, span := instr.StartSpan(ctx, "graph_update")

	// Simulate an error
	testErr := assert.AnError
	start := time.Now()
	instr.EndSpan(span, start, testErr, "graph_update")

	// Verify error was recorded (through ErrorsTotal metric)
	// In real implementation, we'd check the metric value
}

func TestK8sGrapherInstrumentation_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	// Test concurrent metric updates
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			instr.K8sWatchEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.Int("goroutine", id),
			))
			instr.ActiveRelationships.Add(ctx, 1)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func BenchmarkK8sGrapherInstrumentation_MetricUpdate(b *testing.B) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewK8sGrapherInstrumentation(logger)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		instr.RelationshipsDiscovered.Add(ctx, 1, metric.WithAttributes(
			attribute.String("type", "test"),
		))
	}
}
