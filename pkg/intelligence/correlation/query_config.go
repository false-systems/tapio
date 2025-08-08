package correlation

// QueryConfig defines configuration for graph queries
// This ensures all queries have proper limits to prevent unbounded results
type QueryConfig struct {
	// DefaultLimit is the default limit for queries if not specified
	DefaultLimit int

	// MaxLimit is the maximum allowed limit for any query
	MaxLimit int

	// ServiceQueryLimit is the limit for service-related queries
	ServiceQueryLimit int

	// PodQueryLimit is the limit for pod-related queries
	PodQueryLimit int

	// ConfigQueryLimit is the limit for config-related queries
	ConfigQueryLimit int

	// DependencyQueryLimit is the limit for dependency traversal queries
	DependencyQueryLimit int

	// OwnershipQueryLimit is the limit for ownership hierarchy queries
	OwnershipQueryLimit int

	// EnableQueryOptimization enables query optimization features
	EnableQueryOptimization bool

	// TimeoutSeconds is the query timeout in seconds
	TimeoutSeconds int
}

// DefaultQueryConfig returns production-ready query configuration
func DefaultQueryConfig() QueryConfig {
	return QueryConfig{
		DefaultLimit:            100,
		MaxLimit:                1000,
		ServiceQueryLimit:       100,
		PodQueryLimit:           200,
		ConfigQueryLimit:        50,
		DependencyQueryLimit:    150,
		OwnershipQueryLimit:     100,
		EnableQueryOptimization: true,
		TimeoutSeconds:          30,
	}
}

// DevelopmentQueryConfig returns query configuration for development
func DevelopmentQueryConfig() QueryConfig {
	return QueryConfig{
		DefaultLimit:            50,
		MaxLimit:                500,
		ServiceQueryLimit:       50,
		PodQueryLimit:           100,
		ConfigQueryLimit:        25,
		DependencyQueryLimit:    75,
		OwnershipQueryLimit:     50,
		EnableQueryOptimization: false,
		TimeoutSeconds:          60,
	}
}

// GetLimit returns the appropriate limit for a query type
func (c QueryConfig) GetLimit(queryType string) int {
	switch queryType {
	case "service":
		return c.ServiceQueryLimit
	case "pod":
		return c.PodQueryLimit
	case "config":
		return c.ConfigQueryLimit
	case "dependency":
		return c.DependencyQueryLimit
	case "ownership":
		return c.OwnershipQueryLimit
	default:
		return c.DefaultLimit
	}
}

// ValidateLimit ensures a limit is within acceptable bounds
func (c QueryConfig) ValidateLimit(limit int) int {
	if limit <= 0 {
		return c.DefaultLimit
	}
	if limit > c.MaxLimit {
		return c.MaxLimit
	}
	return limit
}
