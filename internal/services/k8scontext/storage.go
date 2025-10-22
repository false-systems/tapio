package k8scontext

import (
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// toPodInfo transforms K8s Pod to PodInfo
func toPodInfo(pod *corev1.Pod) PodInfo {
	labels := pod.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	// Pre-compute OTEL attributes (Beyla priority cascade: env vars → annotations → labels)
	// This is done ONCE on pod add/update for 100x faster event enrichment
	otelAttrs := ComputeOTELAttributes(pod)

	return PodInfo{
		Name:           pod.Name,
		Namespace:      pod.Namespace,
		PodIP:          pod.Status.PodIP,
		HostIP:         pod.Status.HostIP,
		Labels:         labels,
		OTELAttributes: otelAttrs,
	}
}

// toServiceInfo transforms K8s Service to ServiceInfo
func toServiceInfo(service *corev1.Service) ServiceInfo {
	labels := service.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	return ServiceInfo{
		Name:      service.Name,
		Namespace: service.Namespace,
		ClusterIP: service.Spec.ClusterIP,
		Type:      string(service.Spec.Type),
		Labels:    labels,
	}
}

// makePodKey generates NATS KV key for pod (legacy - by IP)
func makePodKey(ip string) string {
	return fmt.Sprintf("pod.ip.%s", ip)
}

// makePodByIPKey generates NATS KV key for pod lookup by IP
func makePodByIPKey(ip string) string {
	return fmt.Sprintf("pod.ip.%s", ip)
}

// makePodByUIDKey generates NATS KV key for pod lookup by UID
func makePodByUIDKey(uid string) string {
	return fmt.Sprintf("pod.uid.%s", uid)
}

// makePodByNameKey generates NATS KV key for pod lookup by namespace/name
func makePodByNameKey(namespace, name string) string {
	return fmt.Sprintf("pod.name.%s.%s", namespace, name)
}

// makePodNameKey generates in-memory cache key for pod by namespace/name
func makePodNameKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// makeServiceKey generates NATS KV key for service
func makeServiceKey(ip string) string {
	return fmt.Sprintf("service.ip.%s", ip)
}

// makeDeploymentKey generates NATS KV key for deployment
func makeDeploymentKey(namespace, name string) string {
	return fmt.Sprintf("deployment.%s.%s", namespace, name)
}

// makeNodeKey generates NATS KV key for node
func makeNodeKey(name string) string {
	return fmt.Sprintf("node.%s", name)
}

// makeOwnerKey generates NATS KV key for ownership
func makeOwnerKey(uid string) string {
	return fmt.Sprintf("ownership.%s", uid)
}

// shouldSkipPod returns true if pod should be skipped
func shouldSkipPod(pod *corev1.Pod) bool {
	return pod.Status.PodIP == ""
}

// shouldSkipService returns true if service should be skipped
func shouldSkipService(service *corev1.Service) bool {
	return service.Spec.ClusterIP == "" || service.Spec.ClusterIP == "None"
}

// serializePodInfo marshals PodInfo to JSON
func serializePodInfo(podInfo PodInfo) ([]byte, error) {
	data, err := json.Marshal(podInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PodInfo: %w", err)
	}
	return data, nil
}

// serializeServiceInfo marshals ServiceInfo to JSON
func serializeServiceInfo(serviceInfo ServiceInfo) ([]byte, error) {
	data, err := json.Marshal(serviceInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ServiceInfo: %w", err)
	}
	return data, nil
}

// storePodMetadata writes pod metadata to NATS KV
func (s *Service) storePodMetadata(pod *corev1.Pod) error {
	if shouldSkipPod(pod) {
		return nil
	}

	podInfo := toPodInfo(pod)
	data, err := serializePodInfo(podInfo)
	if err != nil {
		return err
	}

	key := makePodKey(pod.Status.PodIP)
	_, err = s.kv.Put(key, data)
	if err != nil {
		return fmt.Errorf("failed to store pod metadata for %s: %w", key, err)
	}

	return nil
}

// deletePodMetadata removes pod metadata from NATS KV
func (s *Service) deletePodMetadata(pod *corev1.Pod) error {
	if shouldSkipPod(pod) {
		return nil
	}

	key := makePodKey(pod.Status.PodIP)
	err := s.kv.Delete(key)
	if err != nil {
		return fmt.Errorf("failed to delete pod metadata for %s: %w", key, err)
	}

	return nil
}

// storeServiceMetadata writes service metadata to NATS KV
func (s *Service) storeServiceMetadata(svc *corev1.Service) error {
	if shouldSkipService(svc) {
		return nil
	}

	serviceInfo := toServiceInfo(svc)
	data, err := serializeServiceInfo(serviceInfo)
	if err != nil {
		return err
	}

	key := makeServiceKey(svc.Spec.ClusterIP)
	_, err = s.kv.Put(key, data)
	if err != nil {
		return fmt.Errorf("failed to store service metadata for %s: %w", key, err)
	}

	return nil
}

// deleteServiceMetadata removes service metadata from NATS KV
func (s *Service) deleteServiceMetadata(svc *corev1.Service) error {
	if shouldSkipService(svc) {
		return nil
	}

	key := makeServiceKey(svc.Spec.ClusterIP)
	err := s.kv.Delete(key)
	if err != nil {
		return fmt.Errorf("failed to delete service metadata for %s: %w", key, err)
	}

	return nil
}

// toDeploymentInfo transforms K8s Deployment to DeploymentInfo
func toDeploymentInfo(deployment *appsv1.Deployment) DeploymentInfo {
	labels := deployment.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	image := ""
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		image = deployment.Spec.Template.Spec.Containers[0].Image
	}

	replicas := int32(0)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}

	return DeploymentInfo{
		Name:      deployment.Name,
		Namespace: deployment.Namespace,
		Replicas:  replicas,
		Image:     image,
		Labels:    labels,
	}
}

// toNodeInfo transforms K8s Node to NodeInfo
func toNodeInfo(node *corev1.Node) NodeInfo {
	labels := node.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	return NodeInfo{
		Name:   node.Name,
		Labels: labels,
		Zone:   labels["topology.kubernetes.io/zone"],
		Region: labels["topology.kubernetes.io/region"],
	}
}

// toOwnerInfo extracts ownership info from Pod
func toOwnerInfo(pod *corev1.Pod) *OwnerInfo {
	for _, ownerRef := range pod.OwnerReferences {
		// Look for ReplicaSet owner (created by Deployment)
		if ownerRef.Kind == "ReplicaSet" {
			// We'll need to look up the ReplicaSet to find Deployment
			// For now, just store ReplicaSet info
			return &OwnerInfo{
				OwnerKind: ownerRef.Kind,
				OwnerName: ownerRef.Name,
				Namespace: pod.Namespace,
			}
		}
		// Direct ownership by Deployment, StatefulSet, DaemonSet
		if ownerRef.Kind == "Deployment" || ownerRef.Kind == "StatefulSet" || ownerRef.Kind == "DaemonSet" {
			return &OwnerInfo{
				OwnerKind: ownerRef.Kind,
				OwnerName: ownerRef.Name,
				Namespace: pod.Namespace,
			}
		}
	}
	return nil
}

// serializeDeploymentInfo marshals DeploymentInfo to JSON
func serializeDeploymentInfo(info DeploymentInfo) ([]byte, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DeploymentInfo: %w", err)
	}
	return data, nil
}

// serializeNodeInfo marshals NodeInfo to JSON
func serializeNodeInfo(info NodeInfo) ([]byte, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal NodeInfo: %w", err)
	}
	return data, nil
}

// serializeOwnerInfo marshals OwnerInfo to JSON
func serializeOwnerInfo(info OwnerInfo) ([]byte, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OwnerInfo: %w", err)
	}
	return data, nil
}

// storeDeploymentMetadata writes deployment metadata to NATS KV
func (s *Service) storeDeploymentMetadata(deployment *appsv1.Deployment) error {
	info := toDeploymentInfo(deployment)
	data, err := serializeDeploymentInfo(info)
	if err != nil {
		return err
	}

	key := makeDeploymentKey(deployment.Namespace, deployment.Name)
	_, err = s.kv.Put(key, data)
	if err != nil {
		return fmt.Errorf("failed to store deployment metadata for %s: %w", key, err)
	}

	return nil
}

// deleteDeploymentMetadata removes deployment metadata from NATS KV
func (s *Service) deleteDeploymentMetadata(deployment *appsv1.Deployment) error {
	key := makeDeploymentKey(deployment.Namespace, deployment.Name)
	err := s.kv.Delete(key)
	if err != nil {
		return fmt.Errorf("failed to delete deployment metadata for %s: %w", key, err)
	}

	return nil
}

// storeNodeMetadata writes node metadata to NATS KV
func (s *Service) storeNodeMetadata(node *corev1.Node) error {
	info := toNodeInfo(node)
	data, err := serializeNodeInfo(info)
	if err != nil {
		return err
	}

	key := makeNodeKey(node.Name)
	_, err = s.kv.Put(key, data)
	if err != nil {
		return fmt.Errorf("failed to store node metadata for %s: %w", key, err)
	}

	return nil
}

// deleteNodeMetadata removes node metadata from NATS KV
func (s *Service) deleteNodeMetadata(node *corev1.Node) error {
	key := makeNodeKey(node.Name)
	err := s.kv.Delete(key)
	if err != nil {
		return fmt.Errorf("failed to delete node metadata for %s: %w", key, err)
	}

	return nil
}

// storeOwnerMetadata writes ownership metadata to NATS KV
func (s *Service) storeOwnerMetadata(pod *corev1.Pod) error {
	ownerInfo := toOwnerInfo(pod)
	if ownerInfo == nil {
		// No owner, skip
		return nil
	}

	data, err := serializeOwnerInfo(*ownerInfo)
	if err != nil {
		return err
	}

	key := makeOwnerKey(string(pod.UID))
	_, err = s.kv.Put(key, data)
	if err != nil {
		return fmt.Errorf("failed to store owner metadata for %s: %w", key, err)
	}

	return nil
}

// deleteOwnerMetadata removes ownership metadata from NATS KV
func (s *Service) deleteOwnerMetadata(pod *corev1.Pod) error {
	key := makeOwnerKey(string(pod.UID))
	err := s.kv.Delete(key)
	if err != nil {
		return fmt.Errorf("failed to delete owner metadata for %s: %w", key, err)
	}

	return nil
}
