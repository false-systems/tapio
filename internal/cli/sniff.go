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

	"github.com/falseyair/tapio/pkg/ebpf"
	"github.com/falseyair/tapio/pkg/simple"
	"github.com/falseyair/tapio/pkg/sniffer"
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
	sniffCmd.Flags().BoolVar(&sniffK8sOnly, "k8s-only", false, "Only run K8s API sniffer")
	sniffCmd.Flags().BoolVar(&sniffEBPFOnly, "ebpf-only", false, "Only run eBPF sniffer")
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
	config := sniffer.DefaultManagerConfig()
	config.CorrelationBatchSize = sniffBatchSize
	manager := sniffer.NewSimpleManager(config)
	
	// Register sniffers based on flags
	if !sniffK8sOnly {
		// Create eBPF monitor
		ebpfMonitor := ebpf.NewMonitor()
		
		// Create PID translator
		translator := sniffer.NewSimplePIDTranslator(client)
		if err := translator.Start(ctx); err != nil {
			return fmt.Errorf("failed to start PID translator: %w", err)
		}
		
		// Create and register eBPF sniffer
		ebpfSniffer := sniffer.NewEBPFSniffer(ebpfMonitor, translator)
		if err := manager.Register(ebpfSniffer); err != nil {
			return fmt.Errorf("failed to register eBPF sniffer: %w", err)
		}
		
		fmt.Println("✓ eBPF sniffer registered")
	}
	
	if !sniffEBPFOnly {
		// Create and register K8s sniffer
		k8sSniffer := sniffer.NewK8sSniffer(client)
		if err := manager.Register(k8sSniffer); err != nil {
			return fmt.Errorf("failed to register K8s sniffer: %w", err)
		}
		
		fmt.Println("✓ K8s API sniffer registered")
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

func handleOutput(ctx context.Context, manager *sniffer.SimpleManager) {
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
				outputJSON(insight)
			case "prometheus":
				outputPrometheus(insight)
			default:
				outputText(insight)
			}
		}
	}
}

func outputText(insight sniffer.Insight) {
	// Color codes for severity
	severityColor := map[sniffer.Severity]string{
		sniffer.SeverityCritical: "\033[31m", // Red
		sniffer.SeverityHigh:     "\033[33m", // Yellow
		sniffer.SeverityMedium:   "\033[36m", // Cyan
		sniffer.SeverityLow:      "\033[32m", // Green
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

func outputJSON(insight sniffer.Insight) {
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

func outputPrometheus(insight sniffer.Insight) {
	// Output as Prometheus metrics
	severityValue := map[sniffer.Severity]float64{
		sniffer.SeverityCritical: 3,
		sniffer.SeverityHigh:     2,
		sniffer.SeverityMedium:   1,
		sniffer.SeverityLow:      0,
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

func reportHealth(ctx context.Context, manager *sniffer.SimpleManager) {
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
				if h.Status != sniffer.HealthStatusHealthy {
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
	// Try to get kubeconfig
	config, err := simple.GetKubeConfig()
	if err != nil {
		return nil, err
	}
	
	return kubernetes.NewForConfig(config)
}