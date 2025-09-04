package servicemap

import (
	"time"
)

// Config holds configuration for the service map collector
type Config struct {
	// Base configuration
	Enabled          bool          `yaml:"enabled" json:"enabled"`
	BufferSize       int           `yaml:"buffer_size" json:"buffer_size"`
	
	// Emission strategy
	EmitOnChange           bool          `yaml:"emit_on_change" json:"emit_on_change"`
	ChangeDebounce         time.Duration `yaml:"change_debounce" json:"change_debounce"`
	FullSnapshotInterval   time.Duration `yaml:"full_snapshot_interval" json:"full_snapshot_interval"`
	SkipUnchanged          bool          `yaml:"skip_unchanged" json:"skip_unchanged"`
	MinEmitInterval        time.Duration `yaml:"min_emit_interval" json:"min_emit_interval"`
	
	// Kubernetes configuration
	KubeConfig       string        `yaml:"kube_config" json:"kube_config"`
	Namespaces       []string      `yaml:"namespaces" json:"namespaces"`        // Empty means all namespaces
	ExcludeNamespaces []string     `yaml:"exclude_namespaces" json:"exclude_namespaces"`
	
	// Service discovery
	EnableK8sDiscovery bool        `yaml:"enable_k8s_discovery" json:"enable_k8s_discovery"`
	EnableDNSTracking  bool        `yaml:"enable_dns_tracking" json:"enable_dns_tracking"`
	ServiceTimeout     time.Duration `yaml:"service_timeout" json:"service_timeout"`
	
	// eBPF configuration
	EnableEBPF       bool          `yaml:"enable_ebpf" json:"enable_ebpf"`
	MaxConnections   int           `yaml:"max_connections" json:"max_connections"`
	ConnectionTTL    time.Duration `yaml:"connection_ttl" json:"connection_ttl"`
	
	// Service detection
	AutoDetectType   bool          `yaml:"auto_detect_type" json:"auto_detect_type"`
	PortMappings     map[int32]ServiceType `yaml:"port_mappings" json:"port_mappings"`
	ImagePatterns    map[string]ServiceType `yaml:"image_patterns" json:"image_patterns"`
	
	// Visualization
	EnableVisualization bool       `yaml:"enable_visualization" json:"enable_visualization"`
	GraphUpdateInterval time.Duration `yaml:"graph_update_interval" json:"graph_update_interval"`
	MaxGraphNodes       int        `yaml:"max_graph_nodes" json:"max_graph_nodes"`
	
	// Filtering
	IncludeExternalServices bool   `yaml:"include_external_services" json:"include_external_services"`
	MinConnectionCount      int    `yaml:"min_connection_count" json:"min_connection_count"`
	IgnoreSystemNamespaces  bool   `yaml:"ignore_system_namespaces" json:"ignore_system_namespaces"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:             true,
		BufferSize:          10000,
		
		// Smart emission strategy
		EmitOnChange:         true,
		ChangeDebounce:       5 * time.Second,
		FullSnapshotInterval: 5 * time.Minute,
		SkipUnchanged:        true,
		MinEmitInterval:      5 * time.Second,
		
		// Kubernetes
		Namespaces:          []string{}, // All namespaces by default
		ExcludeNamespaces:   []string{"kube-system", "kube-public", "kube-node-lease"},
		EnableK8sDiscovery:  true,
		ServiceTimeout:      5 * time.Minute,
		
		// eBPF
		EnableEBPF:          true,
		MaxConnections:      100000,
		ConnectionTTL:       5 * time.Minute,
		
		// Service detection
		AutoDetectType:      true,
		PortMappings:        defaultPortMappings(),
		ImagePatterns:       defaultImagePatterns(),
		
		// Visualization
		EnableVisualization: true,
		GraphUpdateInterval: 30 * time.Second,
		MaxGraphNodes:       500,
		
		// Filtering
		IncludeExternalServices: false,
		MinConnectionCount:      1,
		IgnoreSystemNamespaces:  true,
	}
}

// defaultPortMappings returns common port to service type mappings
func defaultPortMappings() map[int32]ServiceType {
	return map[int32]ServiceType{
		// Databases
		3306:  ServiceTypeDatabase, // MySQL
		5432:  ServiceTypeDatabase, // PostgreSQL
		27017: ServiceTypeDatabase, // MongoDB
		9042:  ServiceTypeDatabase, // Cassandra
		
		// Caches
		6379:  ServiceTypeCache,    // Redis
		11211: ServiceTypeCache,    // Memcached
		
		// Message Queues
		5672:  ServiceTypeQueue,    // RabbitMQ
		9092:  ServiceTypeQueue,    // Kafka
		4222:  ServiceTypeQueue,    // NATS
		
		// Proxies
		80:    ServiceTypeProxy,    // HTTP
		443:   ServiceTypeProxy,    // HTTPS
		8080:  ServiceTypeAPI,      // Common API port
		3000:  ServiceTypeAPI,      // Common Node.js port
		8000:  ServiceTypeAPI,      // Common Python port
		9000:  ServiceTypeAPI,      // Common PHP port
	}
}

// defaultImagePatterns returns common image patterns to service type mappings
func defaultImagePatterns() map[string]ServiceType {
	return map[string]ServiceType{
		// Databases
		"postgres":    ServiceTypeDatabase,
		"mysql":       ServiceTypeDatabase,
		"mariadb":     ServiceTypeDatabase,
		"mongo":       ServiceTypeDatabase,
		"cassandra":   ServiceTypeDatabase,
		"cockroach":   ServiceTypeDatabase,
		
		// Caches
		"redis":       ServiceTypeCache,
		"memcached":   ServiceTypeCache,
		"hazelcast":   ServiceTypeCache,
		
		// Queues
		"kafka":       ServiceTypeQueue,
		"rabbitmq":    ServiceTypeQueue,
		"nats":        ServiceTypeQueue,
		"pulsar":      ServiceTypeQueue,
		
		// Proxies
		"nginx":       ServiceTypeProxy,
		"envoy":       ServiceTypeProxy,
		"haproxy":     ServiceTypeProxy,
		"traefik":     ServiceTypeProxy,
		"istio":       ServiceTypeProxy,
		"linkerd":     ServiceTypeProxy,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		c.BufferSize = 10000
	}
	
	if c.MaxConnections <= 0 {
		c.MaxConnections = 100000
	}
	
	if c.ConnectionTTL <= 0 {
		c.ConnectionTTL = 5 * time.Minute
	}
	
	if c.GraphUpdateInterval <= 0 {
		c.GraphUpdateInterval = 30 * time.Second
	}
	
	if c.MaxGraphNodes <= 0 {
		c.MaxGraphNodes = 500
	}
	
	return nil
}