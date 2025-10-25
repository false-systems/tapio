package k8scontext

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"k8s.io/client-go/rest"
)

// PodInfo matches pkg/decoders/k8s_pod.go:PodInfo EXACTLY
// This type MUST remain identical to the decoder type for JSON unmarshaling to work
type PodInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	PodIP     string            `json:"pod_ip"`
	HostIP    string            `json:"host_ip"`
	Labels    map[string]string `json:"labels"`

	// Pre-computed OTEL attributes (optional - backward compatible with omitempty)
	// Computed once on pod add/update using Beyla priority cascade:
	// env vars → annotations → labels → fallback to pod name
	OTELAttributes map[string]string `json:"otel_attributes,omitempty"`
}

// ServiceInfo matches pkg/decoders/k8s_service.go:ServiceInfo EXACTLY
// This type MUST remain identical to the decoder type for JSON unmarshaling to work
type ServiceInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	ClusterIP string            `json:"cluster_ip"`
	Type      string            `json:"type"`
	Labels    map[string]string `json:"labels"`
}

// DeploymentInfo provides deployment context for ownership tracking
type DeploymentInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Replicas  int32             `json:"replicas"`
	Image     string            `json:"image"` // First container image
	Labels    map[string]string `json:"labels"`
}

// NodeInfo provides node context
type NodeInfo struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Zone   string            `json:"zone,omitempty"`   // topology.kubernetes.io/zone
	Region string            `json:"region,omitempty"` // topology.kubernetes.io/region
}

// OwnerInfo tracks Pod ownership (Pod → Deployment/StatefulSet/DaemonSet)
type OwnerInfo struct {
	OwnerKind string `json:"owner_kind"` // Deployment, StatefulSet, DaemonSet
	OwnerName string `json:"owner_name"`
	Namespace string `json:"namespace"`
}

// Config holds Context Service configuration
type Config struct {
	// NATS connection (required)
	NATSConn *nats.Conn

	// KV bucket name (default: "tapio-k8s-context")
	KVBucket string

	// K8s client config (optional - for testing)
	// If nil, uses InClusterConfig()
	K8sConfig *rest.Config

	// Event buffer size (default: 1000)
	EventBufferSize int

	// Max retries for NATS KV writes (default: 3)
	MaxRetries int

	// Retry interval for NATS KV writes (default: 1s)
	RetryInterval time.Duration

	// Event emission configuration (optional - if provided, emits diagnostic events)
	Output    OutputConfig   // Which outputs to enable (OTLP, NATS, stdout)
	Publisher EventPublisher // NATS publisher for enterprise tier (optional)
	ClusterID string         // Cluster ID for multi-cluster support
}

// OutputConfig defines which event outputs are enabled
type OutputConfig struct {
	OTEL   bool // Emit to OTLP (community tier - Grafana, Prometheus)
	Tapio  bool // Emit to NATS with graph enrichment (enterprise tier)
	Stdout bool // Emit to stdout (debugging)
}

// EventPublisher publishes enriched Tapio events to NATS
type EventPublisher interface {
	Publish(ctx context.Context, subject string, event interface{}) error
}
