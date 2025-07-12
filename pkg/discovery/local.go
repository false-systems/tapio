package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LocalService represents a locally discovered service
type LocalService struct {
	// Core identification
	ID      string
	Name    string
	Process string
	PID     int

	// Network details
	Address   string
	Port      int
	Protocol  string
	Interface string

	// Service details
	Type        string
	Command     string
	CommandArgs []string
	WorkingDir  string
	User        string

	// Discovery context
	DiscoveredAt time.Time
	LastSeen     time.Time

	// Health and validation
	Health    HealthStatus
	Endpoints []Endpoint

	// Metadata
	Labels      map[string]string
	Annotations map[string]string

	// Performance metrics
	CPUUsage    float64
	MemoryUsage int64
	Connections int
}

// Implement ServiceType interface
func (ls LocalService) GetID() string            { return ls.ID }
func (ls LocalService) GetType() string          { return "local-service" }
func (ls LocalService) GetEndpoints() []Endpoint { return ls.Endpoints }
func (ls LocalService) GetMetadata() map[string]string {
	metadata := make(map[string]string)
	metadata["process"] = ls.Process
	metadata["pid"] = strconv.Itoa(ls.PID)
	metadata["protocol"] = ls.Protocol
	metadata["interface"] = ls.Interface
	metadata["command"] = ls.Command
	metadata["working_dir"] = ls.WorkingDir
	metadata["user"] = ls.User

	// Add labels and annotations
	for k, v := range ls.Labels {
		metadata["label."+k] = v
	}
	for k, v := range ls.Annotations {
		metadata["annotation."+k] = v
	}

	return metadata
}

// LocalDiscovery implements Discovery interface for local services
type LocalDiscovery struct {
	// Dependencies
	logger     *slog.Logger
	workerPool WorkerPool
	cache      Cache
	validator  Validator

	// Configuration
	config LocalConfig

	// State management
	mu          sync.RWMutex
	healthy     bool
	stats       LocalStats
	scanners    map[string]Scanner
	activeScans int64

	// Port scanning state
	portRanges   []PortRange
	interfaceMap map[string][]net.Interface
}

// LocalConfig configures local service discovery
type LocalConfig struct {
	// Scanning behavior
	ScanInterval    time.Duration
	Timeout         time.Duration
	ConcurrentScans int

	// Port scanning
	PortRanges  []PortRange
	TcpPorts    []int
	UdpPorts    []int
	CommonPorts bool

	// Process discovery
	EnableProcessScan bool
	ProcessPatterns   []string

	// Network interfaces
	Interfaces   []string
	SkipLoopback bool
	SkipPrivate  bool

	// Performance
	WorkerPoolSize int
	CacheTTL       time.Duration
	MaxConcurrency int

	// Validation
	EnableValidation bool
	HealthCheckPorts []int
}

// PortRange defines a range of ports to scan
type PortRange struct {
	Start    int
	End      int
	Protocol string
}

// LocalStats tracks local discovery performance metrics
type LocalStats struct {
	ScanCount         int64
	ServicesFound     int64
	PortsScanned      int64
	ProcessesScanned  int64
	ValidationsPassed int64
	ValidationsFailed int64
	CacheHits         int64
	CacheMisses       int64
	LastScan          time.Time
	AverageScanTime   time.Duration
	TotalScanTime     time.Duration
}

// NewLocalDiscovery creates a new local service discovery instance
func NewLocalDiscovery(config LocalConfig, logger *slog.Logger) (*LocalDiscovery, error) {
	// Set defaults
	if config.ScanInterval == 0 {
		config.ScanInterval = 60 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}
	if config.ConcurrentScans == 0 {
		config.ConcurrentScans = 50
	}
	if config.WorkerPoolSize == 0 {
		config.WorkerPoolSize = 20
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 2 * time.Minute
	}
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = 100
	}

	// Set common ports if enabled
	if config.CommonPorts {
		config.TcpPorts = append(config.TcpPorts, getCommonTcpPorts()...)
		config.UdpPorts = append(config.UdpPorts, getCommonUdpPorts()...)
	}

	// Default port ranges
	if len(config.PortRanges) == 0 && len(config.TcpPorts) == 0 && len(config.UdpPorts) == 0 {
		config.PortRanges = []PortRange{
			{Start: 1, End: 1024, Protocol: "tcp"},
			{Start: 1, End: 1024, Protocol: "udp"},
		}
	}

	// Create dependencies
	workerPool := NewBoundedWorkerPool(config.WorkerPoolSize/2, config.WorkerPoolSize, 30*time.Second)
	cache := NewTTLCache(5000, config.CacheTTL)
	validator := NewLocalValidator(logger)

	// Initialize interface map
	interfaceMap, err := buildInterfaceMap(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build interface map: %w", err)
	}

	ld := &LocalDiscovery{
		logger:       logger,
		workerPool:   workerPool,
		cache:        cache,
		validator:    validator,
		config:       config,
		healthy:      true,
		scanners:     make(map[string]Scanner),
		portRanges:   config.PortRanges,
		interfaceMap: interfaceMap,
	}

	// Initialize scanners
	if err := ld.initializeScanners(); err != nil {
		return nil, fmt.Errorf("failed to initialize scanners: %w", err)
	}

	return ld, nil
}

// Discover performs local service discovery
func (ld *LocalDiscovery) Discover(ctx context.Context, opts DiscoveryOptions) ([]LocalService, error) {
	start := time.Now()
	defer func() {
		ld.updateStats(time.Since(start))
	}()

	atomic.AddInt64(&ld.activeScans, 1)
	defer atomic.AddInt64(&ld.activeScans, -1)

	ld.logger.Debug("Starting local service discovery",
		"timeout", opts.Timeout,
		"concurrency", opts.Concurrency,
		"cache_enabled", opts.EnableCache)

	// Check cache first if enabled
	if opts.EnableCache {
		cacheKey := ld.buildCacheKey(opts)
		if cached, found := ld.cache.Get(ctx, cacheKey); found {
			ld.stats.CacheHits++
			services := cached.([]LocalService)
			ld.logger.Debug("Cache hit for local discovery", "services", len(services))
			return services, nil
		}
		ld.stats.CacheMisses++
	}

	// Perform parallel discovery
	services, err := ld.performParallelDiscovery(ctx, opts)
	if err != nil {
		ld.logger.Error("Local discovery failed", "error", err)
		return nil, fmt.Errorf("discovery failed: %w", err)
	}

	// Validate services if requested
	if opts.EnableValidation && ld.validator != nil {
		services = ld.validateServices(ctx, services)
	}

	// Cache results if enabled
	if opts.EnableCache {
		cacheKey := ld.buildCacheKey(opts)
		ttl := opts.CacheTTL
		if ttl == 0 {
			ttl = ld.config.CacheTTL
		}
		ld.cache.Set(ctx, cacheKey, services, ttl)
	}

	ld.stats.ServicesFound += int64(len(services))
	ld.logger.Info("Local discovery completed",
		"services_found", len(services),
		"duration", time.Since(start))

	return services, nil
}

// DiscoverStream provides continuous local service discovery
func (ld *LocalDiscovery) DiscoverStream(ctx context.Context, opts DiscoveryOptions) (<-chan DiscoveryResult[LocalService], error) {
	resultCh := make(chan DiscoveryResult[LocalService], 100)

	go func() {
		defer close(resultCh)

		ticker := time.NewTicker(ld.config.ScanInterval)
		defer ticker.Stop()

		// Initial discovery
		ld.performStreamDiscovery(ctx, opts, resultCh)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ld.performStreamDiscovery(ctx, opts, resultCh)
			}
		}
	}()

	return resultCh, nil
}

// performParallelDiscovery executes discovery using multiple strategies in parallel
func (ld *LocalDiscovery) performParallelDiscovery(ctx context.Context, opts DiscoveryOptions) ([]LocalService, error) {
	var (
		allServices []LocalService
		mu          sync.Mutex
		wg          sync.WaitGroup
		errs        []error
	)

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Port scanning
	wg.Add(1)
	ld.workerPool.Submit(timeoutCtx, func(workerCtx context.Context) error {
		defer wg.Done()

		services, err := ld.performPortScan(workerCtx, opts)
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("port scan failed: %w", err))
			mu.Unlock()
			return nil
		}

		mu.Lock()
		allServices = append(allServices, services...)
		mu.Unlock()

		return nil
	})

	// Process scanning if enabled
	if ld.config.EnableProcessScan {
		wg.Add(1)
		ld.workerPool.Submit(timeoutCtx, func(workerCtx context.Context) error {
			defer wg.Done()

			services, err := ld.performProcessScan(workerCtx, opts)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("process scan failed: %w", err))
				mu.Unlock()
				return nil
			}

			mu.Lock()
			allServices = append(allServices, services...)
			mu.Unlock()

			return nil
		})
	}

	// HTTP service discovery
	wg.Add(1)
	ld.workerPool.Submit(timeoutCtx, func(workerCtx context.Context) error {
		defer wg.Done()

		services, err := ld.performHttpDiscovery(workerCtx, opts)
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Errorf("HTTP discovery failed: %w", err))
			mu.Unlock()
			return nil
		}

		mu.Lock()
		allServices = append(allServices, services...)
		mu.Unlock()

		return nil
	})

	// Wait for all discovery methods to complete
	wg.Wait()

	// Log any errors but don't fail entirely
	if len(errs) > 0 {
		for _, err := range errs {
			ld.logger.Warn("Discovery method failed", "error", err)
		}
	}

	// Deduplicate services
	services := ld.deduplicateServices(allServices)

	// Apply filters
	services = ld.applyFilters(services, opts.Filters)

	return services, nil
}

// performPortScan scans for services listening on ports
func (ld *LocalDiscovery) performPortScan(ctx context.Context, opts DiscoveryOptions) ([]LocalService, error) {
	var (
		services []LocalService
		mu       sync.Mutex
		wg       sync.WaitGroup
	)

	// Create semaphore for concurrency control
	sem := make(chan struct{}, ld.config.MaxConcurrency)

	// Scan TCP ports
	for _, portRange := range ld.portRanges {
		if portRange.Protocol == "tcp" || portRange.Protocol == "" {
			for port := portRange.Start; port <= portRange.End; port++ {
				wg.Add(1)

				ld.workerPool.Submit(ctx, func(workerCtx context.Context) error {
					defer wg.Done()

					// Acquire semaphore
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-workerCtx.Done():
						return workerCtx.Err()
					}

					service := ld.scanTcpPort(workerCtx, port)
					if service != nil {
						mu.Lock()
						services = append(services, *service)
						mu.Unlock()
					}

					return nil
				})
			}
		}
	}

	// Scan specific TCP ports
	for _, port := range ld.config.TcpPorts {
		wg.Add(1)

		ld.workerPool.Submit(ctx, func(workerCtx context.Context) error {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-workerCtx.Done():
				return workerCtx.Err()
			}

			service := ld.scanTcpPort(workerCtx, port)
			if service != nil {
				mu.Lock()
				services = append(services, *service)
				mu.Unlock()
			}

			return nil
		})
	}

	// Wait for all port scans to complete
	wg.Wait()

	ld.stats.PortsScanned += int64(len(ld.config.TcpPorts) + len(ld.config.UdpPorts))

	return services, nil
}

// scanTcpPort scans a specific TCP port
func (ld *LocalDiscovery) scanTcpPort(ctx context.Context, port int) *LocalService {
	// Try connecting to localhost
	addresses := []string{"127.0.0.1", "localhost"}

	// Add configured interfaces
	for _, ifaces := range ld.interfaceMap {
		for _, iface := range ifaces {
			if addrs, err := iface.Addrs(); err == nil {
				for _, addr := range addrs {
					if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
						addresses = append(addresses, ipnet.IP.String())
					}
				}
			}
		}
	}

	for _, address := range addresses {
		target := net.JoinHostPort(address, strconv.Itoa(port))

		// Set short timeout for port scanning
		conn, err := net.DialTimeout("tcp", target, time.Second)
		if err != nil {
			continue
		}
		conn.Close()

		// Port is open, create service
		service := &LocalService{
			ID:           fmt.Sprintf("tcp-%s-%d", address, port),
			Name:         fmt.Sprintf("tcp-service-%d", port),
			Address:      address,
			Port:         port,
			Protocol:     "tcp",
			Type:         "tcp-service",
			DiscoveredAt: time.Now(),
			LastSeen:     time.Now(),
			Health:       HealthHealthy,
		}

		// Build endpoint
		endpoint := Endpoint{
			Address:  address,
			Port:     port,
			Protocol: "tcp",
			Metadata: map[string]string{
				"discovery_method": "port_scan",
			},
		}
		service.Endpoints = []Endpoint{endpoint}

		// Try to identify service type
		service.Type = ld.identifyServiceType(port, "tcp")
		service.Name = fmt.Sprintf("%s-%d", service.Type, port)

		return service
	}

	return nil
}

// performProcessScan discovers services by scanning running processes
func (ld *LocalDiscovery) performProcessScan(ctx context.Context, opts DiscoveryOptions) ([]LocalService, error) {
	// This would integrate with a process scanning library like gopsutil
	// For now, return empty slice as this requires platform-specific implementation
	ld.logger.Debug("Process scanning not yet implemented")
	return []LocalService{}, nil
}

// performHttpDiscovery discovers HTTP services
func (ld *LocalDiscovery) performHttpDiscovery(ctx context.Context, opts DiscoveryOptions) ([]LocalService, error) {
	var services []LocalService

	// Common HTTP ports to check
	httpPorts := []int{80, 443, 8080, 8443, 9090, 9000, 3000}

	for _, port := range httpPorts {
		service := ld.checkHttpService(ctx, "127.0.0.1", port)
		if service != nil {
			services = append(services, *service)
		}
	}

	return services, nil
}

// checkHttpService checks if an HTTP service is running on the given address and port
func (ld *LocalDiscovery) checkHttpService(ctx context.Context, address string, port int) *LocalService {
	// Try both HTTP and HTTPS
	schemes := []string{"http", "https"}

	for _, scheme := range schemes {
		url := fmt.Sprintf("%s://%s:%d", scheme, address, port)

		client := &http.Client{
			Timeout: 2 * time.Second,
		}

		req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
		if err != nil {
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		// HTTP service found
		service := &LocalService{
			ID:           fmt.Sprintf("http-%s-%d", address, port),
			Name:         fmt.Sprintf("http-service-%d", port),
			Address:      address,
			Port:         port,
			Protocol:     scheme,
			Type:         "http-service",
			DiscoveredAt: time.Now(),
			LastSeen:     time.Now(),
			Health:       HealthHealthy,
		}

		// Build endpoint
		endpoint := Endpoint{
			Address:  address,
			Port:     port,
			Protocol: scheme,
			Secure:   scheme == "https",
			Path:     "/",
			Metadata: map[string]string{
				"discovery_method": "http_check",
				"status_code":      strconv.Itoa(resp.StatusCode),
			},
		}

		// Add server header if available
		if server := resp.Header.Get("Server"); server != "" {
			endpoint.Metadata["server"] = server
			service.Type = ld.identifyHttpServiceType(server)
		}

		service.Endpoints = []Endpoint{endpoint}

		return service
	}

	return nil
}

// Helper functions

func (ld *LocalDiscovery) identifyServiceType(port int, protocol string) string {
	knownPorts := map[int]string{
		22:    "ssh",
		25:    "smtp",
		53:    "dns",
		80:    "http",
		110:   "pop3",
		143:   "imap",
		443:   "https",
		993:   "imaps",
		995:   "pop3s",
		3306:  "mysql",
		5432:  "postgresql",
		6379:  "redis",
		27017: "mongodb",
		9200:  "elasticsearch",
		5672:  "rabbitmq",
	}

	if serviceType, exists := knownPorts[port]; exists {
		return serviceType
	}

	return fmt.Sprintf("%s-service", protocol)
}

func (ld *LocalDiscovery) identifyHttpServiceType(server string) string {
	server = strings.ToLower(server)

	if strings.Contains(server, "nginx") {
		return "nginx"
	}
	if strings.Contains(server, "apache") {
		return "apache"
	}
	if strings.Contains(server, "prometheus") {
		return "prometheus"
	}
	if strings.Contains(server, "grafana") {
		return "grafana"
	}

	return "http-service"
}

func (ld *LocalDiscovery) deduplicateServices(services []LocalService) []LocalService {
	seen := make(map[string]bool)
	var unique []LocalService

	for _, service := range services {
		key := fmt.Sprintf("%s:%d:%s", service.Address, service.Port, service.Protocol)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, service)
		}
	}

	return unique
}

func (ld *LocalDiscovery) validateServices(ctx context.Context, services []LocalService) []LocalService {
	if len(services) == 0 {
		return services
	}

	// Convert to ServiceInfo for validation
	serviceInfos := make([]ServiceInfo, len(services))
	for i, svc := range services {
		serviceInfos[i] = ServiceInfo{
			ID:        svc.ID,
			Name:      svc.Name,
			Type:      svc.Type,
			Endpoints: svc.Endpoints,
			Metadata:  svc.GetMetadata(),
		}
	}

	// Perform batch validation
	validationResults := ld.validator.ValidateBatch(ctx, serviceInfos)

	// Update services with validation results
	for i, result := range validationResults.Results {
		if i < len(services) {
			if result.Valid {
				services[i].Health = HealthHealthy
				ld.stats.ValidationsPassed++
			} else {
				services[i].Health = HealthUnhealthy
				ld.stats.ValidationsFailed++
			}
		}
	}

	return services
}

func (ld *LocalDiscovery) applyFilters(services []LocalService, filters []DiscoveryFilter) []LocalService {
	if len(filters) == 0 {
		return services
	}

	filtered := make([]LocalService, 0, len(services))

	for _, service := range services {
		include := true

		for _, filter := range filters {
			if f, ok := filter.(func(LocalService) bool); ok {
				if !f(service) {
					include = false
					break
				}
			}
		}

		if include {
			filtered = append(filtered, service)
		}
	}

	return filtered
}

func (ld *LocalDiscovery) buildCacheKey(opts DiscoveryOptions) CacheKey {
	key := fmt.Sprintf("local-discovery-%v-%s",
		opts.Labels,
		ld.config.Interfaces)

	return CacheKey{
		Namespace: "local",
		Key:       key,
		Version:   "v1",
	}
}

func (ld *LocalDiscovery) performStreamDiscovery(ctx context.Context, opts DiscoveryOptions, resultCh chan<- DiscoveryResult[LocalService]) {
	start := time.Now()

	services, err := ld.performParallelDiscovery(ctx, opts)
	duration := time.Since(start)

	result := DiscoveryResult[LocalService]{
		Services:  services,
		Error:     err,
		Timestamp: start,
		Duration:  duration,
		Source:    "local",
		Metadata: map[string]interface{}{
			"scan_types":         []string{"port", "process", "http"},
			"cache_enabled":      opts.EnableCache,
			"validation_enabled": opts.EnableValidation,
			"active_scans":       atomic.LoadInt64(&ld.activeScans),
		},
	}

	select {
	case resultCh <- result:
	case <-ctx.Done():
	}
}

func (ld *LocalDiscovery) updateStats(duration time.Duration) {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	ld.stats.ScanCount++
	ld.stats.LastScan = time.Now()
	ld.stats.TotalScanTime += duration

	// Update rolling average
	if ld.stats.AverageScanTime == 0 {
		ld.stats.AverageScanTime = duration
	} else {
		// Simple moving average
		ld.stats.AverageScanTime = (ld.stats.AverageScanTime + duration) / 2
	}
}

func (ld *LocalDiscovery) initializeScanners() error {
	// Initialize any additional scanners here
	return nil
}

// Validate ensures discovered services are reachable and healthy
func (ld *LocalDiscovery) Validate(ctx context.Context, services []LocalService) ValidationResults {
	serviceInfos := make([]ServiceInfo, len(services))
	for i, svc := range services {
		serviceInfos[i] = ServiceInfo{
			ID:        svc.ID,
			Name:      svc.Name,
			Type:      svc.Type,
			Endpoints: svc.Endpoints,
			Metadata:  svc.GetMetadata(),
		}
	}

	return ld.validator.ValidateBatch(ctx, serviceInfos)
}

// Health returns the current health status of the discovery system
func (ld *LocalDiscovery) Health() HealthStatus {
	ld.mu.RLock()
	defer ld.mu.RUnlock()

	if !ld.healthy {
		return HealthUnhealthy
	}

	// Check if too many active scans
	activeScans := atomic.LoadInt64(&ld.activeScans)
	if activeScans > int64(ld.config.MaxConcurrency) {
		return HealthDegraded
	}

	return HealthHealthy
}

// Helper functions for configuration

func getCommonTcpPorts() []int {
	return []int{
		21, 22, 23, 25, 53, 80, 110, 143, 443, 993, 995,
		3306, 5432, 6379, 9200, 27017, 8080, 8443, 9090,
	}
}

func getCommonUdpPorts() []int {
	return []int{
		53, 67, 68, 69, 123, 161, 162, 514,
	}
}

func buildInterfaceMap(config LocalConfig) (map[string][]net.Interface, error) {
	interfaceMap := make(map[string][]net.Interface)

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, iface := range interfaces {
		// Skip loopback if configured
		if config.SkipLoopback && iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Filter by configured interfaces
		if len(config.Interfaces) > 0 {
			found := false
			for _, name := range config.Interfaces {
				if iface.Name == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		interfaceMap[iface.Name] = append(interfaceMap[iface.Name], iface)
	}

	return interfaceMap, nil
}
