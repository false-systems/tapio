package services

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Config holds configuration for the service map observer
type Config struct {
	// Base configuration
	BufferSize int `yaml:"buffer_size" json:"buffer_size"`

	// Emission strategy
	EmitOnChange         bool          `yaml:"emit_on_change" json:"emit_on_change"`
	ChangeDebounce       time.Duration `yaml:"change_debounce" json:"change_debounce"`
	FullSnapshotInterval time.Duration `yaml:"full_snapshot_interval" json:"full_snapshot_interval"`
	SkipUnchanged        bool          `yaml:"skip_unchanged" json:"skip_unchanged"`
	MinEmitInterval      time.Duration `yaml:"min_emit_interval" json:"min_emit_interval"`

	// Kubernetes configuration
	KubeConfig        string   `yaml:"kube_config" json:"kube_config"`
	Namespaces        []string `yaml:"namespaces" json:"namespaces"` // Empty means all namespaces
	ExcludeNamespaces []string `yaml:"exclude_namespaces" json:"exclude_namespaces"`

	// Service discovery
	EnableK8sDiscovery bool          `yaml:"enable_k8s_discovery" json:"enable_k8s_discovery"`
	EnableDNSTracking  bool          `yaml:"enable_dns_tracking" json:"enable_dns_tracking"`
	ServiceTimeout     time.Duration `yaml:"service_timeout" json:"service_timeout"`

	// eBPF configuration
	EnableEBPF     bool          `yaml:"enable_ebpf" json:"enable_ebpf"`
	MaxConnections int           `yaml:"max_connections" json:"max_connections"`
	ConnectionTTL  time.Duration `yaml:"connection_ttl" json:"connection_ttl"`

	// Service detection
	AutoDetectType bool                   `yaml:"auto_detect_type" json:"auto_detect_type"`
	PortMappings   map[int32]ServiceType  `yaml:"port_mappings" json:"port_mappings"`
	ImagePatterns  map[string]ServiceType `yaml:"image_patterns" json:"image_patterns"`

	// Visualization
	EnableVisualization bool          `yaml:"enable_visualization" json:"enable_visualization"`
	GraphUpdateInterval time.Duration `yaml:"graph_update_interval" json:"graph_update_interval"`
	MaxGraphNodes       int           `yaml:"max_graph_nodes" json:"max_graph_nodes"`

	// Filtering
	IncludeExternalServices bool `yaml:"include_external_services" json:"include_external_services"`
	MinConnectionCount      int  `yaml:"min_connection_count" json:"min_connection_count"`
	IgnoreSystemNamespaces  bool `yaml:"ignore_system_namespaces" json:"ignore_system_namespaces"`

	// Logger
	Logger *zap.Logger `yaml:"-" json:"-"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		BufferSize: 10000,

		// Smart emission by default
		EmitOnChange:         true,
		ChangeDebounce:       5 * time.Second,
		FullSnapshotInterval: 5 * time.Minute,
		SkipUnchanged:        true,
		MinEmitInterval:      1 * time.Second,

		// K8s discovery enabled by default
		EnableK8sDiscovery: true,
		ServiceTimeout:     5 * time.Minute,
		ExcludeNamespaces: []string{
			"kube-system",
			"kube-public",
			"kube-node-lease",
		},

		// eBPF enabled for connection tracking
		EnableEBPF:     true,
		MaxConnections: 100000,
		ConnectionTTL:  5 * time.Minute,

		// Auto-detection enabled
		AutoDetectType: true,
		PortMappings:   getDefaultPortMappings(),
		ImagePatterns:  getDefaultImagePatterns(),

		// Visualization
		EnableVisualization: true,
		GraphUpdateInterval: 10 * time.Second,
		MaxGraphNodes:       100,

		// Filtering
		IgnoreSystemNamespaces: true,
		MinConnectionCount:     1,
	}
}

// Validate validates configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be positive")
	}

	if c.ChangeDebounce < 0 {
		return fmt.Errorf("change debounce cannot be negative")
	}

	if c.FullSnapshotInterval < 0 {
		return fmt.Errorf("full snapshot interval cannot be negative")
	}

	if c.MinEmitInterval < 0 {
		return fmt.Errorf("min emit interval cannot be negative")
	}

	if c.ServiceTimeout < 0 {
		return fmt.Errorf("service timeout cannot be negative")
	}

	if c.MaxConnections < 0 {
		return fmt.Errorf("max connections cannot be negative")
	}

	if c.ConnectionTTL < 0 {
		return fmt.Errorf("connection TTL cannot be negative")
	}

	if c.MaxGraphNodes < 0 {
		return fmt.Errorf("max graph nodes cannot be negative")
	}

	if c.MinConnectionCount < 0 {
		return fmt.Errorf("min connection count cannot be negative")
	}

	return nil
}

// getDefaultPortMappings returns default port to service type mappings
func getDefaultPortMappings() map[int32]ServiceType {
	return map[int32]ServiceType{
		// Databases
		3306:  ServiceTypeDatabase, // MySQL
		5432:  ServiceTypeDatabase, // PostgreSQL
		27017: ServiceTypeDatabase, // MongoDB
		9042:  ServiceTypeDatabase, // Cassandra
		7000:  ServiceTypeDatabase, // Cassandra inter-node
		5984:  ServiceTypeDatabase, // CouchDB
		8086:  ServiceTypeDatabase, // InfluxDB

		// Caches
		6379:  ServiceTypeCache, // Redis
		11211: ServiceTypeCache, // Memcached

		// Message Queues
		5672:  ServiceTypeQueue, // RabbitMQ
		15672: ServiceTypeQueue, // RabbitMQ Management
		9092:  ServiceTypeQueue, // Kafka
		4222:  ServiceTypeQueue, // NATS
		8222:  ServiceTypeQueue, // NATS monitoring

		// Proxies/Load Balancers
		80:   ServiceTypeProxy, // HTTP
		443:  ServiceTypeProxy, // HTTPS
		8080: ServiceTypeAPI,   // Common API port
		3000: ServiceTypeAPI,   // Common Node.js port
		8000: ServiceTypeAPI,   // Common Python port
		8888: ServiceTypeAPI,   // Common alternative port
		9000: ServiceTypeAPI,   // PHP-FPM
	}
}

// getDefaultImagePatterns returns default image name patterns to service type mappings
func getDefaultImagePatterns() map[string]ServiceType {
	return map[string]ServiceType{
		// Databases
		"mysql":       ServiceTypeDatabase,
		"postgres":    ServiceTypeDatabase,
		"mongodb":     ServiceTypeDatabase,
		"cassandra":   ServiceTypeDatabase,
		"couchdb":     ServiceTypeDatabase,
		"influxdb":    ServiceTypeDatabase,
		"mariadb":     ServiceTypeDatabase,
		"cockroachdb": ServiceTypeDatabase,

		// Caches
		"redis":     ServiceTypeCache,
		"memcached": ServiceTypeCache,
		"hazelcast": ServiceTypeCache,

		// Message Queues
		"rabbitmq": ServiceTypeQueue,
		"kafka":    ServiceTypeQueue,
		"nats":     ServiceTypeQueue,
		"pulsar":   ServiceTypeQueue,
		"activemq": ServiceTypeQueue,

		// Proxies
		"nginx":   ServiceTypeProxy,
		"haproxy": ServiceTypeProxy,
		"envoy":   ServiceTypeProxy,
		"traefik": ServiceTypeProxy,
		"istio":   ServiceTypeProxy,
		"linkerd": ServiceTypeProxy,
	}
}
