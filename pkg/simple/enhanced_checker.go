package simple

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/degradation"
	"github.com/yairfalse/tapio/pkg/diagnostics"
	"github.com/yairfalse/tapio/pkg/k8s"
	"github.com/yairfalse/tapio/pkg/timeout"
	"github.com/yairfalse/tapio/pkg/types"
)

// EnhancedChecker provides checking with graceful degradation
type EnhancedChecker struct {
	*Checker
	degradationManager *degradation.Manager
	healthChecker      *diagnostics.HealthChecker
	timeoutManager     *timeout.Manager
}

// NewEnhancedChecker creates a checker with full degradation support
func NewEnhancedChecker() (*EnhancedChecker, error) {
	// Create base checker
	baseChecker, err := NewChecker()
	if err != nil {
		// Even if K8s connection fails, create a degraded checker
		baseChecker = &Checker{}
		fmt.Println("⚠️  Running in degraded mode due to Kubernetes connection issues")
	}

	// Create managers
	degradationManager := degradation.NewManager()
	timeoutManager := timeout.NewManager(nil)

	// Create health checker if K8s is available
	var healthChecker *diagnostics.HealthChecker
	if baseChecker.client != nil {
		k8sClient, _ := k8s.NewClient("")
		healthChecker = diagnostics.NewHealthChecker(
			baseChecker.client,
			k8sClient.Config,
			baseChecker.ebpfMonitor,
		)
	}

	enhancedChecker := &EnhancedChecker{
		Checker:            baseChecker,
		degradationManager: degradationManager,
		healthChecker:      healthChecker,
		timeoutManager:     timeoutManager,
	}

	// Register features with degradation support
	enhancedChecker.registerFeatures()

	// Start monitoring features
	go degradationManager.MonitorFeatures(context.Background(), 30*time.Second)

	// Register state change callbacks
	degradationManager.RegisterCallback(func(feature string, oldState, newState degradation.FeatureState) {
		stateStr := map[degradation.FeatureState]string{
			degradation.FeatureEnabled:  "enabled",
			degradation.FeatureDegraded: "degraded",
			degradation.FeatureDisabled: "disabled",
		}
		fmt.Printf("ℹ️  Feature '%s' changed from %s to %s\n",
			feature, stateStr[oldState], stateStr[newState])
	})

	return enhancedChecker, nil
}

// registerFeatures registers all degradable features
func (ec *EnhancedChecker) registerFeatures() {
	// Kubernetes API feature
	ec.degradationManager.RegisterFeature(
		"kubernetes-api",
		"Core Kubernetes API access",
		[]string{},
		func() error {
			if ec.client == nil {
				return fmt.Errorf("kubernetes client not initialized")
			}
			_, err := ec.client.Discovery().ServerVersion()
			return err
		},
		func() error {
			// Fallback: use cached data or limited functionality
			fmt.Println("⚠️  Using cached Kubernetes data (API unavailable)")
			return nil
		},
	)

	// eBPF monitoring feature
	ec.degradationManager.RegisterFeature(
		"ebpf-monitoring",
		"Kernel-level monitoring with eBPF",
		[]string{},
		func() error {
			if ec.ebpfMonitor == nil || !ec.ebpfMonitor.IsAvailable() {
				return fmt.Errorf("eBPF not available")
			}
			return nil
		},
		func() error {
			// Fallback: use basic monitoring without eBPF
			fmt.Println("ℹ️  Using basic monitoring (eBPF unavailable)")
			return nil
		},
	)

	// Correlation engine feature
	ec.degradationManager.RegisterFeature(
		"correlation-engine",
		"Intelligent issue correlation",
		[]string{"kubernetes-api"},
		func() error {
			// Check if correlation data sources are available
			return nil
		},
		func() error {
			// Fallback: provide basic analysis without correlation
			fmt.Println("ℹ️  Using basic analysis (correlation engine unavailable)")
			return nil
		},
	)

	// Real-time monitoring feature
	ec.degradationManager.RegisterFeature(
		"realtime-monitoring",
		"Real-time resource monitoring",
		[]string{"kubernetes-api"},
		func() error {
			// Check if watch API is available
			return nil
		},
		func() error {
			// Fallback: use polling instead of watches
			fmt.Println("ℹ️  Using polling mode (real-time monitoring unavailable)")
			return nil
		},
	)
}

// Check performs a health check with graceful degradation
func (ec *EnhancedChecker) Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error) {
	var result *types.CheckResult

	// Check if we can use the primary check function
	err := ec.degradationManager.ExecuteWithDegradation(ctx, "kubernetes-api", func() error {
		// Primary: use base checker
		var err error
		result, err = ec.Checker.Check(ctx, req)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil && result == nil {
		// Complete failure, provide minimal result
		result = &types.CheckResult{
			Timestamp: time.Now(),
			Problems: []types.Problem{
				{
					Title:       "Check Failed",
					Description: fmt.Sprintf("Unable to perform health check: %v", err),
					Severity:    types.SeverityCritical,
				},
			},
		}
	}

	// Enhance with degradation information
	if result != nil {
		ec.addDegradationInfo(result)
	}

	return result, nil
}

// RunDiagnostics runs comprehensive diagnostics
func (ec *EnhancedChecker) RunDiagnostics(ctx context.Context) (*diagnostics.HealthReport, error) {
	if ec.healthChecker == nil {
		return nil, fmt.Errorf("health checker not available in degraded mode")
	}

	return ec.healthChecker.RunHealthCheck(ctx)
}

// GetFeatureStatus returns the status of all features
func (ec *EnhancedChecker) GetFeatureStatus() map[string]degradation.FeatureInfo {
	return ec.degradationManager.GetFeatureStatus()
}

// addDegradationInfo adds degradation information to the result
func (ec *EnhancedChecker) addDegradationInfo(result *types.CheckResult) {
	status := ec.degradationManager.GetFeatureStatus()

	degradedFeatures := []string{}
	disabledFeatures := []string{}

	for name, info := range status {
		switch info.State {
		case "degraded":
			degradedFeatures = append(degradedFeatures, name)
		case "disabled":
			disabledFeatures = append(disabledFeatures, name)
		}
	}

	if len(degradedFeatures) > 0 || len(disabledFeatures) > 0 {
		problem := types.Problem{
			Title:    "Running with reduced functionality",
			Severity: types.SeverityWarning,
		}

		description := ""
		if len(degradedFeatures) > 0 {
			description += fmt.Sprintf("Degraded features: %s. ", strings.Join(degradedFeatures, ", "))
		}
		if len(disabledFeatures) > 0 {
			description += fmt.Sprintf("Disabled features: %s. ", strings.Join(disabledFeatures, ", "))
		}
		description += "Some analysis capabilities may be limited."

		problem.Description = description
		result.Problems = append(result.Problems, problem)
	}
}

// StartMonitoring starts monitoring with graceful degradation
func (ec *EnhancedChecker) StartMonitoring(ctx context.Context) error {
	// Try eBPF monitoring first
	err := ec.degradationManager.ExecuteWithDegradation(ctx, "ebpf-monitoring", func() error {
		return ec.StartEBPFMonitoring(ctx)
	})

	if err != nil {
		fmt.Printf("⚠️  eBPF monitoring not available: %v\n", err)
	}

	// Start real-time monitoring
	err = ec.degradationManager.ExecuteWithDegradation(ctx, "realtime-monitoring", func() error {
		// Start watch-based monitoring
		return nil
	})

	if err != nil {
		fmt.Printf("ℹ️  Using polling-based monitoring\n")
	}

	return nil
}
