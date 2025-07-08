package simple

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/falseyair/tapio/pkg/types"
)

// Explainer generates detailed explanations of Kubernetes problems
type Explainer struct {
	client  kubernetes.Interface
	checker *Checker
}

// NewExplainer creates a new explainer with auto-detected Kubernetes config
func NewExplainer() (*Explainer, error) {
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w\n\nTIP: Make sure kubectl is configured and working\nTry running: kubectl cluster-info", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w\n\nTIP: Make sure kubectl is configured and working\nTry running: kubectl cluster-info", err)
	}

	checker, err := NewChecker()
	if err != nil {
		return nil, fmt.Errorf("failed to create checker: %w", err)
	}

	return &Explainer{
		client:  client,
		checker: checker,
	}, nil
}

// Explain generates a detailed explanation for a resource
func (e *Explainer) Explain(ctx context.Context, req *types.ExplainRequest) (*types.Explanation, error) {
	// For now, focus on pod explanations
	if req.Resource.Kind != "pod" {
		return nil, fmt.Errorf("explanations for %s not yet supported (try 'pod/name')", req.Resource.Kind)
	}

	namespace := req.Namespace
	if namespace == "" {
		namespace = "default"
	}

	// Get the pod
	pod, err := e.client.CoreV1().Pods(namespace).Get(ctx, req.Resource.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %w", req.Resource.Name, err)
	}

	// Get recent events
	events, err := e.getPodEvents(ctx, pod)
	if err != nil {
		// Don't fail on events error, just log it
		events = []corev1.Event{}
	}

	// Build explanation
	explanation := &types.Explanation{
		Resource: &types.ResourceRef{
			Kind:      "pod",
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Timestamp: time.Now(),
	}

	// Analyze the pod
	e.analyzePodForExplanation(pod, events, explanation, req.Verbose)

	return explanation, nil
}

// analyzePodForExplanation performs deep analysis for explanation
func (e *Explainer) analyzePodForExplanation(pod *corev1.Pod, events []corev1.Event, explanation *types.Explanation, verbose bool) {
	// Build Kubernetes view
	kubernetesView := e.buildKubernetesView(pod, events)
	
	// Build reality check (basic for now, will enhance with eBPF later)
	realityCheck := e.buildRealityCheck(pod, events)
	
	// Find correlations and discrepancies
	correlation := e.buildCorrelation(kubernetesView, realityCheck)

	explanation.Analysis = &types.Analysis{
		KubernetesView: kubernetesView,
		RealityCheck:   realityCheck,
		Correlation:    correlation,
	}

	// Identify root causes
	explanation.RootCauses = e.identifyRootCauses(pod, events)
	
	// Generate solutions
	explanation.Solutions = e.generateSolutions(pod, explanation.RootCauses)
	
	// Create summary
	explanation.Summary = e.createSummary(pod, explanation.RootCauses)
	
	// Add learning content if verbose
	if verbose {
		explanation.Learning = e.generateLearning(pod, explanation.RootCauses)
	}
}

func (e *Explainer) buildKubernetesView(pod *corev1.Pod, events []corev1.Event) *types.KubernetesView {
	view := &types.KubernetesView{
		Status:    string(pod.Status.Phase),
		Phase:     string(pod.Status.Phase),
		Resources: make(map[string]string),
	}

	// Get resource limits and requests
	for _, container := range pod.Spec.Containers {
		if container.Resources.Limits != nil {
			if memLimit := container.Resources.Limits.Memory(); memLimit != nil {
				view.Resources["memory_limit"] = memLimit.String()
			}
			if cpuLimit := container.Resources.Limits.Cpu(); cpuLimit != nil {
				view.Resources["cpu_limit"] = cpuLimit.String()
			}
		}
		if container.Resources.Requests != nil {
			if memRequest := container.Resources.Requests.Memory(); memRequest != nil {
				view.Resources["memory_request"] = memRequest.String()
			}
		}
	}

	// Get conditions
	for _, condition := range pod.Status.Conditions {
		view.Conditions = append(view.Conditions, 
			fmt.Sprintf("%s: %s", condition.Type, condition.Status))
	}

	// Get recent events
	for _, event := range events {
		if event.Type == "Warning" || event.Type == "Error" {
			view.Events = append(view.Events, 
				fmt.Sprintf("%s: %s", event.Reason, event.Message))
		}
	}

	return view
}

func (e *Explainer) buildRealityCheck(pod *corev1.Pod, events []corev1.Event) *types.RealityCheck {
	reality := &types.RealityCheck{}

	// Check restart patterns
	restartCount := 0
	for _, containerStatus := range pod.Status.ContainerStatuses {
		restartCount += int(containerStatus.RestartCount)
	}

	if restartCount > 0 {
		reality.RestartPattern = fmt.Sprintf("Container restarted %d times", restartCount)
	}

	// Extract error patterns from events
	for _, event := range events {
		if strings.Contains(event.Message, "OOMKilled") {
			reality.ErrorPatterns = append(reality.ErrorPatterns, "Out of Memory (OOM) kills detected")
		}
		if strings.Contains(event.Message, "ImagePullBackOff") {
			reality.ErrorPatterns = append(reality.ErrorPatterns, "Cannot pull container image")
		}
		if strings.Contains(event.Message, "CrashLoopBackOff") {
			reality.ErrorPatterns = append(reality.ErrorPatterns, "Container keeps crashing on startup")
		}
		if strings.Contains(event.Message, "ErrImagePull") {
			reality.ErrorPatterns = append(reality.ErrorPatterns, "Failed to pull container image")
		}
		if strings.Contains(event.Message, "FailedScheduling") {
			reality.ErrorPatterns = append(reality.ErrorPatterns, "Pod cannot be scheduled to any node")
		}
	}

	return reality
}

func (e *Explainer) buildCorrelation(k8sView *types.KubernetesView, reality *types.RealityCheck) *types.Correlation {
	correlation := &types.Correlation{}

	// Find discrepancies
	if k8sView.Status == "Running" && len(reality.ErrorPatterns) > 0 {
		correlation.Discrepancies = append(correlation.Discrepancies,
			"Kubernetes reports pod as running, but errors detected in events")
	}

	if reality.RestartPattern != "" {
		correlation.Patterns = append(correlation.Patterns,
			"High restart count indicates instability")
	}

	// Check for resource pressure patterns
	if k8sView.Resources["memory_limit"] != "" && 
	   contains(reality.ErrorPatterns, "Out of Memory") {
		correlation.Patterns = append(correlation.Patterns,
			"Memory usage exceeding limits causes OOM kills")
	}

	return correlation
}

func (e *Explainer) identifyRootCauses(pod *corev1.Pod, events []corev1.Event) []types.RootCause {
	var causes []types.RootCause

	// Check for OOM issues
	for _, event := range events {
		if strings.Contains(event.Message, "OOMKilled") {
			causes = append(causes, types.RootCause{
				Title:       "Memory limit too low",
				Description: "The container is being killed because it exceeds its memory limit",
				Evidence: []string{
					"OOMKilled events in pod history",
					fmt.Sprintf("Memory limit: %s", e.getMemoryLimit(pod)),
				},
				Confidence: 0.9,
			})
		}
	}

	// Check for image issues
	for _, event := range events {
		if strings.Contains(event.Message, "ImagePullBackOff") || 
		   strings.Contains(event.Message, "ErrImagePull") {
			causes = append(causes, types.RootCause{
				Title:       "Container image not found",
				Description: "Kubernetes cannot pull the specified container image",
				Evidence: []string{
					"ImagePullBackOff events",
					"Image pull failures in pod events",
				},
				Confidence: 0.95,
			})
		}
	}

	// Check for scheduling issues
	for _, event := range events {
		if strings.Contains(event.Message, "FailedScheduling") {
			causes = append(causes, types.RootCause{
				Title:       "Pod cannot be scheduled",
				Description: "No nodes available that meet the pod's requirements",
				Evidence: []string{
					"FailedScheduling events",
					event.Message,
				},
				Confidence: 0.9,
			})
		}
	}

	// Check for crash loop
	for _, event := range events {
		if strings.Contains(event.Message, "CrashLoopBackOff") {
			causes = append(causes, types.RootCause{
				Title:       "Application keeps crashing",
				Description: "The container starts but immediately crashes, creating a restart loop",
				Evidence: []string{
					"CrashLoopBackOff events",
					fmt.Sprintf("Restart count: %d", e.getTotalRestarts(pod)),
				},
				Confidence: 0.85,
			})
		}
	}

	// If no specific causes found, provide general guidance
	if len(causes) == 0 && pod.Status.Phase != corev1.PodRunning {
		causes = append(causes, types.RootCause{
			Title:       "Pod not in running state",
			Description: fmt.Sprintf("Pod is in %s phase, which indicates an issue", pod.Status.Phase),
			Evidence: []string{
				fmt.Sprintf("Pod phase: %s", pod.Status.Phase),
			},
			Confidence: 0.7,
		})
	}

	return causes
}

func (e *Explainer) generateSolutions(pod *corev1.Pod, causes []types.RootCause) []types.Solution {
	var solutions []types.Solution

	for _, cause := range causes {
		switch {
		case strings.Contains(cause.Title, "Memory limit"):
			solutions = append(solutions, types.Solution{
				Title:       "Increase memory limit",
				Description: "Allocate more memory to prevent OOM kills",
				Commands: []string{
					fmt.Sprintf("kubectl patch pod %s -p '{\"spec\":{\"containers\":[{\"name\":\"%s\",\"resources\":{\"limits\":{\"memory\":\"1Gi\"}}}]}}'", 
						pod.Name, e.getFirstContainerName(pod)),
					"# Or edit the deployment to make permanent changes",
					fmt.Sprintf("kubectl edit deployment %s", e.getDeploymentName(pod)),
				},
				Urgency:    types.SeverityCritical,
				Difficulty: "easy",
				Risk:       "low",
			})

		case strings.Contains(cause.Title, "image not found"):
			solutions = append(solutions, types.Solution{
				Title:       "Fix image reference",
				Description: "Correct the image name or ensure it exists in the registry",
				Commands: []string{
					fmt.Sprintf("kubectl describe pod %s", pod.Name),
					"# Check the image name in the pod spec",
					"# Verify the image exists in your container registry",
					"# Example: docker pull <image-name>",
				},
				Urgency:    types.SeverityCritical,
				Difficulty: "medium",
				Risk:       "low",
			})

		case strings.Contains(cause.Title, "cannot be scheduled"):
			solutions = append(solutions, types.Solution{
				Title:       "Fix scheduling constraints",
				Description: "Ensure nodes are available that meet pod requirements",
				Commands: []string{
					"kubectl get nodes",
					fmt.Sprintf("kubectl describe pod %s", pod.Name),
					"# Check node selectors, taints, and resource requirements",
					"# Scale up cluster if no capacity available",
				},
				Urgency:    types.SeverityCritical,
				Difficulty: "medium",
				Risk:       "medium",
			})

		case strings.Contains(cause.Title, "keeps crashing"):
			solutions = append(solutions, types.Solution{
				Title:       "Debug application startup",
				Description: "Investigate why the application crashes on startup",
				Commands: []string{
					fmt.Sprintf("kubectl logs %s", pod.Name),
					fmt.Sprintf("kubectl logs %s --previous", pod.Name),
					"# Check application configuration and dependencies",
					"# Verify environment variables and secrets",
				},
				Urgency:    types.SeverityCritical,
				Difficulty: "hard",
				Risk:       "low",
			})
		}
	}

	// Always provide debug commands
	solutions = append(solutions, types.Solution{
		Title:       "Get more information",
		Description: "Gather additional debugging information",
		Commands: []string{
			fmt.Sprintf("kubectl describe pod %s", pod.Name),
			fmt.Sprintf("kubectl logs %s", pod.Name),
			fmt.Sprintf("kubectl get events --field-selector involvedObject.name=%s", pod.Name),
		},
		Urgency:    types.SeverityWarning,
		Difficulty: "easy",
		Risk:       "low",
	})

	return solutions
}

func (e *Explainer) createSummary(pod *corev1.Pod, causes []types.RootCause) string {
	if len(causes) == 0 {
		return fmt.Sprintf("Pod %s appears to be healthy", pod.Name)
	}

	primaryCause := causes[0] // Highest confidence cause
	return fmt.Sprintf("Pod %s: %s", pod.Name, primaryCause.Title)
}

func (e *Explainer) generateLearning(pod *corev1.Pod, causes []types.RootCause) *types.Learning {
	if len(causes) == 0 {
		return nil
	}

	primaryCause := causes[0]
	
	learning := &types.Learning{}

	switch {
	case strings.Contains(primaryCause.Title, "Memory limit"):
		learning.ConceptExplanation = "Memory limits in Kubernetes prevent containers from using too much RAM. When a container exceeds its limit, Kubernetes kills it (OOMKill)."
		learning.WhyItMatters = "Proper memory limits prevent one container from affecting others and help with cluster resource planning."
		learning.CommonMistakes = []string{
			"Setting memory limits too low for the application's needs",
			"Not monitoring actual memory usage patterns",
			"Forgetting to account for memory spikes during startup",
		}
		learning.BestPractices = []string{
			"Monitor actual memory usage before setting limits",
			"Set requests lower than limits to allow for bursts",
			"Use horizontal pod autoscaling for variable workloads",
		}

	case strings.Contains(primaryCause.Title, "image"):
		learning.ConceptExplanation = "Container images are the packaged applications that run in pods. Kubernetes needs to pull these images from a registry before starting containers."
		learning.WhyItMatters = "Correct image references are essential for pod startup. Image pull failures prevent applications from running."
		learning.CommonMistakes = []string{
			"Typos in image names or tags",
			"Using images from private registries without proper authentication",
			"Referencing images that don't exist or were deleted",
		}
		learning.BestPractices = []string{
			"Use specific image tags instead of 'latest'",
			"Test image pulls manually before deploying",
			"Set up proper registry authentication",
		}

	case strings.Contains(primaryCause.Title, "scheduling"):
		learning.ConceptExplanation = "Pod scheduling is how Kubernetes decides which node should run a pod. Scheduling can fail if no nodes meet the pod's requirements."
		learning.WhyItMatters = "Successful scheduling is required for pods to start. Scheduling failures leave pods in Pending state."
		learning.CommonMistakes = []string{
			"Requesting more resources than available on any node",
			"Using node selectors that don't match any nodes",
			"Not accounting for node taints and tolerations",
		}
		learning.BestPractices = []string{
			"Monitor cluster capacity and scale proactively",
			"Use resource requests appropriately",
			"Understand node affinity and anti-affinity rules",
		}

	case strings.Contains(primaryCause.Title, "crashing"):
		learning.ConceptExplanation = "CrashLoopBackOff occurs when a container repeatedly fails to start successfully. Kubernetes will restart it with increasing delays."
		learning.WhyItMatters = "Crash loops indicate fundamental application or configuration issues that prevent normal operation."
		learning.CommonMistakes = []string{
			"Missing required environment variables or secrets",
			"Application trying to connect to unavailable dependencies",
			"Incorrect container entrypoint or command",
		}
		learning.BestPractices = []string{
			"Implement proper health checks and readiness probes",
			"Ensure all dependencies are available before startup",
			"Use init containers for setup tasks",
		}
	}

	return learning
}

// Helper functions
func (e *Explainer) getPodEvents(ctx context.Context, pod *corev1.Pod) ([]corev1.Event, error) {
	events, err := e.client.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", pod.Name),
	})
	if err != nil {
		return nil, err
	}
	return events.Items, nil
}

func (e *Explainer) getMemoryLimit(pod *corev1.Pod) string {
	for _, container := range pod.Spec.Containers {
		if limit := container.Resources.Limits.Memory(); limit != nil {
			return limit.String()
		}
	}
	return "not set"
}

func (e *Explainer) getFirstContainerName(pod *corev1.Pod) string {
	if len(pod.Spec.Containers) > 0 {
		return pod.Spec.Containers[0].Name
	}
	return "app"
}

func (e *Explainer) getDeploymentName(pod *corev1.Pod) string {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			// Extract deployment name from ReplicaSet name
			// Format is usually: deployment-name-xyz
			name := owner.Name
			if idx := strings.LastIndex(name, "-"); idx > 0 {
				return name[:idx]
			}
		}
	}
	return "unknown"
}

func (e *Explainer) getTotalRestarts(pod *corev1.Pod) int {
	total := 0
	for _, containerStatus := range pod.Status.ContainerStatuses {
		total += int(containerStatus.RestartCount)
	}
	return total
}

// contains checks if a slice contains a substring
func contains(slice []string, substr string) bool {
	for _, item := range slice {
		if strings.Contains(item, substr) {
			return true
		}
	}
	return false
}