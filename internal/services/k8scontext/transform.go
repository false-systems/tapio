package k8scontext

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// TransformPod converts a full K8s Pod to minimal PodMeta.
// This is called by informer's SetTransform to reduce memory.
func TransformPod(pod *corev1.Pod) *PodMeta {
	// Extract containers with stripped IDs
	containers := make([]ContainerMeta, 0, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		containers = append(containers, ContainerMeta{
			Name:        cs.Name,
			ContainerID: StripContainerIDPrefix(cs.ContainerID),
			Image:       cs.Image,
			Env:         extractOTELEnvVars(pod, cs.Name),
		})
	}

	// Skip host-networked pods for IP indexing
	podIP := pod.Status.PodIP
	if podIP == pod.Status.HostIP {
		podIP = "" // Host network - don't index by this IP
	}

	// Resolve owner
	ownerKind, ownerName := ResolveOwner(pod)

	return &PodMeta{
		UID:                  string(pod.UID),
		Name:                 pod.Name,
		Namespace:            pod.Namespace,
		NodeName:             pod.Spec.NodeName,
		PodIP:                podIP,
		HostIP:               pod.Status.HostIP,
		Containers:           containers,
		OwnerKind:            ownerKind,
		OwnerName:            ownerName,
		Labels:               pod.Labels,
		OTELServiceName:      computeOTELServiceName(pod, ownerName),
		OTELServiceNamespace: pod.Namespace,
	}
}

// TransformService converts a full K8s Service to minimal ServiceMeta.
func TransformService(svc *corev1.Service) *ServiceMeta {
	ports := make([]PortMeta, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, PortMeta{
			Name:     p.Name,
			Port:     p.Port,
			Protocol: string(p.Protocol),
		})
	}

	return &ServiceMeta{
		UID:       string(svc.UID),
		Name:      svc.Name,
		Namespace: svc.Namespace,
		ClusterIP: svc.Spec.ClusterIP,
		Type:      string(svc.Spec.Type),
		Ports:     ports,
		Selector:  svc.Spec.Selector,
	}
}

// StripContainerIDPrefix removes the runtime prefix from container ID.
// Supported: containerd://, docker://, cri-o://
func StripContainerIDPrefix(fullID string) string {
	if idx := strings.Index(fullID, "://"); idx != -1 {
		return fullID[idx+3:]
	}
	return fullID
}

// OTEL env vars we care about
var otelEnvVars = map[string]struct{}{
	"OTEL_SERVICE_NAME":        {},
	"OTEL_RESOURCE_ATTRIBUTES": {},
}

func extractOTELEnvVars(pod *corev1.Pod, containerName string) map[string]string {
	result := make(map[string]string)

	for _, c := range pod.Spec.Containers {
		if c.Name != containerName {
			continue
		}
		for _, e := range c.Env {
			if _, ok := otelEnvVars[e.Name]; ok && e.Value != "" {
				result[e.Name] = e.Value
			}
		}
	}

	return result
}

func computeOTELServiceName(pod *corev1.Pod, deploymentName string) string {
	// 1. OTEL_SERVICE_NAME env var (highest priority)
	for _, c := range pod.Spec.Containers {
		for _, e := range c.Env {
			if e.Name == "OTEL_SERVICE_NAME" && e.Value != "" {
				return e.Value
			}
		}
	}

	// 2. resource.k8s.deployment.name annotation
	if name := pod.Annotations["resource.k8s.deployment.name"]; name != "" {
		return name
	}

	// 3. app.kubernetes.io/name label
	if name := pod.Labels["app.kubernetes.io/name"]; name != "" {
		return name
	}

	// 4. app label
	if name := pod.Labels["app"]; name != "" {
		return name
	}

	// 5. Resolved deployment name (from heuristic)
	if deploymentName != "" {
		return deploymentName
	}

	// 6. Pod name fallback
	return pod.Name
}
