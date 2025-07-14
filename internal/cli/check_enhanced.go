package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/health"
	"github.com/yairfalse/tapio/pkg/output"
	"k8s.io/client-go/kubernetes"
)

// EnhancedChecker combines local health checking with correlation insights
type EnhancedChecker struct {
	k8sClient    kubernetes.Interface
	healthChecker *health.Checker
	corrClient   *CorrelationClient
}

// NewEnhancedChecker creates a new enhanced checker
func NewEnhancedChecker(k8sClient kubernetes.Interface) (*EnhancedChecker, error) {
	healthChecker, err := health.NewChecker(k8sClient)
	if err != nil {
		return nil, err
	}

	// Try to connect to correlation server (optional)
	corrClient, _ := NewCorrelationClient("")

	return &EnhancedChecker{
		k8sClient:     k8sClient,
		healthChecker: healthChecker,
		corrClient:    corrClient,
	}, nil
}

// CheckResource performs enhanced health check with predictions
func (ec *EnhancedChecker) CheckResource(ctx context.Context, resourceType, resourceName, namespace string) (*EnhancedHealthReport, error) {
	// First, do basic health check
	basicHealth, err := ec.healthChecker.CheckResource(ctx, resourceType, resourceName, namespace)
	if err != nil {
		return nil, err
	}

	report := &EnhancedHealthReport{
		BasicHealth:  basicHealth,
		Predictions:  []*correlation.Prediction{},
		Insights:     []*correlation.InsightResponse{},
		TimeChecked:  time.Now(),
	}

	// Try to get insights from correlation server
	if ec.corrClient != nil {
		insights, err := ec.corrClient.GetInsights(ctx, resourceName, namespace)
		if err == nil && len(insights) > 0 {
			report.Insights = insights
			
			// Extract predictions
			for _, insight := range insights {
				if insight.Prediction != nil {
					report.Predictions = append(report.Predictions, insight.Prediction)
				}
			}
			report.UsingCorrelation = true
		}
	}

	// If no correlation data, try local analysis
	if !report.UsingCorrelation {
		report.LocalAnalysis = ec.performLocalAnalysis(basicHealth)
	}

	return report, nil
}

// performLocalAnalysis does basic trend analysis locally
func (ec *EnhancedChecker) performLocalAnalysis(health *health.Report) *LocalAnalysis {
	analysis := &LocalAnalysis{
		Warnings: []string{},
		Suggestions: []string{},
	}

	// Check for high restart count
	if health.RestartCount > 5 {
		analysis.Warnings = append(analysis.Warnings, 
			fmt.Sprintf("High restart count: %d restarts in last hour", health.RestartCount))
		analysis.Suggestions = append(analysis.Suggestions,
			"Check pod logs for crash reasons: kubectl logs <pod> --previous")
	}

	// Check memory usage
	if health.MemoryUsagePercent > 80 {
		analysis.Warnings = append(analysis.Warnings,
			fmt.Sprintf("High memory usage: %.1f%%", health.MemoryUsagePercent))
		
		// Simple OOM prediction based on current usage
		if health.MemoryUsagePercent > 90 {
			minutesToOOM := (100 - health.MemoryUsagePercent) * 10 // Simple linear prediction
			analysis.Predictions = append(analysis.Predictions,
				fmt.Sprintf("Pod may OOM in approximately %.0f minutes", minutesToOOM))
		}
	}

	// Check CPU usage
	if health.CPUUsagePercent > 80 {
		analysis.Warnings = append(analysis.Warnings,
			fmt.Sprintf("High CPU usage: %.1f%%", health.CPUUsagePercent))
	}

	return analysis
}

// EnhancedHealthReport combines basic health with predictions
type EnhancedHealthReport struct {
	BasicHealth      *health.Report
	Predictions      []*correlation.Prediction
	Insights         []*correlation.InsightResponse
	LocalAnalysis    *LocalAnalysis
	UsingCorrelation bool
	TimeChecked      time.Time
}

// LocalAnalysis provides fallback analysis when correlation server unavailable
type LocalAnalysis struct {
	Warnings    []string
	Predictions []string
	Suggestions []string
}

// FormatEnhancedReport formats the enhanced health report for output
func FormatEnhancedReport(report *EnhancedHealthReport) string {
	var output string

	// Basic health status
	output += output.FormatHealthStatus(report.BasicHealth)

	// Add predictions if available
	if len(report.Predictions) > 0 {
		output += "\n🔮 PREDICTIONS:\n"
		for _, pred := range report.Predictions {
			output += fmt.Sprintf("   → %s\n", FormatPrediction(pred))
		}
	}

	// Add insights if available
	if len(report.Insights) > 0 {
		output += "\n💡 INSIGHTS:\n"
		for _, insight := range report.Insights {
			if insight.Severity == "critical" || insight.Severity == "high" {
				output += FormatInsight(insight)
			}
		}
	}

	// Add local analysis if no correlation data
	if report.LocalAnalysis != nil && !report.UsingCorrelation {
		if len(report.LocalAnalysis.Warnings) > 0 {
			output += "\n⚠️  WARNINGS:\n"
			for _, warning := range report.LocalAnalysis.Warnings {
				output += fmt.Sprintf("   → %s\n", warning)
			}
		}

		if len(report.LocalAnalysis.Predictions) > 0 {
			output += "\n🔮 PREDICTIONS (local analysis):\n"
			for _, pred := range report.LocalAnalysis.Predictions {
				output += fmt.Sprintf("   → %s\n", pred)
			}
		}
	}

	// Add correlation server status
	if report.UsingCorrelation {
		output += "\n✅ Using advanced correlation analysis"
	} else {
		output += "\n⚡ Using local analysis (correlation server unavailable)"
	}

	return output
}

// Example usage in check command:
func RunEnhancedCheck(ctx context.Context, k8sClient kubernetes.Interface, resource string) error {
	checker, err := NewEnhancedChecker(k8sClient)
	if err != nil {
		return err
	}

	// Parse resource (simplified)
	resourceType := "pod"
	resourceName := resource
	namespace := "default"

	report, err := checker.CheckResource(ctx, resourceType, resourceName, namespace)
	if err != nil {
		return err
	}

	fmt.Println(FormatEnhancedReport(report))
	return nil
}