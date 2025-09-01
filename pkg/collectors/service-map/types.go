package servicemap

import (
	"sync/atomic"
	"time"
)

// Service represents a discovered service in the cluster
type Service struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Type      ServiceType       `json:"type"`
	Labels    map[string]string `json:"labels"`
	Version   string            `json:"version"`
	
	// Network info
	Endpoints []Endpoint        `json:"endpoints"`
	Ports     []Port            `json:"ports"`
	
	// Relationships
	Dependencies map[string]*Dependency `json:"dependencies"`  // Services I call
	Dependents   map[string]*Dependent  `json:"dependents"`   // Services that call me
	
	// Health & metrics
	Health       HealthState       `json:"health"`
	RequestRate  float64           `json:"request_rate"`
	ErrorRate    float64           `json:"error_rate"`
	Latency      LatencyStats      `json:"latency"`
	
	// Metadata
	LastSeen     time.Time         `json:"last_seen"`
	FirstSeen    time.Time         `json:"first_seen"`
	IsExternal   bool              `json:"is_external"`
}

// ServiceType categorizes services
type ServiceType string

const (
	ServiceTypeAPI      ServiceType = "api"
	ServiceTypeDatabase ServiceType = "database"
	ServiceTypeCache    ServiceType = "cache"
	ServiceTypeQueue    ServiceType = "queue"
	ServiceTypeProxy    ServiceType = "proxy"
	ServiceTypeUnknown  ServiceType = "unknown"
)

// HealthState represents service health
type HealthState string

const (
	HealthHealthy  HealthState = "healthy"
	HealthDegraded HealthState = "degraded"
	HealthDown     HealthState = "down"
	HealthUnknown  HealthState = "unknown"
)

// Endpoint represents a service endpoint
type Endpoint struct {
	IP       string `json:"ip"`
	Port     int32  `json:"port"`
	PodName  string `json:"pod_name"`
	NodeName string `json:"node_name"`
	Ready    bool   `json:"ready"`
}

// Port represents a service port
type Port struct {
	Name       string `json:"name"`
	Port       int32  `json:"port"`
	TargetPort int32  `json:"target_port"`
	Protocol   string `json:"protocol"`
}

// Dependency represents a service dependency
type Dependency struct {
	Target      string       `json:"target"`
	CallRate    float64      `json:"call_rate"`
	ErrorRate   float64      `json:"error_rate"`
	Latency     LatencyStats `json:"latency"`
	Protocol    string       `json:"protocol"`
	Operations  []string     `json:"operations"` // e.g., ["GET /api/users", "POST /api/orders"]
	FirstSeen   time.Time    `json:"first_seen"`
	LastSeen    time.Time    `json:"last_seen"`
}

// Dependent represents a service that depends on this service
type Dependent struct {
	Source    string    `json:"source"`
	CallRate  float64   `json:"call_rate"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// LatencyStats holds latency statistics
type LatencyStats struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

// Connection represents a network connection from eBPF
type Connection struct {
	SourceIP   uint32    `json:"source_ip"`
	DestIP     uint32    `json:"dest_ip"`
	SourcePort uint16    `json:"source_port"`
	DestPort   uint16    `json:"dest_port"`
	Protocol   uint8     `json:"protocol"`
	Timestamp  time.Time `json:"timestamp"`
	BytesSent  uint64    `json:"bytes_sent"`
	BytesRecv  uint64    `json:"bytes_recv"`
	Latency    uint64    `json:"latency_ns"`
}

// ServiceMap represents the complete service topology
type ServiceMap struct {
	Services     map[string]*Service   `json:"services"`
	Connections  map[string]int        `json:"connections"` // "src->dst" -> count
	LastUpdated  time.Time             `json:"last_updated"`
	ClusterName  string                `json:"cluster_name"`
}

// Visualization types for output formats

// GraphNode represents a node in the visualization graph
type GraphNode struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Type     string            `json:"type"`
	Group    string            `json:"group"`
	Version  string            `json:"version"`
	Size     int               `json:"size"`
	Color    string            `json:"color"`
	Icon     string            `json:"icon"`
	X        float64           `json:"x,omitempty"`
	Y        float64           `json:"y,omitempty"`
	Metadata map[string]string `json:"metadata"`
}

// GraphEdge represents an edge in the visualization graph
type GraphEdge struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	Target   string  `json:"target"`
	Weight   float64 `json:"weight"`
	Color    string  `json:"color"`
	Style    string  `json:"style"`
	Label    string  `json:"label"`
	Animated bool    `json:"animated"`
}

// ServiceGraph represents the service topology for visualization
type ServiceGraph struct {
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
	Cypher string      `json:"cypher,omitempty"`
}

// ChangeEvent represents a change in the service map
type ChangeEvent struct {
	Type      ChangeType
	Service   string
	Target    string // For dependency changes
	Timestamp time.Time
}

// ChangeType represents the type of change
type ChangeType int

const (
	ChangeServiceAdded ChangeType = iota
	ChangeServiceRemoved
	ChangeServiceModified
	ChangeNewDependency
	ChangeDependencyRemoved
	ChangeHealthChanged
	ChangeVersionChanged
	ChangeConnectionUpdate
)