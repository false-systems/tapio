//go:build linux
// +build linux

package storageio

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// RunPremiumDemo demonstrates the premium storage-io collector features
func RunPremiumDemo() error {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	config := &Config{
		BufferSize:        1000,
		SlowIOThresholdMs: 10,
		MinIOSize:         1024,
		EnableVFSRead:     true,
		EnableVFSWrite:    true,
		EnableVFSFsync:    true,
	}

	collector, err := NewCollector("storage-io-premium", config)
	if err != nil {
		return fmt.Errorf("failed to create collector: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger.Info("🎯 Starting Premium Storage I/O Collector Demo")
	logger.Info("==============================================")

	if err := collector.Start(ctx); err != nil {
		return fmt.Errorf("failed to start collector: %w", err)
	}

	// Statistics tracking
	stats := struct {
		TotalEvents    int
		SlowIOCount    int
		VolumeTypes    map[string]int
		AverageLatency time.Duration
		MaxLatency     time.Duration
		TotalBytes     int64
		BlockedCount   int
	}{
		VolumeTypes: make(map[string]int),
	}

	// Monitor events
	go func() {
		for event := range collector.Events() {
			if event.Type != domain.EventTypeStorageIO {
				continue
			}

			stats.TotalEvents++

			// Extract storage data from EventDataContainer
			storageData, ok := event.EventData.GetStorageIOData()
			if ok && storageData != nil {
				// Track volume types
				stats.VolumeTypes[storageData.VolumeType]++

				// Track performance
				if storageData.Duration > stats.MaxLatency {
					stats.MaxLatency = storageData.Duration
				}
				stats.AverageLatency = (stats.AverageLatency*time.Duration(stats.TotalEvents-1) + storageData.Duration) / time.Duration(stats.TotalEvents)
				stats.TotalBytes += storageData.Size

				if storageData.SlowIO {
					stats.SlowIOCount++
				}

				// Log premium insights
				if storageData.SlowIO {
					logger.Warn("⚠️ Slow I/O Detected",
						zap.String("path", storageData.Path),
						zap.String("operation", storageData.Operation),
						zap.Duration("latency", storageData.Duration),
						zap.String("volume_type", storageData.VolumeType),
						zap.String("device", storageData.Device),
						zap.Float64("latency_ms", storageData.LatencyMS),
					)

					// Show timing breakdown if available
					if storageData.QueueTime > 0 || storageData.BlockTime > 0 {
						logger.Info("📊 Timing Breakdown",
							zap.Duration("queue_time", storageData.QueueTime),
							zap.Duration("block_time", storageData.BlockTime),
							zap.Duration("cpu_time", storageData.CPUTime),
						)
					}
				}

				// Highlight blocked I/O
				if storageData.BlockedIO {
					stats.BlockedCount++
					logger.Info("🔒 Blocked I/O Detected",
						zap.String("path", storageData.Path),
						zap.Duration("block_time", storageData.BlockTime),
					)
				}
			}
		}
	}()

	// Wait for demo duration
	<-ctx.Done()

	// Stop collector
	if err := collector.Stop(); err != nil {
		logger.Error("Failed to stop collector", zap.Error(err))
	}

	// Print premium analytics
	logger.Info("📈 Premium Analytics Summary")
	logger.Info("============================")
	logger.Info("Performance Metrics",
		zap.Int("total_events", stats.TotalEvents),
		zap.Int("slow_io_count", stats.SlowIOCount),
		zap.Duration("average_latency", stats.AverageLatency),
		zap.Duration("max_latency", stats.MaxLatency),
		zap.Int64("total_bytes", stats.TotalBytes),
	)

	logger.Info("Volume Type Distribution")
	for volType, count := range stats.VolumeTypes {
		percentage := float64(count) * 100 / float64(stats.TotalEvents)
		logger.Info(fmt.Sprintf("  %s: %d events (%.1f%%)", volType, count, percentage))
	}

	logger.Info("Collector Health",
		zap.String("status", "healthy"),
		zap.Int("events_captured", stats.TotalEvents),
	)

	logger.Info("✨ Premium Demo Complete - This is what paying customers get!")
	return nil
}
