// Package k8scontext provides in-memory K8s metadata lookup for eBPF enrichment.
// It watches local pods and all services, providing O(1) lookups by IP, container ID, or name.
package k8scontext

// Locality indicates the traffic relationship to this node.
type Locality int

const (
	LocalPod       Locality = iota // Pod on this node
	ClusterService                 // Known ClusterIP service
	RemotePod                      // K8s IP range, but not this node
	External                       // Public internet / out-of-cluster
)

func (l Locality) String() string {
	switch l {
	case LocalPod:
		return "local_pod"
	case ClusterService:
		return "cluster_service"
	case RemotePod:
		return "remote_pod"
	case External:
		return "external"
	default:
		return "unknown"
	}
}

// PodMeta is the minimal cached representation of a K8s Pod.
type PodMeta struct {
	// Identity
	UID       string
	Name      string
	Namespace string
	NodeName  string

	// Network (for IP -> Pod lookup)
	PodIP  string
	HostIP string

	// Containers (for CID -> Pod lookup)
	Containers []ContainerMeta

	// Ownership (resolved via heuristic)
	OwnerKind string // Deployment, StatefulSet, DaemonSet, Job, CronJob
	OwnerName string // Resolved root owner name

	// Labels
	Labels map[string]string

	// Pre-computed OTEL attributes
	OTELServiceName      string
	OTELServiceNamespace string

	// Lifecycle
	Terminating bool // True if pod is in tombstone cache
	Synthetic   bool // True if from CRI fallback (not yet in informer)
}

// NamespacedName returns namespace/name format.
func (p *PodMeta) NamespacedName() string {
	return p.Namespace + "/" + p.Name
}

// ContainerMeta is the minimal cached representation of a container.
type ContainerMeta struct {
	Name        string
	ContainerID string // Short ID (prefix stripped)
	Image       string
	Env         map[string]string // Only OTEL-relevant env vars
}

// ShortID returns first 10 chars of container ID.
func (c *ContainerMeta) ShortID() string {
	if len(c.ContainerID) > 10 {
		return c.ContainerID[:10]
	}
	return c.ContainerID
}

// ServiceMeta is the minimal cached representation of a K8s Service.
type ServiceMeta struct {
	UID       string
	Name      string
	Namespace string
	ClusterIP string
	Type      string // ClusterIP, NodePort, LoadBalancer
	Ports     []PortMeta
	Selector  map[string]string
}

// NamespacedName returns namespace/name format.
func (s *ServiceMeta) NamespacedName() string {
	return s.Namespace + "/" + s.Name
}

// PortMeta is a service port.
type PortMeta struct {
	Name     string
	Port     int32
	Protocol string // TCP, UDP
}

// LookupResult is the result of an IP lookup with locality.
type LookupResult struct {
	Pod      *PodMeta
	Service  *ServiceMeta
	Locality Locality
}
