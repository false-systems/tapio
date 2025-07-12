package discovery_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/yairfalse/tapio/pkg/discovery"
)

// ExampleLocalDiscovery demonstrates local service discovery usage
func ExampleLocalDiscovery() {
	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Configure local discovery
	config := discovery.LocalConfig{
		ScanInterval:      30 * time.Second,
		Timeout:           5 * time.Second,
		ConcurrentScans:   20,
		CommonPorts:       true,
		EnableProcessScan: false,
		SkipLoopback:      false,
		WorkerPoolSize:    10,
		CacheTTL:          2 * time.Minute,
		MaxConcurrency:    50,
		EnableValidation:  true,
		TcpPorts:          []int{80, 443, 8080, 9090}, // Custom ports
	}

	// Create discovery instance
	localDiscovery, err := discovery.NewLocalDiscovery(config, logger)
	if err != nil {
		fmt.Printf("Failed to create local discovery: %v\n", err)
		return
	}

	// Set up discovery options
	opts := discovery.DiscoveryOptions{
		Timeout:          10 * time.Second,
		Concurrency:      10,
		EnableCache:      true,
		CacheTTL:         5 * time.Minute,
		EnableValidation: true,
	}

	// Perform discovery
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	services, err := localDiscovery.Discover(ctx, opts)
	if err != nil {
		fmt.Printf("Discovery failed: %v\n", err)
		return
	}

	// Display results
	fmt.Printf("Found %d local services:\n", len(services))
	for _, service := range services {
		fmt.Printf("  - %s (%s) at %s:%d [%s]\n",
			service.Name,
			service.Type,
			service.Address,
			service.Port,
			service.Health)

		for _, endpoint := range service.Endpoints {
			fmt.Printf("    Endpoint: %s://%s:%d%s\n",
				endpoint.Protocol,
				endpoint.Address,
				endpoint.Port,
				endpoint.Path)
		}
	}

	// Check discovery health
	health := localDiscovery.Health()
	fmt.Printf("Discovery system health: %s\n", health)

	// Output format will vary based on what services are running locally
}

// ExampleLocalDiscovery_streaming demonstrates continuous service discovery
func ExampleLocalDiscovery_streaming() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn, // Reduce noise for example
	}))

	config := discovery.LocalConfig{
		ScanInterval:     5 * time.Second, // Frequent scanning for demo
		Timeout:          3 * time.Second,
		ConcurrentScans:  10,
		CommonPorts:      true,
		WorkerPoolSize:   5,
		EnableValidation: false, // Skip validation for faster scanning
	}

	localDiscovery, err := discovery.NewLocalDiscovery(config, logger)
	if err != nil {
		fmt.Printf("Failed to create discovery: %v\n", err)
		return
	}

	opts := discovery.DiscoveryOptions{
		Timeout:     5 * time.Second,
		Concurrency: 5,
		EnableCache: true,
		CacheTTL:    30 * time.Second,
	}

	// Start streaming discovery
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resultCh, err := localDiscovery.DiscoverStream(ctx, opts)
	if err != nil {
		fmt.Printf("Failed to start streaming discovery: %v\n", err)
		return
	}

	// Process discovery results
	scanCount := 0
	for result := range resultCh {
		scanCount++

		if result.Error != nil {
			fmt.Printf("Scan %d failed: %v\n", scanCount, result.Error)
			continue
		}

		fmt.Printf("Scan %d completed in %v: found %d services\n",
			scanCount,
			result.Duration,
			len(result.Services))

		// Show first few services
		for i, service := range result.Services {
			if i >= 3 {
				fmt.Printf("  ... and %d more\n", len(result.Services)-3)
				break
			}
			fmt.Printf("  - %s:%d (%s)\n",
				service.Address,
				service.Port,
				service.Type)
		}

		if scanCount >= 3 {
			break // Limit example output
		}
	}

	fmt.Printf("Completed %d discovery scans\n", scanCount)
}

// ExampleHealthCheckValidator_validation demonstrates service validation
func ExampleHealthCheckValidator_validation() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Create validator
	config := discovery.ValidatorConfig{
		ConnectionTimeout: 3 * time.Second,
		HTTPTimeout:       5 * time.Second,
		MaxRetries:        2,
		MaxConcurrent:     10,
		EnableTCPCheck:    true,
		EnableHTTPCheck:   true,
		EnableDNSCheck:    true,
	}

	validator := discovery.NewHealthCheckValidator(config, logger)

	// Create test services
	services := []discovery.ServiceInfo{
		{
			ID:   "localhost-web",
			Name: "Local Web Server",
			Type: "http",
			Endpoints: []discovery.Endpoint{
				{
					Address:  "127.0.0.1",
					Port:     80,
					Protocol: "http",
					Path:     "/",
				},
			},
		},
		{
			ID:   "localhost-ssh",
			Name: "SSH Server",
			Type: "ssh",
			Endpoints: []discovery.Endpoint{
				{
					Address:  "127.0.0.1",
					Port:     22,
					Protocol: "tcp",
				},
			},
		},
		{
			ID:   "google-dns",
			Name: "Google DNS",
			Type: "dns",
			Endpoints: []discovery.Endpoint{
				{
					Address:  "8.8.8.8",
					Port:     53,
					Protocol: "udp",
				},
			},
		},
	}

	// Validate services
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := validator.ValidateBatch(ctx, services)

	fmt.Printf("Validation Results:\n")
	fmt.Printf("Total: %d, Valid: %d, Invalid: %d, Errors: %d\n",
		results.Summary.Total,
		results.Summary.Valid,
		results.Summary.Invalid,
		results.Summary.Errors)
	fmt.Printf("Average validation time: %v\n", results.Summary.AvgTime)

	for _, result := range results.Results {
		status := "❌ INVALID"
		if result.Valid {
			status = "✅ VALID"
		}

		fmt.Printf("  %s - %s (%v)\n",
			result.ServiceID,
			status,
			result.ResponseTime)

		if result.Error != nil {
			fmt.Printf("    Error: %v\n", result.Error)
		}
	}
}

// ExampleHealthCheckValidator_comprehensive demonstrates comprehensive health checking
func ExampleHealthCheckValidator_comprehensive() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := discovery.ValidatorConfig{
		ConnectionTimeout: 2 * time.Second,
		HTTPTimeout:       5 * time.Second,
		EnableTCPCheck:    true,
		EnableHTTPCheck:   true,
		EnableTLSCheck:    true,
		EnableDNSCheck:    true,
	}

	validator := discovery.NewHealthCheckValidator(config, logger)

	// Test a web service
	service := discovery.ServiceInfo{
		ID:   "example-web-service",
		Name: "Example Web Service",
		Type: "web",
		Endpoints: []discovery.Endpoint{
			{
				Address:  "httpbin.org",
				Port:     80,
				Protocol: "http",
				Path:     "/status/200",
			},
			{
				Address:  "httpbin.org",
				Port:     443,
				Protocol: "https",
				Path:     "/status/200",
				Secure:   true,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	healthResult := validator.HealthCheck(ctx, service)

	fmt.Printf("Health Check Results for %s:\n", healthResult.ServiceID)
	fmt.Printf("Overall Health: %t (Score: %d/100)\n",
		healthResult.Healthy,
		healthResult.Score)

	for checkName, checkResult := range healthResult.Checks {
		status := "❌ FAILED"
		if checkResult.Passed {
			status = "✅ PASSED"
		}

		fmt.Printf("  %s: %s - %s\n",
			checkName,
			status,
			checkResult.Message)

		if len(checkResult.Data) > 0 {
			for key, value := range checkResult.Data {
				fmt.Printf("    %s: %v\n", key, value)
			}
		}
	}
}

// ExampleTTLCache demonstrates the caching capabilities
func ExampleTTLCache() {
	// Create cache
	cache := discovery.NewTTLCache(1000, 5*time.Minute)
	defer cache.Stop()

	ctx := context.Background()

	// Store some services in cache
	services := []discovery.LocalService{
		{
			ID:       "cache-service-1",
			Name:     "Cached Service 1",
			Address:  "127.0.0.1",
			Port:     8080,
			Protocol: "http",
			Type:     "web-service",
		},
		{
			ID:       "cache-service-2",
			Name:     "Cached Service 2",
			Address:  "127.0.0.1",
			Port:     9090,
			Protocol: "tcp",
			Type:     "api-service",
		},
	}

	// Cache the services
	key := discovery.CacheKey{
		Namespace: "local-discovery",
		Key:       "test-scan",
		Version:   "v1",
	}

	err := cache.Set(ctx, key, services, 1*time.Minute)
	if err != nil {
		fmt.Printf("Failed to cache services: %v\n", err)
		return
	}

	// Retrieve from cache
	cached, found := cache.Get(ctx, key)
	if found {
		cachedServices := cached.([]discovery.LocalService)
		fmt.Printf("Retrieved %d services from cache:\n", len(cachedServices))

		for _, service := range cachedServices {
			fmt.Printf("  - %s at %s:%d\n",
				service.Name,
				service.Address,
				service.Port)
		}
	} else {
		fmt.Println("Services not found in cache")
	}

	// Show cache statistics
	stats := cache.Stats()
	fmt.Printf("\nCache Statistics:\n")
	fmt.Printf("  Hit Rate: %.2f%%\n", stats.HitRate*100)
	fmt.Printf("  Miss Rate: %.2f%%\n", stats.MissRate*100)
	fmt.Printf("  Total Entries: %d\n", stats.Entries)
	fmt.Printf("  Cache Size: %d bytes\n", stats.Size)
	fmt.Printf("  Evictions: %d\n", stats.Evictions)
}

// ExampleCircuitBreaker demonstrates resilient discovery with circuit breaker
func ExampleCircuitBreaker() {
	cb := discovery.NewCircuitBreaker(3, 5*time.Second)

	fmt.Println("Circuit Breaker Example:")

	// Simulate successful operations
	for i := 0; i < 3; i++ {
		err := cb.Execute(context.Background(), func() error {
			return nil // Success
		})

		if err != nil {
			fmt.Printf("Unexpected error: %v\n", err)
		} else {
			fmt.Printf("Operation %d: Success (State: %s)\n", i+1, cb.State())
		}
	}

	// Simulate failures to open circuit
	fmt.Println("\nSimulating failures:")
	for i := 0; i < 4; i++ {
		err := cb.Execute(context.Background(), func() error {
			return fmt.Errorf("simulated failure")
		})

		fmt.Printf("Operation %d: %v (State: %s)\n", i+1, err, cb.State())
	}

	// Try operation when circuit is open
	fmt.Println("\nTrying operation with open circuit:")
	err := cb.Execute(context.Background(), func() error {
		fmt.Println("This should not execute")
		return nil
	})
	fmt.Printf("Result: %v (State: %s)\n", err, cb.State())

	// Demonstrate fallback
	fmt.Println("\nUsing fallback:")
	err = cb.ExecuteWithFallback(
		context.Background(),
		func() error {
			return fmt.Errorf("primary failed")
		},
		func() error {
			fmt.Println("Fallback executed successfully")
			return nil
		},
	)
	fmt.Printf("Fallback result: %v\n", err)
}

// ExampleLocalDiscovery_advanced demonstrates advanced discovery patterns
func ExampleLocalDiscovery_advanced() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	// Create local discovery with custom configuration
	config := discovery.LocalConfig{
		ScanInterval:     15 * time.Second,
		Timeout:          5 * time.Second,
		ConcurrentScans:  30,
		WorkerPoolSize:   15,
		CacheTTL:         3 * time.Minute,
		MaxConcurrency:   100,
		EnableValidation: true,

		// Custom port ranges
		PortRanges: []discovery.PortRange{
			{Start: 8000, End: 8010, Protocol: "tcp"},
			{Start: 9000, End: 9010, Protocol: "tcp"},
		},

		// Specific ports
		TcpPorts: []int{22, 80, 443, 3000, 5000},

		// Network interface configuration
		SkipLoopback: false,
		SkipPrivate:  false,
	}

	localDiscovery, err := discovery.NewLocalDiscovery(config, logger)
	if err != nil {
		fmt.Printf("Failed to create discovery: %v\n", err)
		return
	}

	// Custom discovery options with filters
	opts := discovery.DiscoveryOptions{
		Timeout:          10 * time.Second,
		Concurrency:      20,
		EnableCache:      true,
		CacheTTL:         5 * time.Minute,
		EnableValidation: true,

		// Custom filters (these would be implemented as functions)
		Filters: []discovery.DiscoveryFilter{
			// Example: Filter for only HTTP services
			func(service discovery.LocalService) bool {
				return service.Type == "http-service" ||
					service.Type == "nginx" ||
					service.Type == "apache"
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	services, err := localDiscovery.Discover(ctx, opts)
	if err != nil {
		fmt.Printf("Discovery failed: %v\n", err)
		return
	}

	fmt.Printf("Advanced Discovery Results:\n")
	fmt.Printf("Found %d services matching criteria\n", len(services))

	// Group services by type
	servicesByType := make(map[string][]discovery.LocalService)
	for _, service := range services {
		servicesByType[service.Type] = append(servicesByType[service.Type], service)
	}

	for serviceType, typeServices := range servicesByType {
		fmt.Printf("\n%s services (%d):\n", serviceType, len(typeServices))
		for _, service := range typeServices {
			fmt.Printf("  - %s at %s:%d [%s]\n",
				service.Name,
				service.Address,
				service.Port,
				service.Health)
		}
	}

	// Validate specific services
	if len(services) > 0 {
		fmt.Printf("\nValidating first service...\n")

		localServices := []discovery.LocalService{services[0]}

		validationResults := localDiscovery.Validate(ctx, localServices)

		for _, result := range validationResults.Results {
			if result.Valid {
				fmt.Printf("  ✅ %s is reachable (%v)\n",
					result.ServiceID,
					result.ResponseTime)
			} else {
				fmt.Printf("  ❌ %s is not reachable: %v\n",
					result.ServiceID,
					result.Error)
			}
		}
	}

	// Check system health
	health := localDiscovery.Health()
	fmt.Printf("\nDiscovery system health: %s\n", health)
}
