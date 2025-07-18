package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yairfalse/tapio/pkg/collector"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
)

var sniffCmd = &cobra.Command{
	Use:   "sniff",
	Short: "Run lightweight eBPF + K8s correlation engine (Polar Signals style)",
	Long: `Run a MEGA FAST, MEGA SLIM, MEGA SMART correlation engine that:
- Uses only 10-50m CPU and 100-256Mi memory
- Samples at 19Hz with unified eBPF program
- Provides lightning-fast PID→Pod mapping
- Generates actionable kubectl fix commands
- Detects OOM, crash loops, and other issues in real-time`,
	RunE: runSniff,
}

var (
	sniffOutput       string
	sniffSamplingRate float64
	sniffBatchSize    int
	sniffK8sOnly      bool
	sniffEBPFOnly     bool
)

func init() {
	sniffCmd.Flags().StringVarP(&sniffOutput, "output", "o", "text", "Output format: text, json, prometheus")
	sniffCmd.Flags().Float64Var(&sniffSamplingRate, "sampling-rate", 1.0, "Sampling rate (0.0-1.0)")
	sniffCmd.Flags().IntVar(&sniffBatchSize, "batch-size", 100, "Event batch size for correlation")
	sniffCmd.Flags().BoolVar(&sniffK8sOnly, "k8s-only", false, "Only run K8s API collector")
	sniffCmd.Flags().BoolVar(&sniffEBPFOnly, "ebpf-only", false, "Only run eBPF collector")
}

func runSniff(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create Kubernetes client
	client, err := createK8sClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create manager
	config := collector.DefaultManagerConfig()
	config.CorrelationBatchSize = sniffBatchSize
	manager := collector.NewSimpleManager(config)

	// Register collectors based on flags
	if !sniffK8sOnly {
		// Create eBPF monitor
		ebpfConfig := &ebpf.Config{
			Enabled:                 true,
			EnableMemoryMonitoring:  true,
			EnableNetworkMonitoring: true,
			SamplingRate:            0.1,
			BufferSize:              1024,
		}
		ebpfMonitor := ebpf.NewMonitor(ebpfConfig)

		// Create PID translator
		translator := collector.NewSimplePIDTranslator(client)
		if err := translator.Start(ctx); err != nil {
			return fmt.Errorf("failed to start PID translator: %w", err)
		}

		// Create and register eBPF collector
		ebpfCollector := collector.NewEBPFCollector(ebpfMonitor, translator)
		if err := manager.Register(ebpfCollector); err != nil {
			return fmt.Errorf("failed to register eBPF collector: %w", err)
		}

		fmt.Println("✓ eBPF collector registered")
	}

	if !sniffEBPFOnly {
		// Create and register K8s collector
		k8sCollector := collector.NewK8sCollector(client)
		if err := manager.Register(k8sCollector); err != nil {
			return fmt.Errorf("failed to register K8s collector: %w", err)
		}

		fmt.Println("✓ K8s API collector registered")
	}

	// Start manager
	if err := manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	fmt.Println("✓ Correlation engine started")
	fmt.Println("\nMonitoring cluster... Press Ctrl+C to stop\n")

	// Start output handler
	go handleOutput(ctx, manager)

	// Start health reporter
	go reportHealth(ctx, manager)

	// Wait for signal
	<-sigChan
	fmt.Println("\n\nShutting down...")

	// Stop manager
	if err := manager.Stop(); err != nil {
		return fmt.Errorf("failed to stop manager: %w", err)
	}

	return nil
}

func handleOutput(ctx context.Context, manager *collector.SimpleManager) {
	insights := manager.Insights()

	for {
		select {
		case <-ctx.Done():
			return
		case insight, ok := <-insights:
			if !ok {
				return
			}

			switch sniffOutput {
			case "json":
				outputInsightJSON(insight)
			case "prometheus":
				outputPrometheus(insight)
			default:
				outputText(insight)
			}
		}
	}
}

func outputText(insight collector.Insight) {
	// Color codes for severity
	severityColor := map[collector.Severity]string{
		collector.SeverityCritical: "\033[31m", // Red
		collector.SeverityHigh:     "\033[33m", // Yellow
		collector.SeverityMedium:   "\033[36m", // Cyan
		collector.SeverityLow:      "\033[32m", // Green
	}
	reset := "\033[0m"

	fmt.Printf("\n%s━━━ %s %s━━━%s\n",
		severityColor[insight.Severity],
		insight.Severity,
		severityColor[insight.Severity],
		reset)

	fmt.Printf("🔍 %s%s%s\n", severityColor[insight.Severity], insight.Title, reset)
	fmt.Printf("   %s\n\n", insight.Description)

	// Show affected resources
	if len(insight.Resources) > 0 {
		fmt.Println("📦 Affected Resources:")
		for _, res := range insight.Resources {
			fmt.Printf("   • %s: %s", res.Type, res.Name)
			if res.Namespace != "" {
				fmt.Printf(" (namespace: %s)", res.Namespace)
			}
			fmt.Println()
		}
		fmt.Println()
	}

	// Show prediction if available
	if insight.Prediction != nil {
		fmt.Printf("🔮 Prediction: %s\n", insight.Prediction.Type)
		fmt.Printf("   • Probability: %.0f%%\n", insight.Prediction.Probability*100)
		fmt.Printf("   • Time to event: %s\n", insight.Prediction.TimeToEvent.Round(time.Second))
		fmt.Printf("   • Confidence: %.0f%%\n", insight.Prediction.Confidence*100)
		fmt.Println()
	}

	// Show actionable items
	if len(insight.Actions) > 0 {
		fmt.Println("🛠️  Recommended Actions:")
		for i, action := range insight.Actions {
			fmt.Printf("\n   %d. %s\n", i+1, action.Title)
			fmt.Printf("      %s\n", action.Description)
			fmt.Printf("      Risk: %s | Impact: %s\n", action.Risk, action.EstimatedImpact)

			if len(action.Commands) > 0 {
				fmt.Println("\n      Commands to run:")
				for _, cmd := range action.Commands {
					fmt.Printf("      $ %s\n", cmd)
				}
			}
		}
	}

	fmt.Println()
}

func outputInsightJSON(insight collector.Insight) {
	// In production, would use proper JSON encoder
	fmt.Printf(`{
  "id": "%s",
  "timestamp": "%s",
  "type": "%s",
  "severity": "%s",
  "title": "%s",
  "description": "%s"
}
`, insight.ID, insight.Timestamp.Format(time.RFC3339), insight.Type,
		insight.Severity, insight.Title, insight.Description)
}

func outputPrometheus(insight collector.Insight) {
	// Output as Prometheus metrics
	severityValue := map[collector.Severity]float64{
		collector.SeverityCritical: 3,
		collector.SeverityHigh:     2,
		collector.SeverityMedium:   1,
		collector.SeverityLow:      0,
	}

	timestamp := insight.Timestamp.UnixMilli()

	// Insight metric
	fmt.Printf("tapio_insight_severity{type=\"%s\",title=\"%s\"} %f %d\n",
		insight.Type, insight.Title, severityValue[insight.Severity], timestamp)

	// Prediction metrics if available
	if insight.Prediction != nil {
		fmt.Printf("tapio_prediction_probability{type=\"%s\"} %f %d\n",
			insight.Prediction.Type, insight.Prediction.Probability, timestamp)

		fmt.Printf("tapio_prediction_time_to_event_seconds{type=\"%s\"} %f %d\n",
			insight.Prediction.Type, insight.Prediction.TimeToEvent.Seconds(), timestamp)

		fmt.Printf("tapio_prediction_confidence{type=\"%s\"} %f %d\n",
			insight.Prediction.Type, insight.Prediction.Confidence, timestamp)
	}
}

func reportHealth(ctx context.Context, manager *collector.SimpleManager) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := manager.GetStats()

			fmt.Printf("\n📊 Stats: Events: %d | Insights: %d | Correlations: %d | Pods: %d\n",
				stats["correlation_events_processed"],
				stats["correlation_insights_created"],
				stats["correlation_correlation_hits"],
				stats["correlation_tracked_pods"])

			// Check health
			health := manager.Health()
			unhealthy := 0
			for name, h := range health {
				if h.Status != collector.HealthStatusHealthy {
					unhealthy++
					fmt.Printf("⚠️  %s: %s - %s\n", name, h.Status, h.Message)
				}
			}

			if unhealthy == 0 {
				fmt.Println("✅ All systems healthy")
			}
		}
	}
}

func createK8sClient() (kubernetes.Interface, error) {
	// Try to get kubeconfig using standard approach
	config, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}
