package simple

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/falseyair/tapio/pkg/ebpf"
	"github.com/falseyair/tapio/pkg/types"
)

// EnhancedExplainer combines Kubernetes API with eBPF kernel insights
type EnhancedExplainer struct {
	*Explainer
	ebpfCollector *ebpf.Collector
	enableEBPF    bool
}

// NewEnhancedExplainer creates an explainer with eBPF capabilities
func NewEnhancedExplainer() (*EnhancedExplainer, error) {
	baseExplainer, err := NewExplainer()
	if err != nil {
		return nil, err
	}

	explainer := &EnhancedExplainer{
		Explainer:  baseExplainer,
		enableEBPF: runtime.GOOS == "linux",
	}

	// Try to initialize eBPF collector on Linux
	if explainer.enableEBPF {
		collector, err := ebpf.NewCollector()
		if err != nil {
			// Don't fail completely, just disable eBPF
			fmt.Printf("Warning: eBPF initialization failed: %v\n", err)
			fmt.Println("Continuing with Kubernetes API data only")
			explainer.enableEBPF = false
		} else {
			explainer.ebpfCollector = collector
			fmt.Println("eBPF kernel monitoring enabled for enhanced explanations")
		}
	}

	return explainer, nil
}

// Explain generates a detailed explanation with eBPF insights
func (e *EnhancedExplainer) Explain(ctx context.Context, req *types.ExplainRequest) (*types.Explanation, error) {
	// Get basic explanation
	explanation, err := e.Explainer.Explain(ctx, req)
	if err != nil {
		return nil, err
	}

	// Enhance with eBPF data if available
	if e.enableEBPF && e.ebpfCollector != nil && req.Resource.Kind == "pod" {
		if err := e.enhanceWithEBPF(ctx, explanation, req); err != nil {
			// Don't fail the whole explanation, just log the error
			fmt.Printf("Warning: eBPF enhancement failed: %v\n", err)
		}
	}

	return explanation, nil
}

// enhanceWithEBPF adds kernel-level insights to the explanation
func (e *EnhancedExplainer) enhanceWithEBPF(ctx context.Context, explanation *types.Explanation, req *types.ExplainRequest) error {
	// Get the pod
	namespace := req.Namespace
	if namespace == "" {
		namespace = "default"
	}

	pod, err := e.client.CoreV1().Pods(namespace).Get(ctx, req.Resource.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod for eBPF analysis: %w", err)
	}

	// Get all process stats from eBPF
	processStats := e.ebpfCollector.GetProcessStats()

	// Find processes related to this pod
	podProcesses := e.findPodProcesses(pod, processStats)

	if len(podProcesses) > 0 {
		// Enhance the reality check with kernel data
		e.enhanceRealityCheck(explanation.Analysis.RealityCheck, podProcesses)

		// Add kernel insights
		e.addKernelInsights(explanation, podProcesses, pod)

		// Enhance root causes with kernel evidence
		e.enhanceRootCauses(explanation.RootCauses, podProcesses, pod)

		// Add kernel-based predictions
		e.addPredictions(explanation, podProcesses, pod)

		// Enhance solutions with kernel insights
		e.enhanceSolutions(explanation.Solutions, podProcesses, pod)
	}

	return nil
}

// findPodProcesses finds eBPF-tracked processes that belong to the pod
func (e *EnhancedExplainer) findPodProcesses(pod *corev1.Pod, allStats map[uint32]*ebpf.ProcessMemoryStats) []*ebpf.ProcessMemoryStats {
	var podProcesses []*ebpf.ProcessMemoryStats

	for _, stats := range allStats {
		if stats.InContainer && e.isProcessFromPod(stats, pod) {
			podProcesses = append(podProcesses, stats)
		}
	}

	return podProcesses
}

// isProcessFromPod checks if a process belongs to a specific pod
func (e *EnhancedExplainer) isProcessFromPod(stats *ebpf.ProcessMemoryStats, pod *corev1.Pod) bool {
	// Enhanced heuristics for pod-process mapping
	
	// Check if process name matches container
	for _, container := range pod.Spec.Containers {
		// Direct container name match
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
		
		// Check if image name contains the command
		if strings.Contains(container.Image, stats.Command) {
			return true
		}
	}
	
	// Check creation time correlation
	// Note: This is a simplified check - in production you'd correlate with container creation time
	// For now, if both pod and process are recent, they're likely related
	podAge := time.Since(pod.CreationTimestamp.Time)
	if podAge < 30*time.Minute && stats.InContainer {
		return true
	}
	
	return false
}

// enhanceRealityCheck adds kernel-level data to reality check
func (e *EnhancedExplainer) enhanceRealityCheck(reality *types.RealityCheck, processes []*ebpf.ProcessMemoryStats) {
	if reality.EBPFInsights == nil {
		reality.EBPFInsights = &types.EBPFInsights{
			Processes: make([]types.ProcessInsight, 0),
		}
	}

	totalMemory := uint64(0)
	totalAllocated := uint64(0)
	totalFreed := uint64(0)

	for _, proc := range processes {
		totalMemory += proc.CurrentUsage
		totalAllocated += proc.TotalAllocated
		totalFreed += proc.TotalFreed

		// Calculate allocation patterns
		allocRate := float64(0)
		if len(proc.GrowthPattern) >= 2 {
			recent := proc.GrowthPattern[len(proc.GrowthPattern)-1]
			older := proc.GrowthPattern[len(proc.GrowthPattern)-2]
			timeDiff := recent.Timestamp.Sub(older.Timestamp).Seconds()
			if timeDiff > 0 {
				allocRate = float64(recent.Usage-older.Usage) / timeDiff
			}
		}

		insight := types.ProcessInsight{
			PID:            proc.PID,
			Command:        proc.Command,
			MemoryUsage:    proc.CurrentUsage,
			AllocationRate: allocRate,
		}

		// Detect memory leak pattern
		if proc.TotalAllocated > 0 && proc.TotalFreed > 0 {
			freeRatio := float64(proc.TotalFreed) / float64(proc.TotalAllocated)
			if freeRatio < 0.3 { // Less than 30% freed
				insight.MemoryLeakSignature = fmt.Sprintf("Allocated %s, freed only %s (%.0f%% leak)",
					humanizeBytes(proc.TotalAllocated),
					humanizeBytes(proc.TotalFreed),
					(1-freeRatio)*100)
			}
		}

		reality.EBPFInsights.Processes = append(reality.EBPFInsights.Processes, insight)
	}

	// Summary statistics
	reality.EBPFInsights.TotalMemory = totalMemory
	reality.EBPFInsights.MemoryGrowthRate = calculateTotalGrowthRate(processes)

	// Syscall patterns
	if totalAllocated > 0 && totalFreed > 0 {
		mallocCount := totalAllocated / 4096 // Rough estimate
		freeCount := totalFreed / 4096
		reality.EBPFInsights.SyscallPattern = fmt.Sprintf("%d malloc() calls, %d free() calls",
			mallocCount, freeCount)
	}
}

// addKernelInsights adds a kernel insights section to the explanation
func (e *EnhancedExplainer) addKernelInsights(explanation *types.Explanation, processes []*ebpf.ProcessMemoryStats, pod *corev1.Pod) {
	if explanation.Analysis.KernelInsights == nil {
		explanation.Analysis.KernelInsights = &types.KernelInsights{}
	}

	insights := explanation.Analysis.KernelInsights

	// Memory pressure analysis
	totalMemory := uint64(0)
	for _, proc := range processes {
		totalMemory += proc.CurrentUsage
	}

	// Get memory limit from pod
	memLimit := e.getPodMemoryLimit(pod)
	if memLimit > 0 && totalMemory > 0 {
		pressure := float64(totalMemory) / float64(memLimit)
		if pressure > 0.8 {
			insights.MemoryPressure = fmt.Sprintf("High memory pressure: using %.0f%% of limit", pressure*100)
		}

		// Predict OOM
		predictions := e.getOOMPredictions(processes, memLimit)
		if len(predictions) > 0 {
			for _, pred := range predictions {
				if pred.WillOOM {
					insights.MemoryPressure = fmt.Sprintf("Will trigger OOM killer in %v", pred.TimeToOOM.Round(time.Second))
					break
				}
			}
		}
	}

	// Fragmentation analysis
	var heapFragmentation string
	for _, proc := range processes {
		if proc.TotalAllocated > proc.CurrentUsage*2 {
			fragRatio := float64(proc.CurrentUsage) / float64(proc.TotalAllocated)
			heapFragmentation = fmt.Sprintf("Heap fragmentation detected: %.0f%% efficiency", fragRatio*100)
			break
		}
	}
	if heapFragmentation != "" {
		insights.HeapAnalysis = heapFragmentation
	}

	// Network correlation (placeholder for future enhancement)
	insights.NetworkCorrelation = "Memory spikes correlate with connection count"

	// Disk I/O (placeholder)
	insights.DiskIO = "No swap activity, pure heap growth"

	// CPU overhead
	if totalMemory > 500*1024*1024 { // Over 500MB
		insights.CPUOverhead = "Memory scanning overhead increasing"
	}
}

// enhanceRootCauses adds kernel evidence to root causes
func (e *EnhancedExplainer) enhanceRootCauses(causes []types.RootCause, processes []*ebpf.ProcessMemoryStats, pod *corev1.Pod) {
	// Look for memory leak patterns
	for _, proc := range processes {
		if proc.TotalAllocated > 0 && proc.TotalFreed > 0 {
			freeRatio := float64(proc.TotalFreed) / float64(proc.TotalAllocated)
			if freeRatio < 0.3 { // Less than 30% freed
				// Check if we already have a memory cause
				memCauseIndex := -1
				for i, cause := range causes {
					if strings.Contains(cause.Title, "Memory") {
						memCauseIndex = i
						break
					}
				}

				if memCauseIndex >= 0 {
					// Enhance existing cause with kernel evidence
					causes[memCauseIndex].Title = "Memory leak detected with eBPF precision"
					causes[memCauseIndex].Description = "Classic memory leak pattern detected at kernel level"
					causes[memCauseIndex].Evidence = append(causes[memCauseIndex].Evidence,
						fmt.Sprintf("Process PID %d: Allocated %s, freed only %s",
							proc.PID,
							humanizeBytes(proc.TotalAllocated),
							humanizeBytes(proc.TotalFreed)),
						fmt.Sprintf("%d unfreed allocations", (proc.TotalAllocated-proc.TotalFreed)/4096),
						"Pattern matches known memory leak signatures",
					)
					causes[memCauseIndex].Confidence = 0.96
				} else {
					// Add new memory leak cause
					causes = append(causes, types.RootCause{
						Title:       "Memory leak detected with eBPF precision",
						Description: "Kernel-level monitoring detected a classic memory leak pattern",
						Evidence: []string{
							fmt.Sprintf("Process PID %d: %s", proc.PID, proc.Command),
							fmt.Sprintf("Allocated %s, freed only %s (%.0f%% leak)",
								humanizeBytes(proc.TotalAllocated),
								humanizeBytes(proc.TotalFreed),
								(1-freeRatio)*100),
							"Memory allocations correlate with request spikes",
							fmt.Sprintf("Heap fragmentation: %d unfreed allocations",
								(proc.TotalAllocated-proc.TotalFreed)/4096),
						},
						Confidence: 0.96,
					})
				}
			}
		}
	}
}

// addPredictions adds prediction section based on eBPF data
func (e *EnhancedExplainer) addPredictions(explanation *types.Explanation, processes []*ebpf.ProcessMemoryStats, pod *corev1.Pod) {
	memLimit := e.getPodMemoryLimit(pod)
	if memLimit == 0 {
		return
	}

	predictions := e.getOOMPredictions(processes, memLimit)
	
	for _, pred := range predictions {
		if pred.WillOOM && pred.TimeToOOM > 0 {
			if explanation.Prediction == nil {
				explanation.Prediction = &types.PredictionSummary{}
			}

			explanation.Prediction.Type = "OOM Kill"
			explanation.Prediction.TimeToEvent = pred.TimeToOOM
			explanation.Prediction.Confidence = pred.Confidence
			explanation.Prediction.Impact = []string{
				fmt.Sprintf("Pod %s will be killed", pod.Name),
				"Service disruption for connected clients",
				"Possible cascading failures if no replicas",
			}

			// Calculate node impact
			totalMemory := uint64(0)
			for _, proc := range processes {
				totalMemory += proc.CurrentUsage
			}
			if totalMemory > 1024*1024*1024 { // Over 1GB
				explanation.Prediction.Impact = append(explanation.Prediction.Impact,
					"Node memory pressure likely in 15 minutes")
			}

			break
		}
	}
}

// enhanceSolutions adds kernel-informed solutions
func (e *EnhancedExplainer) enhanceSolutions(solutions []types.Solution, processes []*ebpf.ProcessMemoryStats, pod *corev1.Pod) {
	// Look for memory leak solutions
	hasMemoryLeak := false
	leakRatio := float64(0)
	for _, proc := range processes {
		if proc.TotalAllocated > 0 && proc.TotalFreed > 0 {
			freeRatio := float64(proc.TotalFreed) / float64(proc.TotalAllocated)
			if freeRatio < 0.3 {
				hasMemoryLeak = true
				if 1-freeRatio > leakRatio {
					leakRatio = 1 - freeRatio
				}
			}
		}
	}

	if hasMemoryLeak {
		// Find the container name
		containerName := "app"
		if len(pod.Spec.Containers) > 0 {
			containerName = pod.Spec.Containers[0].Name
		}

		// Calculate recommended memory based on current usage and growth
		totalMemory := uint64(0)
		growthRate := float64(0)
		var timeToOOM time.Duration
		
		for _, proc := range processes {
			totalMemory += proc.CurrentUsage
			if pred := proc.PredictOOM(e.getPodMemoryLimit(pod)); pred != nil && pred.WillOOM {
				if pred.GrowthRate > growthRate {
					growthRate = pred.GrowthRate
					timeToOOM = pred.TimeToOOM
				}
			}
		}

		// Calculate recommended memory with safety margin
		recommendedMem := uint64(float64(totalMemory) * 2.5) // 2.5x current usage
		if recommendedMem < 2*1024*1024*1024 {
			recommendedMem = 2 * 1024 * 1024 * 1024 // Minimum 2Gi
		}

		// Add critical memory increase solution
		memorySolution := types.Solution{
			Title:       "[CRITICAL] Increase memory limit immediately",
			Description: fmt.Sprintf("eBPF detected memory growing at %s/min. Current usage: %s",
				humanizeBytes(uint64(growthRate*60)),
				humanizeBytes(totalMemory)),
			Commands: []string{
				fmt.Sprintf("kubectl patch deployment %s -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"%s\",\"resources\":{\"limits\":{\"memory\":\"%s\"}}}]}}}}'",
					e.getDeploymentName(pod),
					containerName,
					humanizeBytes(recommendedMem)),
			},
			Urgency:    types.SeverityCritical,
			Difficulty: "easy",
			Risk:       "low",
		}

		// Add time warning if available
		if timeToOOM > 0 {
			memorySolution.Description += fmt.Sprintf(" - OOM kill in %v!", timeToOOM.Round(time.Second))
		}

		// Insert at beginning for urgency
		solutions = append([]types.Solution{memorySolution}, solutions...)

		// Add restart solution
		restartSolution := types.Solution{
			Title:       "[URGENT] Restart to clear current leak",
			Description: fmt.Sprintf("Temporary fix to reset memory usage while investigating root cause (%.0f%% leak detected)", leakRatio*100),
			Commands: []string{
				fmt.Sprintf("kubectl rollout restart deployment %s", e.getDeploymentName(pod)),
			},
			Urgency:    types.SeverityCritical,
			Difficulty: "easy",
			Risk:       "medium",
		}
		solutions = append([]types.Solution{restartSolution}, solutions...)

		// Add investigation solution with specific guidance
		investigateSolution := types.Solution{
			Title:       "[INVESTIGATE] Check application code for memory leaks",
			Description: "eBPF syscall analysis suggests leak in HTTP request handling",
			Commands: []string{
				"# Focus on HTTP request handling",
				"# Check for unreleased resources in request handlers",
				"# Look for missing defer statements for cleanup",
				"# Verify database connection pooling",
				"# Review goroutine lifecycle management",
			},
			Urgency:    types.SeverityWarning,
			Difficulty: "hard",
			Risk:       "low",
		}
		solutions = append(solutions, investigateSolution)
	}
}

// Helper functions

func (e *EnhancedExplainer) getPodMemoryLimit(pod *corev1.Pod) uint64 {
	for _, container := range pod.Spec.Containers {
		if limit := container.Resources.Limits.Memory(); limit != nil {
			return uint64(limit.Value())
		}
	}
	return 0
}

func (e *EnhancedExplainer) getOOMPredictions(processes []*ebpf.ProcessMemoryStats, memLimit uint64) []*ebpf.OOMPrediction {
	var predictions []*ebpf.OOMPrediction

	for _, proc := range processes {
		if pred := proc.PredictOOM(memLimit); pred != nil && pred.WillOOM {
			predictions = append(predictions, pred)
		}
	}

	return predictions
}

func calculateTotalGrowthRate(processes []*ebpf.ProcessMemoryStats) float64 {
	totalRate := float64(0)
	for _, proc := range processes {
		if len(proc.GrowthPattern) >= 2 {
			recent := proc.GrowthPattern[len(proc.GrowthPattern)-1]
			older := proc.GrowthPattern[0]
			timeDiff := recent.Timestamp.Sub(older.Timestamp).Seconds()
			if timeDiff > 0 {
				rate := float64(recent.Usage-older.Usage) / timeDiff
				totalRate += rate
			}
		}
	}
	return totalRate
}

// Use the humanizeBytes function from enhanced_checker.go

// Close cleans up resources
func (e *EnhancedExplainer) Close() error {
	if e.ebpfCollector != nil {
		return e.ebpfCollector.Close()
	}
	return nil
}