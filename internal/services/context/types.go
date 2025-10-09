package context

import "github.com/nats-io/nats.go"

// PodInfo matches decoder expectations (from pkg/decoders/k8s_pod.go)
type PodInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	PodIP     string            `json:"pod_ip"`
	HostIP    string            `json:"host_ip"`
	Labels    map[string]string `json:"labels"`
}

// ServiceInfo matches decoder expectations (from pkg/decoders/k8s_service.go)
type ServiceInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	ClusterIP string            `json:"cluster_ip"`
	Type      string            `json:"type"`
	Labels    map[string]string `json:"labels"`
}

// Config holds Context Service configuration
type Config struct {
	NATSConn *nats.Conn
	KVBucket string
}
