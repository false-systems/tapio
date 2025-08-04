package k8sgrapher

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// formatSelector converts a map[string]string selector to a string representation
func formatSelector(selector map[string]string) string {
	if len(selector) == 0 {
		return ""
	}
	return labels.SelectorFromSet(selector).String()
}

// formatLabels converts a map[string]string to a string representation for Neo4j
func formatLabels(labelMap map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range labelMap {
		result[k] = v
	}
	return result
}

// formatLabelSelector converts a LabelSelector to string representation
func formatLabelSelector(selector *metav1.LabelSelector) string {
	if selector == nil {
		return ""
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return ""
	}
	return labelSelector.String()
}

// getOwnerInfo extracts owner information from a Pod
func getOwnerInfo(pod *corev1.Pod) (string, string) {
	if len(pod.OwnerReferences) == 0 {
		return "", ""
	}

	// Return the first owner (usually there's only one)
	owner := pod.OwnerReferences[0]
	return owner.Kind, owner.Name
}

// isPodReady checks if a Pod is in Ready condition
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// getMapKeys returns the keys from a string map as a slice
func getMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// getSecretKeys returns the keys from a secret data map as a slice
func getSecretKeys(data map[string][]byte) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}

// sanitizeForNeo4j ensures string values are safe for Neo4j queries
func sanitizeForNeo4j(s string) string {
	// Neo4j handles most escaping automatically when using parameters
	// This is mainly for safety in case of direct query building
	return strings.ReplaceAll(s, "'", "\\'")
}

// buildResourceID creates a unique identifier for a K8s resource
func buildResourceID(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// parseResourceID splits a resource ID back into namespace and name
func parseResourceID(resourceID string) (namespace, name string) {
	parts := strings.SplitN(resourceID, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", resourceID
}
