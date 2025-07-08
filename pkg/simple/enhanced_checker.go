package simple

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	// "github.com/falseyair/tapio/pkg/ebpf" // Temporarily disabled for demo
	"github.com/falseyair/tapio/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// EnhancedChecker combines Kubernetes API data with eBPF kernel data
type EnhancedChecker struct {
	*Checker // Embed basic checker
	ebpfCollector *ebpf.Collector
	enableEBPF    bool
}

// NewEnhancedChecker creates a checker with optional eBPF capabilities
func NewEnhancedChecker() (*EnhancedChecker, error) {
	baseChecker, err := NewChecker()
	if err != nil {
		return nil, err
	}

	checker := &EnhancedChecker{
		Checker:    baseChecker,
		enableEBPF: runtime.GOOS == "linux",
	}

	// Try to initialize eBPF collector on Linux
	if checker.enableEBPF {
		collector, err := ebpf.NewCollector()
		if err != nil {
			// Don't fail completely, just disable eBPF
			fmt.Printf("Warning: eBPF initialization failed: %v\n", err)
			fmt.Println("Continuing with Kubernetes API data only")
			checker.enableEBPF = false
		} else {
			checker.ebpfCollector = collector
			fmt.Println("eBPF kernel monitoring enabled")
		}
	}

	return checker, nil
}

// Check performs enhanced health check with eBPF correlation
func (c *EnhancedChecker) Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error) {
	// Get basic Kubernetes check results
	result, err := c.Checker.Check(ctx, req)
	if err != nil {
		return nil, err
	}

	// Enhance with eBPF data if available
	if c.enableEBPF && c.ebpfCollector != nil {
		if err := c.enhanceWithEBPF(ctx, result, req); err != nil {
			// Don't fail the whole check, just log the error
			fmt.Printf("Warning: eBPF enhancement failed: %v\n", err)
		}
	}

	return result, nil
}

// enhanceWithEBPF adds eBPF insights to the check result
func (c *EnhancedChecker) enhanceWithEBPF(ctx context.Context, result *types.CheckResult, req *types.CheckRequest) error {
	// Get container processes from eBPF
	containerStats := c.ebpfCollector.GetContainerProcesses()

	// Get pods to correlate with eBPF data
	namespace := req.Namespace
	if namespace == "" && !req.All {
		namespace = "default"
	}

	pods, err := c.getPods(ctx, namespace, req.All)
	if err != nil {
		return fmt.Errorf("failed to get pods for eBPF correlation: %w", err)
	}

	// Create PID to pod mapping
	pidToPod := c.createPIDToPodMapping(pods, containerStats)

	// Get memory limits for processes
	memoryLimits := c.extractMemoryLimits(pods, pidToPod)

	// Get OOM predictions
	predictions := c.ebpfCollector.GetMemoryPredictions(memoryLimits)

	// Enhance existing problems or create new ones
	c.enhanceProblemsWithPredictions(result, predictions, pidToPod)

	return nil
}

// createPIDToPodMapping maps container PIDs to pods
func (c *EnhancedChecker) createPIDToPodMapping(pods []corev1.Pod, containerStats map[uint32]*ebpf.ProcessMemoryStats) map[uint32]*corev1.Pod {
	pidToPod := make(map[uint32]*corev1.Pod)

	// This is a simplified mapping - in production you'd use container runtime info
	// For now, we'll match by container PID patterns
	for i := range pods {
		pod := &pods[i]
		// In real implementation, you'd query container runtime for PID mapping
		// For demo, we'll use a heuristic based on process names
		for pid, stats := range containerStats {
			if stats.InContainer && c.isProcessFromPod(stats, pod) {
				pidToPod[pid] = pod
			}
		}
	}

	return pidToPod
}

// isProcessFromPod heuristically determines if a process belongs to a pod
func (c *EnhancedChecker) isProcessFromPod(stats *ebpf.ProcessMemoryStats, pod *corev1.Pod) bool {
	// Simple heuristic: check if process command matches container name or image
	for _, container := range pod.Spec.Containers {
		if strings.Contains(stats.Command, container.Name) {
			return true
		}
		// Extract binary name from image
		imageParts := strings.Split(container.Image, "/")
		imageName := imageParts[len(imageParts)-1]
		imageName = strings.Split(imageName, ":")[0]
		if strings.Contains(stats.Command, imageName) {
			return true
		}
	}
	return false
}

// extractMemoryLimits gets memory limits for processes
func (c *EnhancedChecker) extractMemoryLimits(pods []corev1.Pod, pidToPod map[uint32]*corev1.Pod) map[uint32]uint64 {
	limits := make(map[uint32]uint64)

	for pid, pod := range pidToPod {
		// Get memory limit from first container (simplified)
		if len(pod.Spec.Containers) > 0 {
			container := pod.Spec.Containers[0]
			if memLimit, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
				limits[pid] = uint64(memLimit.Value())
			}
		}
	}

	return limits
}

// enhanceProblemsWithPredictions adds eBPF predictions to problems
func (c *EnhancedChecker) enhanceProblemsWithPredictions(result *types.CheckResult, predictions map[uint32]*ebpf.OOMPrediction, pidToPod map[uint32]*corev1.Pod) {
	// Create map of existing problems by pod name
	problemsByPod := make(map[string]*types.Problem)
	for i := range result.Problems {
		problem := &result.Problems[i]
		if problem.Resource.Kind == "pod" {
			key := fmt.Sprintf("%s/%s", problem.Resource.Namespace, problem.Resource.Name)
			problemsByPod[key] = problem
		}
	}

	// Add predictions to problems
	for pid, prediction := range predictions {
		if pod, ok := pidToPod[pid]; ok {
			key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

			// Check if we already have a problem for this pod
			if existingProblem, exists := problemsByPod[key]; exists {
				// Enhance existing problem with prediction
				c.enhanceProblemWithPrediction(existingProblem, prediction)
			} else {
				// Create new problem based on prediction
				newProblem := c.createProblemFromPrediction(pod, prediction)
				result.Problems = append(result.Problems, newProblem)

				// Update summary counts
				switch newProblem.Severity {
				case types.SeverityWarning:
					result.Summary.WarningPods++
					result.Summary.HealthyPods--
				case types.SeverityCritical:
					result.Summary.CriticalPods++
					result.Summary.HealthyPods--
				}
			}

			// Add quick fix for OOM prediction
			c.addOOMQuickFix(result, pod, prediction)
		}
	}
}

// enhanceProblemWithPrediction adds eBPF prediction to existing problem
func (c *EnhancedChecker) enhanceProblemWithPrediction(problem *types.Problem, prediction *ebpf.OOMPrediction) {
	// Upgrade severity if needed
	if prediction.TimeToOOM < 10*time.Minute && problem.Severity < types.SeverityCritical {
		problem.Severity = types.SeverityCritical
	} else if prediction.TimeToOOM < 30*time.Minute && problem.Severity < types.SeverityWarning {
		problem.Severity = types.SeverityWarning
	}

	// Add prediction info
	problem.Prediction = &types.Prediction{
		TimeToFailure: prediction.TimeToOOM,
		Confidence:    prediction.Confidence,
		Reason:        fmt.Sprintf("Memory growing at %.1f MB/min, will exceed limit", prediction.GrowthRate*60/1024/1024),
	}

	// Update description with eBPF insights
	problem.Description = fmt.Sprintf("%s\neBPF: Memory at %s/%s, growing %.1f MB/min",
		problem.Description,
		humanizeBytes(prediction.CurrentUsage),
		humanizeBytes(prediction.MemoryLimit),
		prediction.GrowthRate*60/1024/1024)
}

// createProblemFromPrediction creates a new problem from eBPF prediction
func (c *EnhancedChecker) createProblemFromPrediction(pod *corev1.Pod, prediction *ebpf.OOMPrediction) types.Problem {
	severity := types.SeverityWarning
	if prediction.TimeToOOM < 10*time.Minute {
		severity = types.SeverityCritical
	}

	return types.Problem{
		Resource: types.ResourceRef{
			Kind:      "pod",
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Severity: severity,
		Title:    fmt.Sprintf("Memory leak detected - OOM in %v", prediction.TimeToOOM.Round(time.Minute)),
		Description: fmt.Sprintf("eBPF detected memory growing at %.1f MB/min. Currently using %s of %s limit",
			prediction.GrowthRate*60/1024/1024,
			humanizeBytes(prediction.CurrentUsage),
			humanizeBytes(prediction.MemoryLimit)),
		Prediction: &types.Prediction{
			TimeToFailure: prediction.TimeToOOM,
			Confidence:    prediction.Confidence,
			Reason:        "Kernel-level memory tracking shows consistent growth pattern",
		},
	}
}

// addOOMQuickFix adds quick fix for OOM prediction
func (c *EnhancedChecker) addOOMQuickFix(result *types.CheckResult, pod *corev1.Pod, prediction *ebpf.OOMPrediction) {
	urgency := types.SeverityWarning
	if prediction.TimeToOOM < 10*time.Minute {
		urgency = types.SeverityCritical
	}

	fix := types.QuickFix{
		Command: fmt.Sprintf("kubectl delete pod %s -n %s --grace-period=30", pod.Name, pod.Namespace),
		Description: fmt.Sprintf("Restart pod before OOM (predicted in %v)", prediction.TimeToOOM.Round(time.Minute)),
		Urgency:     urgency,
		Safe:        false, // Deleting pods is not always safe
	}

	// Check if this fix already exists
	exists := false
	for _, existingFix := range result.QuickFixes {
		if existingFix.Command == fix.Command {
			exists = true
			break
		}
	}

	if !exists {
		result.QuickFixes = append(result.QuickFixes, fix)
	}
}

// humanizeBytes converts bytes to human readable format
func humanizeBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Close cleans up resources
func (c *EnhancedChecker) Close() error {
	if c.ebpfCollector != nil {
		return c.ebpfCollector.Close()
	}
	return nil
}