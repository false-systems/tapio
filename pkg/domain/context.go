package domain

// PodContext holds K8s pod metadata with pre-computed OTEL attributes for fast event enrichment.
// This is a Level 0 domain type with ZERO dependencies.
type PodContext struct {
	// K8s identifiers
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`

	// Network identifiers (for observer lookup)
	PodIP    string `json:"pod_ip"`
	NodeName string `json:"node_name"`

	// Owner references (for topology)
	OwnerKind string `json:"owner_kind"` // e.g., "Deployment", "StatefulSet"
	OwnerName string `json:"owner_name"` // e.g., "api-backend"

	// Pre-computed OTEL attributes (computed once on pod add/update)
	// This avoids per-event label parsing and string manipulation (100x speedup)
	// Priority cascade: env vars → annotations → labels (Beyla pattern)
	OTELAttributes map[string]string `json:"otel_attributes,omitempty"`
}
