package health

import (
	"context"
	"fmt"
	"time"

	"github.com/falseyair/tapio/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Checker struct {
	client *k8s.Client
}

func NewChecker(client *k8s.Client) *Checker {
	return &Checker{
		client: client,
	}
}

func (c *Checker) Check(ctx context.Context, namespace string) (*Report, error) {
	report := &Report{
		Timestamp:     time.Now(),
		OverallStatus: StatusHealthy,
		Namespaces:    []NamespaceHealth{},
		Pods:          []PodHealth{},
		Issues:        []Issue{},
	}

	// Get pods
	pods, err := c.getPods(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get pods: %w", err)
	}

	// Analyze pods by namespace
	namespaceMap := make(map[string]*NamespaceHealth)
	
	for _, pod := range pods.Items {
		// Get or create namespace health entry
		nsHealth, exists := namespaceMap[pod.Namespace]
		if !exists {
			nsHealth = &NamespaceHealth{
				Name:        pod.Namespace,
				Status:      StatusHealthy,
				TotalPods:   0,
				HealthyPods: 0,
			}
			namespaceMap[pod.Namespace] = nsHealth
		}
		
		nsHealth.TotalPods++
		report.TotalPods++
		
		// Analyze pod health
		podHealth := c.analyzePod(&pod)
		report.Pods = append(report.Pods, podHealth)
		
		if podHealth.Ready && podHealth.Status == "Running" {
			nsHealth.HealthyPods++
			report.HealthyPods++
		} else {
			// Add issues for unhealthy pods
			if podHealth.Status == "CrashLoopBackOff" || podHealth.Status == "Error" {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityCritical,
					Message:  fmt.Sprintf("Pod %s is in %s state", podHealth.Name, podHealth.Status),
					Resource: fmt.Sprintf("%s/%s", podHealth.Namespace, podHealth.Name),
				})
				nsHealth.Status = StatusCritical
			} else if podHealth.Status == "Pending" || !podHealth.Ready {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("Pod %s is not ready (status: %s)", podHealth.Name, podHealth.Status),
					Resource: fmt.Sprintf("%s/%s", podHealth.Namespace, podHealth.Name),
				})
				if nsHealth.Status == StatusHealthy {
					nsHealth.Status = StatusWarning
				}
			}
			
			// Check restart count
			if podHealth.RestartCount > 5 {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("Pod %s has restarted %d times", podHealth.Name, podHealth.RestartCount),
					Resource: fmt.Sprintf("%s/%s", podHealth.Namespace, podHealth.Name),
				})
			}
		}
	}
	
	// Convert namespace map to slice and determine overall status
	hasWarning := false
	hasCritical := false
	
	for _, nsHealth := range namespaceMap {
		report.Namespaces = append(report.Namespaces, *nsHealth)
		if nsHealth.Status == StatusCritical {
			hasCritical = true
		} else if nsHealth.Status == StatusWarning {
			hasWarning = true
		}
	}
	
	// Set overall status
	if hasCritical {
		report.OverallStatus = StatusCritical
	} else if hasWarning {
		report.OverallStatus = StatusWarning
	} else if report.TotalPods == 0 {
		report.OverallStatus = StatusUnknown
		report.Issues = append(report.Issues, Issue{
			Severity: SeverityInfo,
			Message:  "No pods found in the specified namespace(s)",
			Resource: "",
		})
	}
	
	return report, nil
}

func (c *Checker) getPods(ctx context.Context, namespace string) (*corev1.PodList, error) {
	listOptions := metav1.ListOptions{}
	
	if namespace == "" {
		// All namespaces
		return c.client.Clientset.CoreV1().Pods("").List(ctx, listOptions)
	}
	
	return c.client.Clientset.CoreV1().Pods(namespace).List(ctx, listOptions)
}

func (c *Checker) analyzePod(pod *corev1.Pod) PodHealth {
	health := PodHealth{
		Name:         pod.Name,
		Namespace:    pod.Namespace,
		Status:       string(pod.Status.Phase),
		RestartCount: 0,
		Age:          time.Since(pod.CreationTimestamp.Time),
		Ready:        false,
		Issues:       []string{},
	}
	
	// Check container statuses
	for _, cs := range pod.Status.ContainerStatuses {
		health.RestartCount += cs.RestartCount
		
		if cs.State.Waiting != nil {
			health.Status = cs.State.Waiting.Reason
		} else if cs.State.Terminated != nil {
			health.Status = cs.State.Terminated.Reason
		}
		
		if cs.Ready {
			health.Ready = true
		}
	}
	
	// Check pod conditions
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			health.Ready = true
		}
	}
	
	return health
}