package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/metrics"
	"github.com/yairfalse/tapio/pkg/simple"
)

var (
	metricsAddr          string
	updateInterval       time.Duration
	prometheusEnableEBPF bool
	prometheusUniversalFormat   bool
)

var prometheusCmd = &cobra.Command{
	Use:   "prometheus",
	Short: "Start Prometheus metrics exporter",
	Long: `Start a Prometheus metrics server that exports Tapio health and prediction metrics.

The server continuously monitors your cluster using Tapio's intelligence engine
and provides metrics that can be scraped by Prometheus for alerting and dashboards.

Features:
  • OOM prediction metrics with precise timing
  • Pod health status and cluster health scores
  • Zero-configuration auto-discovery
  • Universal data format for enhanced metrics (--universal)
  • eBPF kernel-level monitoring (--enable-ebpf)
  • Correlation engine findings`,
	Example: `  # Start metrics server on default port
  tapio prometheus

  # Start on custom port with faster updates  
  tapio prometheus --addr :9090 --interval 10s

  # Test the metrics
  curl http://localhost:8080/metrics | grep tapio`,
	RunE: runPrometheus,
}

func init() {
	prometheusCmd.Flags().StringVar(&metricsAddr, "addr", ":8080",
		"Address to listen on for metrics HTTP server")
	prometheusCmd.Flags().DurationVar(&updateInterval, "interval", 30*time.Second,
		"How often to update metrics by scanning the cluster")
	prometheusCmd.Flags().BoolVar(&prometheusEnableEBPF, "enable-ebpf", false,
		"Enable eBPF monitoring for enhanced metrics (requires root)")
	prometheusCmd.Flags().BoolVar(&prometheusUniversalFormat, "universal", true,
		"Use universal data format for enhanced metrics")
}

func runPrometheus(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("🌲 Starting Tapio Prometheus Exporter...")
	if prometheusUniversalFormat {
		fmt.Println("✨ Using universal data format for enhanced metrics")
	}

	// Create eBPF config if enabled
	var ebpfConfig *ebpf.Config
	if prometheusEnableEBPF {
		ebpfConfig = &ebpf.Config{
			Enabled:         true,
			EventBufferSize: 1000,
			RetentionPeriod: "5m",
		}
	}

	// Create checker with eBPF config
	checker, err := simple.NewCheckerWithConfig(ebpfConfig)
	if err != nil {
		return fmt.Errorf("failed to create checker: %w", err)
	}

	// Try to start eBPF monitoring if enabled
	if prometheusEnableEBPF {
		err = checker.StartEBPFMonitoring(ctx)
		if err != nil {
			fmt.Printf("[WARN] eBPF monitoring not available: %v\n", err)
			fmt.Println("[INFO] Continuing with Kubernetes API metrics only")
		} else {
			fmt.Println("[OK] eBPF monitoring enabled for enhanced metrics")
			defer func() {
				if err := checker.StopEBPFMonitoring(); err != nil {
					fmt.Printf("[ERROR] Failed to stop eBPF monitoring: %v\n", err)
				}
			}()
		}
	} else {
		fmt.Println("[INFO] Using Kubernetes API metrics only")
	}

	// Create Prometheus exporter
	exporter := metrics.NewPrometheusExporter(checker, checker.GetEBPFMonitor())

	// Start periodic metrics updates in background
	if prometheusUniversalFormat {
		go func() {
			ticker := time.NewTicker(updateInterval)
			defer ticker.Stop()

			// Initial update
			if err := exporter.UpdateMetricsWithUniversal(ctx); err != nil {
				fmt.Printf("Warning: Initial universal metrics update failed: %v\n", err)
			} else {
				fmt.Println("[OK] Universal metrics initialized")
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := exporter.UpdateMetricsWithUniversal(ctx); err != nil {
						fmt.Printf("Warning: Universal metrics update failed: %v\n", err)
					}
				}
			}
		}()
	} else {
		go exporter.StartPeriodicUpdates(ctx, updateInterval)
	}

	// Start metrics server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- exporter.StartMetricsServer(metricsAddr)
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("🚀 Tapio Prometheus exporter is running!")
	fmt.Printf("📊 Metrics available at: http://%s/metrics\n", metricsAddr)
	fmt.Println("💡 Use Ctrl+C to stop")

	select {
	case err := <-serverErr:
		return fmt.Errorf("metrics server failed: %w", err)
	case sig := <-sigChan:
		fmt.Printf("\n📡 Received signal %v, shutting down gracefully...\n", sig)
		cancel()

		// Give server a moment to finish current requests
		time.Sleep(1 * time.Second)
		fmt.Println("✅ Tapio Prometheus exporter stopped")
		return nil
	}
}
