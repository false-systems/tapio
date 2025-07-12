package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
)

// HealthCheckValidator implements Validator interface with comprehensive health checking
type HealthCheckValidator struct {
	logger     *slog.Logger
	workerPool WorkerPool

	// HTTP client for web service validation
	httpClient *http.Client

	// Configuration
	config ValidatorConfig

	// Metrics
	mu    sync.RWMutex
	stats ValidatorStats
}

// ValidatorConfig configures validation behavior
type ValidatorConfig struct {
	// Timeouts
	ConnectionTimeout time.Duration
	ReadTimeout       time.Duration

	// HTTP validation
	HTTPTimeout     time.Duration
	FollowRedirects bool
	MaxRedirects    int

	// TCP validation
	TCPTimeout   time.Duration
	TCPKeepAlive time.Duration

	// Retry behavior
	MaxRetries        int
	RetryDelay        time.Duration
	BackoffMultiplier float64

	// Concurrency
	MaxConcurrent int

	// Health check types
	EnableTCPCheck  bool
	EnableHTTPCheck bool
	EnableTLSCheck  bool
	EnableDNSCheck  bool
}

// ValidatorStats tracks validation performance
type ValidatorStats struct {
	TotalValidations      int64
	SuccessfulValidations int64
	FailedValidations     int64
	AverageLatency        time.Duration
	LastValidation        time.Time

	// By validation type
	TCPChecks  int64
	HTTPChecks int64
	TLSChecks  int64
	DNSChecks  int64
}

// NewHealthCheckValidator creates a new health check validator
func NewHealthCheckValidator(config ValidatorConfig, logger *slog.Logger) *HealthCheckValidator {
	// Set defaults
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 5 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 10 * time.Second
	}
	if config.HTTPTimeout == 0 {
		config.HTTPTimeout = 10 * time.Second
	}
	if config.TCPTimeout == 0 {
		config.TCPTimeout = 5 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 1 * time.Second
	}
	if config.BackoffMultiplier == 0 {
		config.BackoffMultiplier = 2.0
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 50
	}
	if config.MaxRedirects == 0 {
		config.MaxRedirects = 5
	}

	// Enable all checks by default
	if !config.EnableTCPCheck && !config.EnableHTTPCheck && !config.EnableTLSCheck && !config.EnableDNSCheck {
		config.EnableTCPCheck = true
		config.EnableHTTPCheck = true
		config.EnableTLSCheck = true
		config.EnableDNSCheck = true
	}

	// Create HTTP client with custom transport
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   config.ConnectionTimeout,
			KeepAlive: config.TCPKeepAlive,
		}).DialContext,
		TLSHandshakeTimeout:   config.ConnectionTimeout,
		ResponseHeaderTimeout: config.ReadTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
	}

	var checkRedirect func(req *http.Request, via []*http.Request) error
	if !config.FollowRedirects {
		checkRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		checkRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= config.MaxRedirects {
				return fmt.Errorf("too many redirects")
			}
			return nil
		}
	}

	httpClient := &http.Client{
		Transport:     transport,
		Timeout:       config.HTTPTimeout,
		CheckRedirect: checkRedirect,
	}

	workerPool := NewBoundedWorkerPool(config.MaxConcurrent/2, config.MaxConcurrent, 30*time.Second)

	return &HealthCheckValidator{
		logger:     logger,
		workerPool: workerPool,
		httpClient: httpClient,
		config:     config,
	}
}

// ValidateConnection tests if a service is reachable
func (v *HealthCheckValidator) ValidateConnection(ctx context.Context, service ServiceInfo) ValidationResult {
	start := time.Now()

	result := ValidationResult{
		ServiceID: service.ID,
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}

	defer func() {
		result.ResponseTime = time.Since(start)
		v.updateStats(result)
	}()

	// Validate each endpoint
	var errors []error
	var validEndpoints int

	for _, endpoint := range service.Endpoints {
		if err := v.validateEndpoint(ctx, endpoint, &result); err != nil {
			errors = append(errors, err)
			v.logger.Debug("Endpoint validation failed",
				"service", service.ID,
				"endpoint", fmt.Sprintf("%s://%s:%d", endpoint.Protocol, endpoint.Address, endpoint.Port),
				"error", err)
		} else {
			validEndpoints++
		}
	}

	// Service is valid if at least one endpoint is reachable
	result.Valid = validEndpoints > 0

	if len(errors) > 0 && validEndpoints == 0 {
		result.Error = &ValidationError{
			ServiceID: service.ID,
			Errors:    errors,
		}
	}

	result.Details["valid_endpoints"] = validEndpoints
	result.Details["total_endpoints"] = len(service.Endpoints)
	result.Details["validation_methods"] = v.getEnabledMethods()

	return result
}

// ValidateBatch performs parallel validation of multiple services
func (v *HealthCheckValidator) ValidateBatch(ctx context.Context, services []ServiceInfo) ValidationResults {
	if len(services) == 0 {
		return ValidationResults{
			Summary: ValidationSummary{
				Total:   0,
				Valid:   0,
				Invalid: 0,
				Errors:  0,
			},
		}
	}

	start := time.Now()

	results := make([]ValidationResult, len(services))
	var wg sync.WaitGroup

	// Create semaphore for concurrency control
	sem := make(chan struct{}, v.config.MaxConcurrent)

	for i, service := range services {
		wg.Add(1)

		// Submit validation work
		v.workerPool.Submit(ctx, func(workerCtx context.Context) error {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-workerCtx.Done():
				return workerCtx.Err()
			}

			// Validate service
			results[i] = v.ValidateConnection(workerCtx, service)
			return nil
		})
	}

	// Wait for all validations to complete
	wg.Wait()

	// Calculate summary
	summary := v.calculateSummary(results)

	return ValidationResults{
		Results:  results,
		Summary:  summary,
		Duration: time.Since(start),
	}
}

// HealthCheck performs comprehensive health validation
func (v *HealthCheckValidator) HealthCheck(ctx context.Context, service ServiceInfo) HealthCheckResult {
	start := time.Now()

	result := HealthCheckResult{
		ServiceID: service.ID,
		Checks:    make(map[string]CheckResult),
		Timestamp: start,
	}

	var totalScore int
	var checkCount int

	// Perform different types of health checks
	for _, endpoint := range service.Endpoints {
		if v.config.EnableTCPCheck {
			checkResult := v.performTCPCheck(ctx, endpoint)
			result.Checks[fmt.Sprintf("tcp_%s_%d", endpoint.Address, endpoint.Port)] = checkResult
			if checkResult.Passed {
				totalScore += 25
			}
			checkCount++
		}

		if v.config.EnableHTTPCheck && (endpoint.Protocol == "http" || endpoint.Protocol == "https") {
			checkResult := v.performHTTPCheck(ctx, endpoint)
			result.Checks[fmt.Sprintf("http_%s_%d", endpoint.Address, endpoint.Port)] = checkResult
			if checkResult.Passed {
				totalScore += 25
			}
			checkCount++
		}

		if v.config.EnableTLSCheck && endpoint.Secure {
			checkResult := v.performTLSCheck(ctx, endpoint)
			result.Checks[fmt.Sprintf("tls_%s_%d", endpoint.Address, endpoint.Port)] = checkResult
			if checkResult.Passed {
				totalScore += 25
			}
			checkCount++
		}

		if v.config.EnableDNSCheck {
			checkResult := v.performDNSCheck(ctx, endpoint)
			result.Checks[fmt.Sprintf("dns_%s", endpoint.Address)] = checkResult
			if checkResult.Passed {
				totalScore += 25
			}
			checkCount++
		}
	}

	// Calculate overall health score
	if checkCount > 0 {
		result.Score = (totalScore * 100) / (checkCount * 25)
	}

	result.Healthy = result.Score >= 50 // At least 50% of checks must pass

	return result
}

// validateEndpoint validates a specific endpoint
func (v *HealthCheckValidator) validateEndpoint(ctx context.Context, endpoint Endpoint, result *ValidationResult) error {
	var lastErr error

	// Retry logic with exponential backoff
	delay := v.config.RetryDelay

	for attempt := 0; attempt <= v.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			delay = time.Duration(float64(delay) * v.config.BackoffMultiplier)
		}

		err := v.performEndpointValidation(ctx, endpoint, result)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		v.logger.Debug("Endpoint validation attempt failed",
			"endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port),
			"attempt", attempt+1,
			"error", err)
	}

	return lastErr
}

// performEndpointValidation performs the actual endpoint validation
func (v *HealthCheckValidator) performEndpointValidation(ctx context.Context, endpoint Endpoint, result *ValidationResult) error {
	switch endpoint.Protocol {
	case "tcp":
		return v.validateTCP(ctx, endpoint, result)
	case "http", "https":
		return v.validateHTTP(ctx, endpoint, result)
	case "udp":
		return v.validateUDP(ctx, endpoint, result)
	default:
		// Default to TCP validation
		return v.validateTCP(ctx, endpoint, result)
	}
}

// validateTCP performs TCP connection validation
func (v *HealthCheckValidator) validateTCP(ctx context.Context, endpoint Endpoint, result *ValidationResult) error {
	v.stats.TCPChecks++

	address := net.JoinHostPort(endpoint.Address, strconv.Itoa(endpoint.Port))

	conn, err := net.DialTimeout("tcp", address, v.config.TCPTimeout)
	if err != nil {
		return fmt.Errorf("TCP connection failed: %w", err)
	}
	defer conn.Close()

	result.Details[fmt.Sprintf("tcp_%s_%d", endpoint.Address, endpoint.Port)] = "connected"

	return nil
}

// validateHTTP performs HTTP validation
func (v *HealthCheckValidator) validateHTTP(ctx context.Context, endpoint Endpoint, result *ValidationResult) error {
	v.stats.HTTPChecks++

	scheme := endpoint.Protocol
	if scheme == "" {
		if endpoint.Secure || endpoint.Port == 443 {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	url := fmt.Sprintf("%s://%s:%d%s", scheme, endpoint.Address, endpoint.Port, endpoint.Path)

	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add timeout to request context
	reqCtx, cancel := context.WithTimeout(ctx, v.config.HTTPTimeout)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Consider 2xx and 3xx status codes as healthy
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Details[fmt.Sprintf("http_%s_%d", endpoint.Address, endpoint.Port)] = map[string]interface{}{
			"status_code": resp.StatusCode,
			"status":      resp.Status,
			"headers":     resp.Header,
		}
		return nil
	}

	return fmt.Errorf("HTTP health check failed: status %d", resp.StatusCode)
}

// validateUDP performs UDP validation (basic connectivity test)
func (v *HealthCheckValidator) validateUDP(ctx context.Context, endpoint Endpoint, result *ValidationResult) error {
	address := net.JoinHostPort(endpoint.Address, strconv.Itoa(endpoint.Port))

	conn, err := net.DialTimeout("udp", address, v.config.TCPTimeout)
	if err != nil {
		return fmt.Errorf("UDP connection failed: %w", err)
	}
	defer conn.Close()

	result.Details[fmt.Sprintf("udp_%s_%d", endpoint.Address, endpoint.Port)] = "connected"

	return nil
}

// Health check implementations

func (v *HealthCheckValidator) performTCPCheck(ctx context.Context, endpoint Endpoint) CheckResult {
	address := net.JoinHostPort(endpoint.Address, strconv.Itoa(endpoint.Port))

	conn, err := net.DialTimeout("tcp", address, v.config.TCPTimeout)
	if err != nil {
		return CheckResult{
			Name:    "TCP Connection",
			Passed:  false,
			Message: fmt.Sprintf("Failed to connect: %v", err),
			Data: map[string]interface{}{
				"address": address,
				"error":   err.Error(),
			},
		}
	}
	defer conn.Close()

	return CheckResult{
		Name:    "TCP Connection",
		Passed:  true,
		Message: "TCP connection successful",
		Data: map[string]interface{}{
			"address":     address,
			"local_addr":  conn.LocalAddr().String(),
			"remote_addr": conn.RemoteAddr().String(),
		},
	}
}

func (v *HealthCheckValidator) performHTTPCheck(ctx context.Context, endpoint Endpoint) CheckResult {
	scheme := endpoint.Protocol
	if scheme == "" {
		if endpoint.Secure || endpoint.Port == 443 {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	url := fmt.Sprintf("%s://%s:%d%s", scheme, endpoint.Address, endpoint.Port, endpoint.Path)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return CheckResult{
			Name:    "HTTP Request",
			Passed:  false,
			Message: fmt.Sprintf("Failed to create request: %v", err),
			Data: map[string]interface{}{
				"url":   url,
				"error": err.Error(),
			},
		}
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return CheckResult{
			Name:    "HTTP Request",
			Passed:  false,
			Message: fmt.Sprintf("HTTP request failed: %v", err),
			Data: map[string]interface{}{
				"url":   url,
				"error": err.Error(),
			},
		}
	}
	defer resp.Body.Close()

	return CheckResult{
		Name:    "HTTP Request",
		Passed:  resp.StatusCode >= 200 && resp.StatusCode < 400,
		Message: fmt.Sprintf("HTTP %d %s", resp.StatusCode, resp.Status),
		Data: map[string]interface{}{
			"url":         url,
			"status_code": resp.StatusCode,
			"status":      resp.Status,
			"headers":     resp.Header,
		},
	}
}

func (v *HealthCheckValidator) performTLSCheck(ctx context.Context, endpoint Endpoint) CheckResult {
	// This would perform TLS certificate validation
	// For now, return a simple check result
	return CheckResult{
		Name:    "TLS Certificate",
		Passed:  true,
		Message: "TLS check not implemented",
		Data: map[string]interface{}{
			"endpoint": fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port),
		},
	}
}

func (v *HealthCheckValidator) performDNSCheck(ctx context.Context, endpoint Endpoint) CheckResult {
	ips, err := net.LookupIP(endpoint.Address)
	if err != nil {
		return CheckResult{
			Name:    "DNS Resolution",
			Passed:  false,
			Message: fmt.Sprintf("DNS lookup failed: %v", err),
			Data: map[string]interface{}{
				"hostname": endpoint.Address,
				"error":    err.Error(),
			},
		}
	}

	var ipStrings []string
	for _, ip := range ips {
		ipStrings = append(ipStrings, ip.String())
	}

	return CheckResult{
		Name:    "DNS Resolution",
		Passed:  len(ips) > 0,
		Message: fmt.Sprintf("Resolved to %d IP(s)", len(ips)),
		Data: map[string]interface{}{
			"hostname": endpoint.Address,
			"ips":      ipStrings,
		},
	}
}

// Helper methods

func (v *HealthCheckValidator) updateStats(result ValidationResult) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.stats.TotalValidations++
	v.stats.LastValidation = time.Now()

	if result.Valid {
		v.stats.SuccessfulValidations++
	} else {
		v.stats.FailedValidations++
	}

	// Update rolling average latency
	if v.stats.AverageLatency == 0 {
		v.stats.AverageLatency = result.ResponseTime
	} else {
		// Simple moving average
		v.stats.AverageLatency = (v.stats.AverageLatency + result.ResponseTime) / 2
	}
}

func (v *HealthCheckValidator) calculateSummary(results []ValidationResult) ValidationSummary {
	summary := ValidationSummary{
		Total: len(results),
	}

	var totalTime time.Duration

	for _, result := range results {
		if result.Valid {
			summary.Valid++
		} else {
			summary.Invalid++
		}

		if result.Error != nil {
			summary.Errors++
		}

		totalTime += result.ResponseTime
	}

	if summary.Total > 0 {
		summary.AvgTime = totalTime / time.Duration(summary.Total)
	}

	return summary
}

func (v *HealthCheckValidator) getEnabledMethods() []string {
	var methods []string

	if v.config.EnableTCPCheck {
		methods = append(methods, "tcp")
	}
	if v.config.EnableHTTPCheck {
		methods = append(methods, "http")
	}
	if v.config.EnableTLSCheck {
		methods = append(methods, "tls")
	}
	if v.config.EnableDNSCheck {
		methods = append(methods, "dns")
	}

	return methods
}

// ValidationError represents a validation error with multiple underlying errors
type ValidationError struct {
	ServiceID string
	Errors    []error
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("validation failed for service %s: %v", e.ServiceID, e.Errors[0])
	}
	return fmt.Sprintf("validation failed for service %s: %d errors", e.ServiceID, len(e.Errors))
}

// NewKubernetesValidator creates a validator for Kubernetes services
func NewKubernetesValidator(client kubernetes.Interface, logger *slog.Logger) Validator {
	config := ValidatorConfig{
		ConnectionTimeout: 5 * time.Second,
		HTTPTimeout:       10 * time.Second,
		MaxRetries:        2,
		MaxConcurrent:     20,
		EnableTCPCheck:    true,
		EnableHTTPCheck:   true,
		EnableDNSCheck:    true,
	}

	return NewHealthCheckValidator(config, logger)
}

// NewLocalValidator creates a validator for local services
func NewLocalValidator(logger *slog.Logger) Validator {
	config := ValidatorConfig{
		ConnectionTimeout: 3 * time.Second,
		HTTPTimeout:       5 * time.Second,
		MaxRetries:        1,
		MaxConcurrent:     50,
		EnableTCPCheck:    true,
		EnableHTTPCheck:   true,
		EnableDNSCheck:    false, // Skip DNS for local services
	}

	return NewHealthCheckValidator(config, logger)
}
