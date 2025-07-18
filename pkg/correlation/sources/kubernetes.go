package sources

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/domain"
)

// KubernetesDataSource implements DataSource interface for Kubernetes data
type KubernetesDataSource struct {
	checker *simple.Checker
}

// NewKubernetesDataSource creates a new Kubernetes data source
func NewKubernetesDataSource(checker *simple.Checker) *KubernetesDataSource {
	return &KubernetesDataSource{
		checker: checker,
	}
}

// GetType returns the source type
func (k *KubernetesDataSource) GetType() correlation.SourceType {
	return correlation.SourceKubernetes
}

// IsAvailable checks if the Kubernetes API is available
func (k *KubernetesDataSource) IsAvailable() bool {
	// Check if we can access the API
	client := k.checker.GetClient()
	if client == nil {
		return false
	}

	// Try a simple API call
	ctx := context.Background()
	req := &types.CheckRequest{
		Namespace: "default",
		All:       false,
	}

	_, err := k.checker.Check(ctx, req)
	return err == nil
}

// GetData retrieves data of the specified type
func (k *KubernetesDataSource) GetData(ctx context.Context, dataType string, params map[string]interface{}) (interface{}, error) {
	switch dataType {
	case "kubernetes_data":
		return k.getKubernetesData(ctx, params)
	case "pods":
		return k.getPods(ctx, params)
	case "events":
		return k.getEvents(ctx, params)
	case "problems":
		return k.getProblems(ctx, params)
	default:
		return nil, fmt.Errorf("unsupported data type: %s", dataType)
	}
}

// getKubernetesData retrieves comprehensive Kubernetes data
func (k *KubernetesDataSource) getKubernetesData(ctx context.Context, params map[string]interface{}) (*correlation.KubernetesData, error) {
	namespace := ""
	all := true

	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
		all = false
	}

	// Get health check data
	req := &types.CheckRequest{
		Namespace: namespace,
		All:       all,
		Verbose:   true,
	}

	result, err := k.checker.Check(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to check health: %w", err)
	}

	// Get pods
	pods, err := k.checker.GetPods(ctx, namespace, all)
	if err != nil {
		return nil, fmt.Errorf("failed to get pods: %w", err)
	}

	// Get events from pods
	var events []corev1.Event
	for _, pod := range pods {
		podEvents, err := k.getEventsForPod(ctx, &pod)
		if err == nil {
			events = append(events, podEvents...)
		}
	}

	return &correlation.KubernetesData{
		Pods:      pods,
		Events:    events,
		Problems:  result.Problems,
		Metrics:   make(map[string]interface{}), // TODO: Add metrics if available
		Timestamp: result.Timestamp,
	}, nil
}

// getPods retrieves pods
func (k *KubernetesDataSource) getPods(ctx context.Context, params map[string]interface{}) ([]corev1.Pod, error) {
	namespace := ""
	all := true

	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
		all = false
	}

	return k.checker.GetPods(ctx, namespace, all)
}

// getEvents retrieves events
func (k *KubernetesDataSource) getEvents(ctx context.Context, params map[string]interface{}) ([]corev1.Event, error) {
	namespace := ""
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	// Get all pods first
	pods, err := k.checker.GetPods(ctx, namespace, namespace == "")
	if err != nil {
		return nil, err
	}

	// Collect events for all pods
	var allEvents []corev1.Event
	for _, pod := range pods {
		events, err := k.getEventsForPod(ctx, &pod)
		if err == nil {
			allEvents = append(allEvents, events...)
		}
	}

	return allEvents, nil
}

// getEventsForPod retrieves events for a specific pod
func (k *KubernetesDataSource) getEventsForPod(ctx context.Context, pod *corev1.Pod) ([]corev1.Event, error) {
	// Get the client from checker
	client := k.checker.GetClient()
	if client == nil {
		return nil, fmt.Errorf("kubernetes client not available")
	}

	fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.namespace=%s", pod.Name, pod.Namespace)
	eventList, err := client.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, err
	}

	return eventList.Items, nil
}

// getProblems retrieves problems by running health check
func (k *KubernetesDataSource) getProblems(ctx context.Context, params map[string]interface{}) ([]types.Problem, error) {
	namespace := ""
	all := true

	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
		all = false
	}

	req := &types.CheckRequest{
		Namespace: namespace,
		All:       all,
		Verbose:   true,
	}

	result, err := k.checker.Check(ctx, req)
	if err != nil {
		return nil, err
	}

	return result.Problems, nil
}
