package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	"github.com/yairfalse/tapio/pkg/metrics"
	"github.com/yairfalse/tapio/pkg/simple"
)

var (
	metricsAddr               string
	updateInterval            time.Duration
	prometheusEnableEBPF      bool
	prometheusUniversalFormat bool
)

var prometheusCmd = &cobra.Command{
	Use:   "prometheus",
	Short: "Start Prometheus metrics exporter",
	Long: `📊 Start a Prometheus metrics server that exports Tapio health and prediction metrics.

The server continuously monitors your cluster using Tapio's intelligence engine
and provides metrics that can be scraped by Prometheus for alerting and dashboards.

Features:
  • OOM prediction metrics with precise timing
  • Pod health status and cluster health scores
  • Zero-configuration auto-discovery
  • Universal data format for enhanced metrics (--universal)
  • eBPF kernel-level monitoring (--enable-ebpf)
  • Correlation engine findings and insights
  • Graceful shutdown handling

The metrics server runs continuously until stopped with Ctrl+C.`,

	Example: `  # Start metrics server on default port
  tapio prometheus

  # Start on custom port with faster updates  
  tapio prometheus --addr :9090 --interval 10s

  # Enhanced monitoring with eBPF (requires root)
  sudo tapio prometheus --enable-ebpf

  # Test the metrics endpoint
  curl http://localhost:8080/metrics | grep tapio

  # View Tapio-specific metrics
  curl -s http://localhost:8080/metrics | grep "^tapio_"`,

	// Validate arguments before running
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate address format
		if err := validatePrometheusAddress(metricsAddr); err != nil {
			return err
		}

		// Validate update interval
		if err := validateUpdateInterval(updateInterval); err != nil {
			return err
		}

		return nil
	},

	RunE: runPrometheus,
}

func init() {
	prometheusCmd.Flags().StringVar(&metricsAddr, "addr", ":8080",
		"Address to listen on for metrics HTTP server (format: :port or host:port)")
	prometheusCmd.Flags().DurationVar(&updateInterval, "interval", 30*time.Second,
		"How often to update metrics by scanning the cluster (minimum: 5s)")
	prometheusCmd.Flags().BoolVar(&prometheusEnableEBPF, "enable-ebpf", false,
		"Enable eBPF monitoring for enhanced metrics (requires root)")
	prometheusCmd.Flags().BoolVar(&prometheusUniversalFormat, "universal", true,
		"Use universal data format for enhanced metrics")
}

// validatePrometheusAddress validates the Prometheus server address format
func validatePrometheusAddress(addr string) error {
	if addr == "" {
		return NewCLIError(
			"address validation",
			"Address cannot be empty",
			"Provide a valid address like ':8080' or 'localhost:9090'",
		).WithExamples(
			"tapio prometheus --addr :8080",
			"tapio prometheus --addr localhost:9090",
		)
	}

	// Basic validation - should start with : or contain :
	if !strings.Contains(addr, ":") {
		return NewCLIError(
			"address validation",
			"Invalid address format",
			"Address must include a port (e.g., ':8080' or 'host:port')",
		).WithExamples(
			"tapio prometheus --addr :8080",
			"tapio prometheus --addr 0.0.0.0:9090",
		)
	}

	return nil
}

// validateUpdateInterval validates the metrics update interval
func validateUpdateInterval(interval time.Duration) error {
	if interval < 5*time.Second {
		return NewCLIError(
			"interval validation",
			"Update interval too short",
			"Minimum interval is 5 seconds to avoid overwhelming the cluster",
		).WithExamples(
			"tapio prometheus --interval 10s",
			"tapio prometheus --interval 1m",
		)
	}

	if interval > 10*time.Minute {
		return NewCLIError(
			"interval validation",
			"Update interval very long",
			"Consider a shorter interval for more responsive monitoring",
		).WithExamples(
			"tapio prometheus --interval 30s",
			"tapio prometheus --interval 2m",
		)
	}

	return nil
}

func runPrometheus(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup progress tracking
	steps := []string{
		"Initializing Prometheus exporter",
		"Connecting to Kubernetes",
		"Starting metrics collection",
		"Starting HTTP server",
	}

	if prometheusEnableEBPF {
		steps = append(steps, "Starting eBPF monitoring")
	}

	progress := NewStepProgress(steps).WithVerbose(verbose)
	progress.Start()

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

	progress.NextStep() // Move to "Connecting to Kubernetes"

	// Create checker with eBPF config
	checker, err := simple.NewCheckerWithConfig(ebpfConfig)
	if err != nil {
		progress.Error(err)
		return ErrKubernetesConnection(err)
	}

	progress.NextStep() // Move to "Starting metrics collection"

	// Try to start eBPF monitoring if enabled
	if prometheusEnableEBPF {
		progress.NextStep() // Move to "Starting eBPF monitoring"

		err = checker.StartEBPFMonitoring(ctx)
		if err != nil {
			progress.Warning(fmt.Sprintf("eBPF monitoring not available: %v", err))
			fmt.Println("ℹ️  Continuing with Kubernetes API metrics only")
		} else {
			fmt.Println("✨ eBPF monitoring enabled for enhanced metrics")
			defer func() {
				if err := checker.StopEBPFMonitoring(); err != nil {
					fmt.Printf("⚠️  Failed to stop eBPF monitoring: %v\n", err)
				}
			}()
		}
	} else {
		if verbose {
			fmt.Println("ℹ️  Using Kubernetes API metrics only")
		}
	}

	// Create Prometheus exporter
	exporter := metrics.NewPrometheusExporter(checker, checker.GetEBPFMonitor())
	if exporter == nil {
		progress.Error(fmt.Errorf("failed to create Prometheus exporter"))
		return NewCLIError(
			"prometheus initialization",
			"Failed to create Prometheus exporter",
			"Check if all dependencies are available and try again",
		).WithExamples(
			"tapio prometheus --verbose  # Get detailed error information",
		)
	}

	// Start periodic metrics updates in background
	if prometheusUniversalFormat {
		go func() {
			ticker := time.NewTicker(updateInterval)
			defer ticker.Stop()

			// Initial update
			if err := exporter.UpdateMetricsWithUniversal(ctx); err != nil {
				fmt.Printf("⚠️  Initial universal metrics update failed: %v\n", err)
			} else {
				if verbose {
					fmt.Println("✅ Universal metrics initialized")
				}
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := exporter.UpdateMetricsWithUniversal(ctx); err != nil && verbose {
						fmt.Printf("⚠️  Universal metrics update failed: %v\n", err)
					}
				}
			}
		}()
	} else {
		go exporter.StartPeriodicUpdates(ctx, updateInterval)
	}

	progress.NextStep() // Move to "Starting HTTP server"

	// Start metrics server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := exporter.StartMetricsServer(metricsAddr); err != nil {
			serverErr <- NewCLIError(
				"server startup",
				fmt.Sprintf("Failed to start metrics server on %s", metricsAddr),
				"Check if the port is available and you have permission to bind to it",
			).WithExamples(
				"tapio prometheus --addr :8081  # Try different port",
				"sudo tapio prometheus  # If binding to port <1024",
				"lsof -i :8080  # Check what's using the port",
			)
		}
	}()

	// Complete progress tracking
	progress.Finish("Prometheus exporter ready")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("\n🚀 Tapio Prometheus exporter is running!")
	fmt.Printf("📊 Metrics available at: http://%s/metrics\n", metricsAddr)
	fmt.Printf("🔄 Update interval: %v\n", updateInterval)
	fmt.Println("💡 Use Ctrl+C to stop")

	select {
	case err := <-serverErr:
		return err
	case sig := <-sigChan:
		fmt.Printf("\n📡 Received signal %v, shutting down gracefully...\n", sig)
		cancel()

		// Give server a moment to finish current requests
		time.Sleep(1 * time.Second)
		fmt.Println("✅ Tapio Prometheus exporter stopped")
		return nil
	}
}
