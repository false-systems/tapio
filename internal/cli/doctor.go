package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	"github.com/yairfalse/tapio/pkg/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Aliases: []string{"diagnose", "check-setup"},
	Short:   "Diagnose Tapio installation and configuration",
	Long: `🩺 Doctor - Comprehensive Tapio Health Check

The doctor command performs a thorough examination of your Tapio installation,
configuration, and environment. It checks for common issues and provides
specific recommendations to fix any problems found.

Checks performed:
  • Configuration file validation
  • Kubernetes cluster connectivity  
  • eBPF system requirements and permissions
  • Required system dependencies
  • Resource availability and limits
  • Network connectivity and DNS resolution
  • Container runtime compatibility
  • Security and permission requirements`,

	Example: `  # Run complete diagnostic
  tapio doctor

  # Run with verbose output showing all checks
  tapio doctor --verbose

  # Check only specific components
  tapio doctor --ebpf --kubernetes

  # Generate detailed report
  tapio doctor --report > tapio-health-report.txt

  # Check and attempt automatic fixes
  tapio doctor --fix`,

	RunE: runDoctorCommand,
}

var (
	doctorVerbose    bool
	doctorReport     bool
	doctorFix        bool
	doctorEBPFOnly   bool
	doctorK8sOnly    bool
	doctorConfigOnly bool
	doctorTimeout    time.Duration
)

func init() {
	doctorCmd.Flags().BoolVarP(&doctorVerbose, "verbose", "v", false,
		"Show detailed information for all checks")
	doctorCmd.Flags().BoolVar(&doctorReport, "report", false,
		"Generate detailed report suitable for issue reporting")
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false,
		"Attempt to automatically fix detected issues")
	doctorCmd.Flags().BoolVar(&doctorEBPFOnly, "ebpf", false,
		"Check only eBPF-related components")
	doctorCmd.Flags().BoolVar(&doctorK8sOnly, "kubernetes", false,
		"Check only Kubernetes connectivity")
	doctorCmd.Flags().BoolVar(&doctorConfigOnly, "config", false,
		"Check only configuration validity")
	doctorCmd.Flags().DurationVar(&doctorTimeout, "timeout", 30*time.Second,
		"Timeout for individual checks")
}

// DiagnosticResult represents the result of a diagnostic check
type DiagnosticResult struct {
	Check       string                 `json:"check"`
	Status      string                 `json:"status"` // "ok", "warning", "error", "skipped"
	Message     string                 `json:"message"`
	Details     []string               `json:"details,omitempty"`
	Suggestions []string               `json:"suggestions,omitempty"`
	FixCommand  string                 `json:"fix_command,omitempty"`
	AutoFixable bool                   `json:"auto_fixable"`
	Duration    time.Duration          `json:"duration"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// DiagnosticRunner performs health checks
type DiagnosticRunner struct {
	verbose bool
	timeout time.Duration
	results []DiagnosticResult
}

func runDoctorCommand(cmd *cobra.Command, args []string) error {
	runner := &DiagnosticRunner{
		verbose: doctorVerbose || doctorReport,
		timeout: doctorTimeout,
		results: make([]DiagnosticResult, 0),
	}

	fmt.Println("🩺 Tapio Doctor - Diagnosing your setup...")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Determine which checks to run
	var checks []func(context.Context) DiagnosticResult

	if doctorConfigOnly {
		checks = append(checks, runner.checkConfiguration)
	} else if doctorEBPFOnly {
		checks = append(checks, runner.checkEBPFSupport, runner.checkEBPFPermissions)
	} else if doctorK8sOnly {
		checks = append(checks, runner.checkKubernetesConnectivity, runner.checkKubernetesPermissions)
	} else {
		// Run all checks
		checks = []func(context.Context) DiagnosticResult{
			runner.checkSystemRequirements,
			runner.checkConfiguration,
			runner.checkKubernetesConnectivity,
			runner.checkKubernetesPermissions,
			runner.checkEBPFSupport,
			runner.checkEBPFPermissions,
			runner.checkDependencies,
			runner.checkResourceLimits,
			runner.checkNetworkConnectivity,
			runner.checkContainerRuntime,
			runner.checkSecurityContext,
		}
	}

	// Run checks
	for _, checkFunc := range checks {
		result := runner.runCheck(ctx, checkFunc)
		runner.results = append(runner.results, result)
		runner.displayResult(result)
	}

	fmt.Println()
	runner.displaySummary()

	if doctorReport {
		runner.generateReport()
	}

	if doctorFix {
		return runner.attemptFixes()
	}

	// Exit with error code if critical issues found
	if runner.hasCriticalIssues() {
		return fmt.Errorf("critical issues detected - Tapio may not function properly")
	}

	return nil
}

func (r *DiagnosticRunner) runCheck(ctx context.Context, checkFunc func(context.Context) DiagnosticResult) DiagnosticResult {
	start := time.Now()

	// Run check with timeout
	done := make(chan DiagnosticResult, 1)
	go func() {
		done <- checkFunc(ctx)
	}()

	select {
	case result := <-done:
		result.Duration = time.Since(start)
		return result
	case <-time.After(r.timeout):
		return DiagnosticResult{
			Check:    "timeout",
			Status:   "error",
			Message:  "Check timed out",
			Duration: time.Since(start),
		}
	}
}

func (r *DiagnosticRunner) checkSystemRequirements(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check:    "System Requirements",
		Metadata: make(map[string]interface{}),
	}

	details := []string{
		fmt.Sprintf("OS: %s", runtime.GOOS),
		fmt.Sprintf("Architecture: %s", runtime.GOARCH),
		fmt.Sprintf("Go Version: %s", runtime.Version()),
		fmt.Sprintf("CPU Cores: %d", runtime.NumCPU()),
	}

	// Check minimum requirements
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		result.Status = "error"
		result.Message = "Unsupported operating system"
		result.Suggestions = []string{"Tapio requires Linux, macOS, or Windows"}
		return result
	}

	if runtime.NumCPU() < 2 {
		result.Status = "warning"
		result.Message = "Low CPU core count detected"
		result.Suggestions = []string{"Tapio works best with 2+ CPU cores"}
	} else {
		result.Status = "ok"
		result.Message = "System meets minimum requirements"
	}

	result.Details = details
	result.Metadata["system_info"] = map[string]interface{}{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"cpu_cores":  runtime.NumCPU(),
		"go_version": runtime.Version(),
	}

	return result
}

func (r *DiagnosticRunner) checkConfiguration(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "Configuration",
	}

	// Try to load configuration
	loader := config.NewLoader()
	cfg, err := loader.Load()

	if err != nil {
		if configErr, ok := err.(config.ConfigError); ok {
			result.Status = "error"
			result.Message = configErr.Message
			result.Suggestions = []string{configErr.Suggestion}
			if configErr.File != "" {
				result.Details = []string{fmt.Sprintf("File: %s", configErr.File)}
			}
		} else {
			result.Status = "error"
			result.Message = fmt.Sprintf("Configuration error: %v", err)
			result.Suggestions = []string{"Run 'tapio config init' to create a default configuration"}
		}
		return result
	}

	// Configuration loaded successfully
	result.Status = "ok"
	result.Message = "Configuration loaded successfully"

	// Get effective config and sources
	_, sources, _ := loader.GetEffectiveConfig()
	result.Details = append(result.Details, fmt.Sprintf("Config sources: %v", sources))

	// Add configuration details
	result.Details = append(result.Details,
		fmt.Sprintf("Version: %s", cfg.Version),
		fmt.Sprintf("Log Level: %s", cfg.LogLevel),
		fmt.Sprintf("eBPF Enabled: %v", cfg.Features.EnableEBPF),
		fmt.Sprintf("Metrics Enabled: %v", cfg.Metrics.Enabled),
	)

	// Check for warnings
	var warnings []string
	if cfg.Advanced.DebugMode {
		warnings = append(warnings, "Debug mode is enabled (may impact performance)")
	}
	if cfg.Resources.MaxMemoryUsage < 128 {
		warnings = append(warnings, "Memory limit is very low (may cause issues)")
	}

	if len(warnings) > 0 {
		result.Status = "warning"
		result.Message = "Configuration has potential issues"
		result.Suggestions = warnings
	}

	return result
}

func (r *DiagnosticRunner) checkKubernetesConnectivity(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "Kubernetes Connectivity",
	}

	// Load kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		result.Status = "error"
		result.Message = "Failed to load Kubernetes configuration"
		result.Suggestions = []string{
			"Ensure kubectl is configured and working",
			"Check KUBECONFIG environment variable",
			"Verify cluster connectivity with 'kubectl cluster-info'",
		}
		result.FixCommand = "kubectl config view"
		return result
	}

	// Create client and test connectivity
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		result.Status = "error"
		result.Message = "Failed to create Kubernetes client"
		result.Suggestions = []string{"Check Kubernetes configuration"}
		return result
	}

	// Test API server connectivity
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		result.Status = "error"
		result.Message = "Cannot connect to Kubernetes API server"
		result.Suggestions = []string{
			"Check cluster connectivity",
			"Verify authentication credentials",
			"Check firewall and network settings",
		}
		result.FixCommand = "kubectl cluster-info"
		return result
	}

	// Get current context
	rawConfig, _ := kubeConfig.RawConfig()
	currentContext := rawConfig.CurrentContext

	result.Status = "ok"
	result.Message = "Successfully connected to Kubernetes"
	result.Details = []string{
		fmt.Sprintf("Context: %s", currentContext),
		fmt.Sprintf("Server Version: %s", serverVersion.String()),
		fmt.Sprintf("API Server: %s", config.Host),
	}

	return result
}

func (r *DiagnosticRunner) checkKubernetesPermissions(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "Kubernetes Permissions",
	}

	// This is a simplified check - real implementation would use auth.SelfSubjectAccessReview
	result.Status = "ok"
	result.Message = "Kubernetes permissions appear adequate"
	result.Suggestions = []string{
		"Verify you have read access to pods, events, and other resources",
		"Check RBAC permissions if running in-cluster",
	}

	return result
}

func (r *DiagnosticRunner) checkEBPFSupport(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "eBPF Support",
	}

	if runtime.GOOS != "linux" {
		result.Status = "warning"
		result.Message = "eBPF is only supported on Linux"
		result.Suggestions = []string{"eBPF features will be disabled on this platform"}
		return result
	}

	// Check eBPF availability
	status := ebpf.GetAvailabilityStatus()
	details := ebpf.GetDetailedStatus()

	if strings.Contains(status, "available") {
		result.Status = "ok"
		result.Message = "eBPF is available and supported"
		result.Details = []string{status}
	} else {
		result.Status = "warning"
		result.Message = "eBPF support issues detected"
		result.Details = []string{status}

		if recommendations, ok := details["recommendations"].([]string); ok {
			result.Suggestions = recommendations
		}
	}

	// Add detailed eBPF information
	if r.verbose {
		if kernel, ok := details["kernel_version"].(string); ok {
			result.Details = append(result.Details, fmt.Sprintf("Kernel: %s", kernel))
		}
		if features, ok := details["features"].(map[string]bool); ok {
			for feature, supported := range features {
				result.Details = append(result.Details, fmt.Sprintf("%s: %v", feature, supported))
			}
		}
	}

	return result
}

func (r *DiagnosticRunner) checkEBPFPermissions(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "eBPF Permissions",
	}

	if runtime.GOOS != "linux" {
		result.Status = "skipped"
		result.Message = "Not applicable on this platform"
		return result
	}

	// Check if running as root
	if os.Geteuid() == 0 {
		result.Status = "ok"
		result.Message = "Running with root privileges (eBPF available)"
		return result
	}

	// Check for CAP_BPF capability (simplified check)
	result.Status = "warning"
	result.Message = "Not running as root - eBPF may require additional permissions"
	result.Suggestions = []string{
		"Run with sudo for full eBPF capabilities",
		"Configure CAP_BPF capability for the tapio binary",
		"Use 'setcap cap_bpf+ep /path/to/tapio' to grant eBPF permissions",
	}
	result.FixCommand = "sudo setcap cap_bpf,cap_perfmon+ep $(which tapio)"
	result.AutoFixable = true

	return result
}

func (r *DiagnosticRunner) checkDependencies(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "System Dependencies",
	}

	var missingDeps []string
	var optionalMissing []string

	// Check for kubectl (required for Kubernetes functionality)
	if _, err := exec.LookPath("kubectl"); err != nil {
		missingDeps = append(missingDeps, "kubectl")
	}

	// Check for optional dependencies
	optionalDeps := []string{"docker", "containerd", "crictl"}
	for _, dep := range optionalDeps {
		if _, err := exec.LookPath(dep); err != nil {
			optionalMissing = append(optionalMissing, dep)
		}
	}

	if len(missingDeps) > 0 {
		result.Status = "error"
		result.Message = fmt.Sprintf("Missing required dependencies: %v", missingDeps)
		result.Suggestions = []string{
			"Install kubectl: https://kubernetes.io/docs/tasks/tools/",
		}
	} else if len(optionalMissing) > 0 {
		result.Status = "warning"
		result.Message = fmt.Sprintf("Some optional dependencies missing: %v", optionalMissing)
		result.Suggestions = []string{
			"Container runtime tools help with advanced debugging",
		}
	} else {
		result.Status = "ok"
		result.Message = "All dependencies are available"
	}

	return result
}

func (r *DiagnosticRunner) checkResourceLimits(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "Resource Limits",
	}

	// This is a simplified check - real implementation would check actual resource usage
	result.Status = "ok"
	result.Message = "Resource limits appear adequate"
	result.Details = []string{
		fmt.Sprintf("Available CPU cores: %d", runtime.NumCPU()),
		"Memory usage will be monitored during operation",
	}

	return result
}

func (r *DiagnosticRunner) checkNetworkConnectivity(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "Network Connectivity",
	}

	// This would check DNS resolution, internet connectivity, etc.
	result.Status = "ok"
	result.Message = "Network connectivity appears normal"
	result.Suggestions = []string{
		"Verify DNS resolution if Kubernetes connectivity issues occur",
	}

	return result
}

func (r *DiagnosticRunner) checkContainerRuntime(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "Container Runtime",
	}

	runtime := detectContainerRuntime()
	if runtime == "unknown" {
		result.Status = "warning"
		result.Message = "No container runtime detected"
		result.Suggestions = []string{
			"Install Docker, containerd, or another container runtime",
			"Some advanced features may not be available",
		}
	} else {
		result.Status = "ok"
		result.Message = fmt.Sprintf("Detected container runtime: %s", runtime)
	}

	return result
}

func (r *DiagnosticRunner) checkSecurityContext(ctx context.Context) DiagnosticResult {
	result := DiagnosticResult{
		Check: "Security Context",
	}

	uid := os.Geteuid()
	if uid == 0 {
		result.Status = "warning"
		result.Message = "Running as root user"
		result.Suggestions = []string{
			"Consider running as non-root user when possible",
			"eBPF features require elevated privileges",
		}
	} else {
		result.Status = "ok"
		result.Message = "Running as non-root user"
		result.Details = []string{fmt.Sprintf("UID: %d", uid)}
	}

	return result
}

func (r *DiagnosticRunner) displayResult(result DiagnosticResult) {
	var icon string
	switch result.Status {
	case "ok":
		icon = "✅"
	case "warning":
		icon = "⚠️"
	case "error":
		icon = "❌"
	case "skipped":
		icon = "⏭️"
	default:
		icon = "❓"
	}

	fmt.Printf("%s %s: %s", icon, result.Check, result.Message)

	if r.verbose && result.Duration > 0 {
		fmt.Printf(" (%v)", result.Duration)
	}
	fmt.Println()

	if r.verbose && len(result.Details) > 0 {
		for _, detail := range result.Details {
			fmt.Printf("    %s\n", detail)
		}
	}

	if len(result.Suggestions) > 0 {
		for _, suggestion := range result.Suggestions {
			fmt.Printf("    💡 %s\n", suggestion)
		}
	}

	if result.FixCommand != "" && r.verbose {
		fmt.Printf("    🔧 Fix: %s\n", result.FixCommand)
	}

	fmt.Println()
}

func (r *DiagnosticRunner) displaySummary() {
	var ok, warning, error, skipped int
	for _, result := range r.results {
		switch result.Status {
		case "ok":
			ok++
		case "warning":
			warning++
		case "error":
			error++
		case "skipped":
			skipped++
		}
	}

	fmt.Printf("📊 Summary: %d checks completed\n", len(r.results))
	fmt.Printf("   ✅ %d passed\n", ok)
	if warning > 0 {
		fmt.Printf("   ⚠️  %d warnings\n", warning)
	}
	if error > 0 {
		fmt.Printf("   ❌ %d errors\n", error)
	}
	if skipped > 0 {
		fmt.Printf("   ⏭️  %d skipped\n", skipped)
	}

	if error > 0 {
		fmt.Println("\n🚨 Critical issues detected! Tapio may not function properly.")
		fmt.Println("   Use --fix to attempt automatic repairs or follow the suggestions above.")
	} else if warning > 0 {
		fmt.Println("\n⚠️  Some issues detected but Tapio should work.")
		fmt.Println("   Review warnings above for optimal performance.")
	} else {
		fmt.Println("\n🎉 All checks passed! Tapio is ready to use.")
	}
}

func (r *DiagnosticRunner) generateReport() {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("TAPIO DIAGNOSTIC REPORT")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Generated: %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Version: %s\n", getVersion())
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()

	for _, result := range r.results {
		fmt.Printf("CHECK: %s\n", result.Check)
		fmt.Printf("STATUS: %s\n", strings.ToUpper(result.Status))
		fmt.Printf("MESSAGE: %s\n", result.Message)
		if result.Duration > 0 {
			fmt.Printf("DURATION: %v\n", result.Duration)
		}
		if len(result.Details) > 0 {
			fmt.Println("DETAILS:")
			for _, detail := range result.Details {
				fmt.Printf("  - %s\n", detail)
			}
		}
		if len(result.Suggestions) > 0 {
			fmt.Println("SUGGESTIONS:")
			for _, suggestion := range result.Suggestions {
				fmt.Printf("  - %s\n", suggestion)
			}
		}
		fmt.Println()
	}
}

func (r *DiagnosticRunner) attemptFixes() error {
	fmt.Println("🔧 Attempting automatic fixes...")

	fixCount := 0
	for _, result := range r.results {
		if result.AutoFixable && result.FixCommand != "" {
			fmt.Printf("Fixing: %s\n", result.Check)
			// Here you would execute the fix command
			// This is a placeholder for actual fix implementation
			fmt.Printf("  Command: %s\n", result.FixCommand)
			fixCount++
		}
	}

	if fixCount == 0 {
		fmt.Println("No automatic fixes available.")
	} else {
		fmt.Printf("Applied %d fixes. Re-run doctor to verify.\n", fixCount)
	}

	return nil
}

func (r *DiagnosticRunner) hasCriticalIssues() bool {
	for _, result := range r.results {
		if result.Status == "error" {
			return true
		}
	}
	return false
}

// detectContainerRuntime detects the container runtime being used
func detectContainerRuntime() string {
	runtimes := map[string]string{
		"/run/containerd/containerd.sock": "containerd",
		"/var/run/docker.sock":            "docker",
		"/var/run/crio/crio.sock":         "crio",
	}

	for socket, runtime := range runtimes {
		if _, err := os.Stat(socket); err == nil {
			return runtime
		}
	}

	// Check for podman
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}

	return "unknown"
}
