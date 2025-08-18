package loader

import (
	"context"
	"fmt"
	"time"

	neo4jint "github.com/yairfalse/tapio/pkg/integrations/neo4j"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// startHealthMonitor starts the health monitoring goroutine
func (l *Loader) startHealthMonitor(ctx context.Context) {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.runHealthMonitor(ctx)
	}()
}

// runHealthMonitor periodically monitors and reports health status
func (l *Loader) runHealthMonitor(ctx context.Context) {
	ctx, span := l.tracer.Start(ctx, "loader.health_monitor")
	defer span.End()

	l.logger.Info("Starting health monitor",
		zap.Duration("check_interval", l.config.HealthCheckInterval))

	ticker := time.NewTicker(l.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.logger.Info("Health monitor context cancelled")
			return

		case <-ticker.C:
			if l.isShutdown.Load() {
				return
			}

			l.performHealthCheck(ctx)
		}
	}
}

// performHealthCheck performs a comprehensive health check
func (l *Loader) performHealthCheck(ctx context.Context) {
	ctx, span := l.tracer.Start(ctx, "loader.health_check")
	defer span.End()

	healthStatus := l.checkComponentHealth(ctx)

	// Update metrics with health information
	l.updateMetrics(func(m *LoaderMetrics) {
		m.HealthStatus = healthStatus.Status
		m.BacklogSize = l.getBacklogSize()
		m.ActiveWorkers = l.getActiveWorkerCount()

		// Calculate performance metrics
		l.calculatePerformanceMetrics(m)
	})

	// Log health status
	l.logHealthStatus(healthStatus)

	// Set span attributes for monitoring
	span.SetAttributes(
		attribute.String("health.status", healthStatus.Status),
		attribute.Bool("health.nats_connected", healthStatus.NATSConnected),
		attribute.Bool("health.neo4j_connected", healthStatus.Neo4jConnected),
		attribute.Int("health.backlog_size", healthStatus.Metrics.BacklogSize),
		attribute.Int("health.active_workers", healthStatus.Metrics.ActiveWorkers),
		attribute.Float64("health.error_rate", healthStatus.Metrics.ErrorRate),
		attribute.Float64("health.throughput", healthStatus.Metrics.ThroughputPerSecond),
	)
}

// checkComponentHealth checks the health of all components
func (l *Loader) checkComponentHealth(ctx context.Context) HealthStatus {
	status := HealthStatus{
		LastCheck:      time.Now(),
		NATSConnected:  l.checkNATSHealth(),
		Neo4jConnected: l.checkNeo4jHealth(ctx),
		Metrics:        l.GetMetrics(),
		Details:        make(map[string]string),
		Errors:         make([]string, 0),
		Warnings:       make([]string, 0),
	}

	// Determine overall health status
	status.Status = l.determineOverallHealth(&status)

	// Add component-specific details
	l.addHealthDetails(&status)

	return status
}

// checkNATSHealth checks NATS connection health
func (l *Loader) checkNATSHealth() bool {
	if l.nc == nil {
		return false
	}

	return l.nc.IsConnected()
}

// checkNeo4jHealth checks Neo4j connection health
func (l *Loader) checkNeo4jHealth(ctx context.Context) bool {
	if l.neo4jClient == nil {
		return false
	}

	// Create a quick health check context with timeout
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Try a simple query to verify connectivity using typed transaction
	err := l.neo4jClient.ExecuteTypedWrite(healthCtx, func(ctx context.Context, tx *neo4jint.TypedTransaction) error {
		// No parameters needed for simple health check
		_, err := tx.Run(ctx, "RETURN 1", nil)
		return err
	})

	return err == nil
}

// determineOverallHealth determines the overall health status
func (l *Loader) determineOverallHealth(status *HealthStatus) string {
	// Check for critical failures
	if !status.NATSConnected {
		status.Errors = append(status.Errors, "NATS connection lost")
		return "unhealthy"
	}

	if !status.Neo4jConnected {
		status.Errors = append(status.Errors, "Neo4j connection lost")
		return "unhealthy"
	}

	// Check for performance issues
	metrics := status.Metrics

	// High error rate indicates degraded performance
	if metrics.ErrorRate > 0.1 { // More than 10% error rate
		status.Warnings = append(status.Warnings,
			"High error rate: "+formatPercent(metrics.ErrorRate))
		return "degraded"
	}

	// Large backlog indicates processing issues
	if metrics.BacklogSize > l.config.BatchSize*5 { // More than 5 batches worth
		status.Warnings = append(status.Warnings,
			"Large processing backlog: "+formatInt(metrics.BacklogSize))
		return "degraded"
	}

	// Check if we're processing events recently
	timeSinceLastProcessed := time.Since(metrics.LastProcessedTime)
	if timeSinceLastProcessed > 5*time.Minute && metrics.EventsReceived > 0 {
		status.Warnings = append(status.Warnings,
			"No recent processing activity: "+formatDuration(timeSinceLastProcessed))
		return "degraded"
	}

	// Low throughput might indicate issues
	if metrics.ThroughputPerSecond < 1.0 && metrics.EventsReceived > 100 {
		status.Warnings = append(status.Warnings,
			"Low throughput: "+formatFloat(metrics.ThroughputPerSecond)+" events/sec")
		return "degraded"
	}

	return "healthy"
}

// addHealthDetails adds detailed health information
func (l *Loader) addHealthDetails(status *HealthStatus) {
	metrics := status.Metrics

	// Add performance details
	status.Details["throughput"] = formatFloat(metrics.ThroughputPerSecond) + " events/sec"
	status.Details["processing_latency"] = formatFloat(metrics.ProcessingLatency) + " ms"
	status.Details["storage_latency"] = formatFloat(metrics.StorageLatency) + " ms"
	status.Details["backlog_size"] = formatInt(metrics.BacklogSize)
	status.Details["active_workers"] = formatInt(metrics.ActiveWorkers)
	status.Details["error_rate"] = formatPercent(metrics.ErrorRate)

	// Add processing statistics
	status.Details["events_received"] = formatInt64(metrics.EventsReceived)
	status.Details["events_processed"] = formatInt64(metrics.EventsProcessed)
	status.Details["events_failed"] = formatInt64(metrics.EventsFailed)
	status.Details["batches_processed"] = formatInt64(metrics.BatchesProcessed)
	status.Details["batches_failed"] = formatInt64(metrics.BatchesFailed)

	// Add timing information
	status.Details["last_processed"] = metrics.LastProcessedTime.Format(time.RFC3339)
	status.Details["uptime"] = time.Since(metrics.LastProcessedTime).String()

	// Add configuration details
	status.Details["batch_size"] = formatInt(l.config.BatchSize)
	status.Details["max_concurrency"] = formatInt(l.config.MaxConcurrency)
	status.Details["batch_timeout"] = l.config.BatchTimeout.String()
}

// logHealthStatus logs the current health status
func (l *Loader) logHealthStatus(status HealthStatus) {
	fields := []zap.Field{
		zap.String("status", status.Status),
		zap.Bool("nats_connected", status.NATSConnected),
		zap.Bool("neo4j_connected", status.Neo4jConnected),
		zap.Int64("events_received", status.Metrics.EventsReceived),
		zap.Int64("events_processed", status.Metrics.EventsProcessed),
		zap.Int64("events_failed", status.Metrics.EventsFailed),
		zap.Float64("error_rate", status.Metrics.ErrorRate),
		zap.Float64("throughput_per_sec", status.Metrics.ThroughputPerSecond),
		zap.Int("backlog_size", status.Metrics.BacklogSize),
		zap.Int("active_workers", status.Metrics.ActiveWorkers),
		zap.Float64("processing_latency_ms", status.Metrics.ProcessingLatency),
		zap.Float64("storage_latency_ms", status.Metrics.StorageLatency),
	}

	// Add errors and warnings if present
	if len(status.Errors) > 0 {
		fields = append(fields, zap.Strings("errors", status.Errors))
	}
	if len(status.Warnings) > 0 {
		fields = append(fields, zap.Strings("warnings", status.Warnings))
	}

	switch status.Status {
	case "healthy":
		l.logger.Info("Health check passed", fields...)
	case "degraded":
		l.logger.Warn("Health check degraded", fields...)
	case "unhealthy":
		l.logger.Error("Health check failed", fields...)
	default:
		l.logger.Info("Health check completed", fields...)
	}
}

// getBacklogSize returns the current backlog size
func (l *Loader) getBacklogSize() int {
	return len(l.batchChannel)
}

// getActiveWorkerCount returns the current number of active workers
func (l *Loader) getActiveWorkerCount() int {
	return len(l.workerPool)
}

// calculatePerformanceMetrics calculates performance metrics
func (l *Loader) calculatePerformanceMetrics(metrics *LoaderMetrics) {
	now := time.Now()

	// Update processing latency (moving average)
	// This would be updated by actual processing operations

	// Calculate throughput
	if metrics.EventsReceived > 0 {
		duration := now.Sub(metrics.LastProcessedTime).Seconds()
		if duration > 0 {
			metrics.ThroughputPerSecond = float64(metrics.EventsProcessed) / duration
		}
	}

	// Calculate error rate
	if metrics.EventsReceived > 0 {
		metrics.ErrorRate = float64(metrics.EventsFailed) / float64(metrics.EventsReceived)
	}
}

// Helper formatting functions
func formatInt(val int) string {
	return fmt.Sprintf("%d", val)
}

func formatInt64(val int64) string {
	return fmt.Sprintf("%d", val)
}

func formatFloat(val float64) string {
	return fmt.Sprintf("%.2f", val)
}

func formatPercent(val float64) string {
	return fmt.Sprintf("%.2f%%", val*100)
}

func formatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}
