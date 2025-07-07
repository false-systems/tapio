package simple

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/falseyair/tapio/pkg/types"
)

// Checker performs health checks on Kubernetes resources
type Checker struct {
	client kubernetes.Interface
}

// NewChecker creates a new checker with auto-detected Kubernetes config
func NewChecker() (*Checker, error) {
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Checker{client: client}, nil
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

	// Check if pod is running
	if pod.Status.Phase != corev1.PodRunning {
		problem.Severity = types.SeverityCritical
		problem.Title = fmt.Sprintf("Pod not running (phase: %s)", pod.Status.Phase)
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

// Helper functions
func getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}

	// Fall back to kubeconfig file
	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

func (c *Checker) getPods(ctx context.Context, namespace string, all bool) ([]corev1.Pod, error) {
	listOptions := metav1.ListOptions{}

	if all {
		namespace = ""
	}

	podList, err := c.client.CoreV1().Pods(namespace).List(ctx, listOptions)
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}

func (c *Checker) filterPods(pods []corev1.Pod, resource string) []corev1.Pod {
	// Handle pod/name format
	if strings.HasPrefix(resource, "pod/") {
		podName := strings.TrimPrefix(resource, "pod/")
		var filtered []corev1.Pod
		for _, pod := range pods {
			if pod.Name == podName {
				filtered = append(filtered, pod)
				break
			}
		}
		return filtered
	}

	// Simple filtering by name or labels
	var filtered []corev1.Pod
	for _, pod := range pods {
		if pod.Name == resource ||
			pod.Labels["app"] == resource ||
			pod.Labels["app.kubernetes.io/name"] == resource {
			filtered = append(filtered, pod)
		}
	}
	return filtered
}

func (c *Checker) getPodStatusDescription(pod *corev1.Pod) string {
	switch pod.Status.Phase {
	case corev1.PodPending:
		return "Pod is waiting to be scheduled or containers are being created"
	case corev1.PodFailed:
		return "Pod has terminated with failure"
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