package simple

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/falseyair/tapio/pkg/ebpf"
	"github.com/falseyair/tapio/pkg/k8s"
	"github.com/falseyair/tapio/pkg/types"
)

// Checker performs health checks on Kubernetes resources
type Checker struct {
	client      kubernetes.Interface
	ebpfMonitor ebpf.Monitor
}

// NewChecker creates a new checker with auto-detected Kubernetes config
func NewChecker() (*Checker, error) {
	k8sClient, err := k8s.NewClient("")
	if err != nil {
		return nil, enhanceK8sError(err)
	}

	// Create eBPF monitor with default config (disabled by default)
	ebpfMonitor := ebpf.NewMonitor(nil)

	return &Checker{
		client:      k8sClient.Clientset,
		ebpfMonitor: ebpfMonitor,
	}, nil
}

// NewCheckerWithConfig creates a new checker with custom eBPF configuration
func NewCheckerWithConfig(ebpfConfig *ebpf.Config) (*Checker, error) {
	k8sClient, err := k8s.NewClient("")
	if err != nil {
		return nil, enhanceK8sError(err)
	}

	// Create eBPF monitor with provided config
	ebpfMonitor := ebpf.NewMonitor(ebpfConfig)

	return &Checker{
		client:      k8sClient.Clientset,
		ebpfMonitor: ebpfMonitor,
	}, nil
}

// GetClient returns the Kubernetes client for direct access
func (c *Checker) GetClient() kubernetes.Interface {
	return c.client
}

// GetEBPFMonitor returns the eBPF monitor for direct access
func (c *Checker) GetEBPFMonitor() ebpf.Monitor {
	return c.ebpfMonitor
}

// StartEBPFMonitoring starts eBPF monitoring if available and configured
func (c *Checker) StartEBPFMonitoring(ctx context.Context) error {
	if c.ebpfMonitor == nil {
		return fmt.Errorf("eBPF monitor not initialized")
	}

	if !c.ebpfMonitor.IsAvailable() {
		return fmt.Errorf("eBPF monitoring not available on this system")
	}

	return c.ebpfMonitor.Start(ctx)
}

// StopEBPFMonitoring stops eBPF monitoring
func (c *Checker) StopEBPFMonitoring() error {
	if c.ebpfMonitor == nil {
		return nil
	}

	return c.ebpfMonitor.Stop()
}

// enhanceK8sError provides user-friendly error messages for common K8s issues
func enhanceK8sError(err error) error {
	errStr := err.Error()

	switch {
	case strings.Contains(errStr, "connection refused"):
		return fmt.Errorf("❌ Kubernetes cluster not running\n🔧 Try: minikube start, kind create cluster, or check your cluster status")
	case strings.Contains(errStr, "no such host"):
		return fmt.Errorf("❌ Cannot reach Kubernetes API server\n🔧 Check your kubeconfig and network connectivity")
	case strings.Contains(errStr, "couldn't get current server API group list"):
		return fmt.Errorf("❌ Kubernetes API server unreachable\n🔧 Try: kubectl cluster-info or restart your cluster")
	case strings.Contains(errStr, "The connection to the server"):
		return fmt.Errorf("❌ Kubernetes cluster connection failed\n🔧 Check if your cluster is running: kubectl get nodes")
	case strings.Contains(errStr, "kubeconfig"):
		return fmt.Errorf("❌ No valid kubeconfig found\n🔧 Try: kubectl config view or set KUBECONFIG environment variable")
	default:
		return fmt.Errorf("❌ Kubernetes connection failed: %w\n🔧 Run 'kubectl cluster-info' to check cluster status", err)
	}
}

// Check performs a health check based on the request
func (c *Checker) Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error) {
	namespace := req.Namespace
	if namespace == "" && !req.All {
		namespace = "default" // TODO: Get from current context
	}

	pods, err := c.getPods(ctx, namespace, req.All)
	if err != nil {
		return nil, fmt.Errorf("failed to get pods: %w", err)
	}

	// Filter pods if specific resource requested
	if req.Resource != "" {
		pods = c.filterPods(pods, req.Resource)
	}

	result := &types.CheckResult{
		Timestamp: time.Now(),
	}

	// Handle empty pod list with helpful message
	if len(pods) == 0 {
		result.Problems = append(result.Problems, types.Problem{
			Title:       "No pods found",
			Description: c.getEmptyPodsMessage(namespace, req.All, req.Resource),
			Severity:    types.SeverityWarning,
		})
		return result, nil
	}

	// Analyze each pod
	for i := range pods {
		analysis := c.analyzePod(&pods[i])
		result.Summary.TotalPods++

		switch analysis.Severity {
		case types.SeverityHealthy:
			result.Summary.HealthyPods++
		case types.SeverityWarning:
			result.Summary.WarningPods++
			result.Problems = append(result.Problems, analysis)
		case types.SeverityCritical:
			result.Summary.CriticalPods++
			result.Problems = append(result.Problems, analysis)
		}
	}

	// Generate quick fixes
	result.QuickFixes = c.generateQuickFixes(result.Problems)

	return result, nil
}

// analyzePod performs health analysis on a single pod
func (c *Checker) analyzePod(pod *corev1.Pod) types.Problem {
	problem := types.Problem{
		Resource: types.ResourceRef{
			Kind:      "pod",
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Severity: types.SeverityHealthy,
	}

	// Handle different pod phases intelligently
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// Pod is running - continue with other checks
		break

	case corev1.PodSucceeded:
		// Check if this is a job that completed successfully
		if c.isJob(pod) {
			problem.Severity = types.SeverityHealthy
			problem.Title = "Job completed successfully"
			problem.Description = "This job finished its work and terminated normally"
			return problem
		} else {
			// Regular pod shouldn't be in Succeeded state
			problem.Severity = types.SeverityWarning
			problem.Title = "Pod completed unexpectedly"
			problem.Description = "Regular pods should stay running, not complete"
			return problem
		}

	case corev1.PodFailed:
		problem.Severity = types.SeverityCritical
		problem.Title = "Pod failed"
		problem.Description = c.getPodStatusDescription(pod)
		return problem

	case corev1.PodPending:
		problem.Severity = types.SeverityWarning
		problem.Title = "Pod stuck pending"
		problem.Description = c.getPodStatusDescription(pod)
		return problem

	default:
		problem.Severity = types.SeverityCritical
		problem.Title = fmt.Sprintf("Pod in unexpected phase: %s", pod.Status.Phase)
		problem.Description = c.getPodStatusDescription(pod)
		return problem
	}

	// Check container statuses
	for i := range pod.Status.ContainerStatuses {
		containerStatus := &pod.Status.ContainerStatuses[i]
		if !containerStatus.Ready {
			problem.Severity = types.SeverityWarning
			problem.Title = "Container not ready"
			problem.Description = fmt.Sprintf("Container %s is not ready", containerStatus.Name)
		}

		// Check restart count
		if containerStatus.RestartCount > 3 {
			problem.Severity = types.SeverityWarning
			problem.Title = "High restart count"
			problem.Description = fmt.Sprintf("Container %s has restarted %d times",
				containerStatus.Name, containerStatus.RestartCount)

			// Predict if restarts are frequent
			if containerStatus.RestartCount > 10 {
				problem.Severity = types.SeverityCritical
				problem.Prediction = &types.Prediction{
					TimeToFailure: 15 * time.Minute,
					Confidence:    0.8,
					Reason:        "Frequent restarts indicate unstable container",
				}
			}
		}
	}

	return problem
}

// isJob checks if a pod belongs to a Job or CronJob
func (c *Checker) isJob(pod *corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "Job" || owner.Kind == "CronJob" {
			return true
		}
	}
	return false
}

// getEmptyPodsMessage provides helpful context when no pods are found
func (c *Checker) getEmptyPodsMessage(namespace string, all bool, resource string) string {
	if resource != "" {
		return fmt.Sprintf("No pods match resource '%s'. Try 'kubectl get pods --all-namespaces | grep %s'", resource, resource)
	}

	if all {
		return "No pods found in entire cluster. Try deploying some workloads or check if cluster is empty."
	}

	if namespace == "" {
		namespace = "default"
	}

	return fmt.Sprintf("No pods found in namespace '%s'. Try:\n🔧 kubectl get pods -n %s\n🔧 kubectl get pods --all-namespaces\n🔧 Deploy some workloads to test", namespace, namespace)
}

// Helper functions

func (c *Checker) getPods(ctx context.Context, namespace string, all bool) ([]corev1.Pod, error) {
	listOptions := metav1.ListOptions{}

	if all {
		namespace = ""
	}

	podList, err := c.client.CoreV1().Pods(namespace).List(ctx, listOptions)
	if err != nil {
		return nil, enhanceK8sError(err)
	}

	return podList.Items, nil
}

func (c *Checker) filterPods(pods []corev1.Pod, resource string) []corev1.Pod {
	// Handle pod/name format
	if strings.HasPrefix(resource, "pod/") {
		podName := strings.TrimPrefix(resource, "pod/")
		var filtered []corev1.Pod
		for i := range pods {
			if pods[i].Name == podName {
				filtered = append(filtered, pods[i])
				break
			}
		}
		return filtered
	}

	// Simple filtering by name or labels
	var filtered []corev1.Pod
	for i := range pods {
		pod := &pods[i]
		if pod.Name == resource ||
			pod.Labels["app"] == resource ||
			pod.Labels["app.kubernetes.io/name"] == resource {
			filtered = append(filtered, *pod)
		}
	}
	return filtered
}

func (c *Checker) getPodStatusDescription(pod *corev1.Pod) string {
	switch pod.Status.Phase {
	case corev1.PodPending:
		// Check if it's an unschedulable pod
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
				return fmt.Sprintf("Pod cannot be scheduled: %s", condition.Message)
			}
		}
		return "Pod is waiting to be scheduled or containers are being created"

	case corev1.PodFailed:
		// Try to get specific failure reason
		if pod.Status.Message != "" {
			return fmt.Sprintf("Pod failed: %s", pod.Status.Message)
		}
		return "Pod has terminated with failure"

	case corev1.PodSucceeded:
		if c.isJob(pod) {
			return "Job completed its work successfully"
		}
		return "Pod completed and terminated (unusual for regular pods)"

	default:
		return fmt.Sprintf("Pod is in %s phase", pod.Status.Phase)
	}
}

func (c *Checker) generateQuickFixes(problems []types.Problem) []types.QuickFix {
	var fixes []types.QuickFix

	for _, problem := range problems {
		switch {
		case strings.Contains(problem.Title, "restart count"):
			fixes = append(fixes, types.QuickFix{
				Command:     fmt.Sprintf("kubectl logs %s -n %s --previous", problem.Resource.Name, problem.Resource.Namespace),
				Description: "Check logs for error patterns",
				Urgency:     types.SeverityWarning,
				Safe:        true,
			})
		case problem.Severity == types.SeverityCritical:
			fixes = append(fixes, types.QuickFix{
				Command:     fmt.Sprintf("kubectl describe pod %s -n %s", problem.Resource.Name, problem.Resource.Namespace),
				Description: "Get detailed pod information",
				Urgency:     types.SeverityCritical,
				Safe:        true,
			})
		}
	}

	return fixes
}
