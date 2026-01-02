package k8scontext

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// ResolveOwner extracts the root owner kind and name from a pod.
// Uses heuristics from Beyla plus advanced patterns (ArgoCD, Knative, Flux).
func ResolveOwner(pod *corev1.Pod) (kind, name string) {
	// 1. GOLD STANDARD: Explicit annotation
	if n := pod.Annotations["resource.k8s.deployment.name"]; n != "" {
		return "Deployment", n
	}

	// 2. ArgoCD pattern
	if n := pod.Labels["app.kubernetes.io/instance"]; n != "" {
		return "Application", n
	}

	// 3. Knative pattern
	if n := pod.Labels["serving.knative.dev/service"]; n != "" {
		return "KnativeService", n
	}

	// 4. Flux pattern
	if n := pod.Labels["kustomize.toolkit.fluxcd.io/name"]; n != "" {
		return "Kustomization", n
	}

	// 5. Standard owner references with heuristic
	for _, ref := range pod.OwnerReferences {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}

		switch ref.Kind {
		case "ReplicaSet":
			// Heuristic: strip hash suffix to get Deployment name
			if idx := strings.LastIndexByte(ref.Name, '-'); idx > 0 {
				return "Deployment", ref.Name[:idx]
			}
			return "ReplicaSet", ref.Name

		case "Job":
			// Heuristic: strip suffix to get CronJob name
			if idx := strings.LastIndexByte(ref.Name, '-'); idx > 0 {
				return "CronJob", ref.Name[:idx]
			}
			return "Job", ref.Name

		case "StatefulSet", "DaemonSet":
			return ref.Kind, ref.Name

		default:
			return ref.Kind, ref.Name
		}
	}

	// No owner - standalone pod
	return "Pod", pod.Name
}
