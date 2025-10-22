package k8scontext

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// ComputeOTELAttributes extracts OTEL attributes from K8s Pod using Beyla's priority cascade:
// 1. Start with labels (app.kubernetes.io/* → service.*)
// 2. Override with annotations (resource.opentelemetry.io/* → *)
// 3. Override with env vars (OTEL_SERVICE_NAME, etc.)
//
// This function is called ONCE per pod (on add/update) to pre-compute attributes.
// The result is cached in PodContext.OTELAttributes for fast event enrichment.
func ComputeOTELAttributes(pod *corev1.Pod) map[string]string {
	if pod == nil {
		return make(map[string]string)
	}

	attrs := make(map[string]string)

	// Phase 1: Extract from labels (app.kubernetes.io/name → service.name)
	if pod.Labels != nil {
		if name := pod.Labels["app.kubernetes.io/name"]; name != "" {
			attrs["service.name"] = name
		}
		if version := pod.Labels["app.kubernetes.io/version"]; version != "" {
			attrs["service.version"] = version
		}
	}

	// Phase 2: Override with annotations (resource.opentelemetry.io/service.name → service.name)
	if pod.Annotations != nil {
		for key, value := range pod.Annotations {
			if strings.HasPrefix(key, "resource.opentelemetry.io/") {
				attrName := strings.TrimPrefix(key, "resource.opentelemetry.io/")
				attrs[attrName] = value
			}
		}
	}

	// Phase 3: Override with env vars (highest priority)
	if len(pod.Spec.Containers) > 0 {
		for _, env := range pod.Spec.Containers[0].Env {
			if env.Name == "OTEL_SERVICE_NAME" && env.Value != "" {
				attrs["service.name"] = env.Value
			}
		}
	}

	// Fallback: Use pod name if no service.name found
	if _, exists := attrs["service.name"]; !exists {
		attrs["service.name"] = pod.Name
	}

	return attrs
}
