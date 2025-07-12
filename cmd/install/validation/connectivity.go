package validation

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ConnectivityChecker checks network connectivity
type ConnectivityChecker struct {
	client    *http.Client
	resolver  *net.Resolver
	timeout   time.Duration
	userAgent string
}

// NewConnectivityChecker creates a new connectivity checker
func NewConnectivityChecker() *ConnectivityChecker {
	return &ConnectivityChecker{
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: 3 * time.Second,
				}
				return d.DialContext(ctx, network, address)
			},
		},
		timeout:   30 * time.Second,
		userAgent: "Tapio-Installer/1.0",
	}
}

// CheckEndpoints checks connectivity to multiple endpoints
func (c *ConnectivityChecker) CheckEndpoints(ctx context.Context, endpoints []string) error {
	if len(endpoints) == 0 {
		return nil
	}
	
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	
	// Check endpoints concurrently
	results := make(chan error, len(endpoints))
	var wg sync.WaitGroup
	
	for _, endpoint := range endpoints {
		wg.Add(1)
		go func(ep string) {
			defer wg.Done()
			if err := c.checkEndpoint(ctx, ep); err != nil {
				results <- fmt.Errorf("endpoint %s: %w", ep, err)
			} else {
				results <- nil
			}
		}(endpoint)
	}
	
	// Wait for all checks to complete
	go func() {
		wg.Wait()
		close(results)
	}()
	
	// Collect results
	var errors []string
	successCount := 0
	
	for err := range results {
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			successCount++
		}
	}
	
	// Return error if any endpoint failed
	if len(errors) > 0 {
		return fmt.Errorf("connectivity check failed (%d/%d succeeded): %s",
			successCount, len(endpoints), strings.Join(errors, "; "))
	}
	
	return nil
}

// checkEndpoint checks connectivity to a single endpoint
func (c *ConnectivityChecker) checkEndpoint(ctx context.Context, endpoint string) error {
	// Parse URL
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	
	// Check DNS resolution
	if err := c.checkDNS(ctx, u.Hostname()); err != nil {
		return fmt.Errorf("DNS resolution failed: %w", err)
	}
	
	// Check TCP connectivity
	if err := c.checkTCP(ctx, u.Host); err != nil {
		return fmt.Errorf("TCP connection failed: %w", err)
	}
	
	// Check HTTP/HTTPS connectivity
	if u.Scheme == "http" || u.Scheme == "https" {
		if err := c.checkHTTP(ctx, endpoint); err != nil {
			return fmt.Errorf("HTTP check failed: %w", err)
		}
	}
	
	return nil
}

// checkDNS verifies DNS resolution
func (c *ConnectivityChecker) checkDNS(ctx context.Context, hostname string) error {
	// Skip DNS check for IP addresses
	if net.ParseIP(hostname) != nil {
		return nil
	}
	
	// Skip DNS check for localhost
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return nil
	}
	
	// Resolve hostname
	addrs, err := c.resolver.LookupHost(ctx, hostname)
	if err != nil {
		return err
	}
	
	if len(addrs) == 0 {
		return fmt.Errorf("no addresses found for %s", hostname)
	}
	
	return nil
}

// checkTCP verifies TCP connectivity
func (c *ConnectivityChecker) checkTCP(ctx context.Context, address string) error {
	// Add default port if missing
	if !strings.Contains(address, ":") {
		address += ":80"
	}
	
	// Try to establish TCP connection
	d := net.Dialer{
		Timeout: 5 * time.Second,
	}
	
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	conn.Close()
	
	return nil
}

// checkHTTP verifies HTTP/HTTPS connectivity
func (c *ConnectivityChecker) checkHTTP(ctx context.Context, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	
	req.Header.Set("User-Agent", c.userAgent)
	
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	// Accept any 2xx or 3xx status code
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	
	return nil
}

// CheckInternetConnectivity checks general internet connectivity
func (c *ConnectivityChecker) CheckInternetConnectivity(ctx context.Context) error {
	// Check well-known endpoints
	endpoints := []string{
		"https://www.google.com",
		"https://www.cloudflare.com",
		"https://www.github.com",
	}
	
	// Try each endpoint
	for _, endpoint := range endpoints {
		if err := c.checkEndpoint(ctx, endpoint); err == nil {
			return nil // At least one succeeded
		}
	}
	
	return fmt.Errorf("no internet connectivity detected")
}

// CheckProxy checks if a proxy is configured and working
func (c *ConnectivityChecker) CheckProxy(ctx context.Context, proxyURL string) error {
	// Parse proxy URL
	u, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}
	
	// Create client with proxy
	proxyClient := &http.Client{
		Timeout: c.timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(u),
		},
	}
	
	// Test proxy connectivity
	testURL := "http://www.google.com"
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return err
	}
	
	resp, err := proxyClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxy test failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		return fmt.Errorf("proxy returned error: %s", resp.Status)
	}
	
	return nil
}

// MeasureLatency measures latency to an endpoint
func (c *ConnectivityChecker) MeasureLatency(ctx context.Context, endpoint string) (time.Duration, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return 0, fmt.Errorf("invalid URL: %w", err)
	}
	
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	
	start := time.Now()
	
	d := net.Dialer{
		Timeout: 5 * time.Second,
	}
	
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return 0, err
	}
	conn.Close()
	
	return time.Since(start), nil
}

// ConnectivityReport represents a connectivity test report
type ConnectivityReport struct {
	Timestamp    time.Time
	Duration     time.Duration
	Results      []EndpointResult
	DNSServers   []string
	ProxyEnabled bool
	ProxyURL     string
}

// EndpointResult represents the result of checking an endpoint
type EndpointResult struct {
	Endpoint     string
	Reachable    bool
	Latency      time.Duration
	Error        error
	DNSResolved  bool
	TCPConnected bool
	HTTPStatus   int
}

// RunConnectivityDiagnostics performs comprehensive connectivity diagnostics
func (c *ConnectivityChecker) RunConnectivityDiagnostics(ctx context.Context, endpoints []string) (*ConnectivityReport, error) {
	startTime := time.Now()
	report := &ConnectivityReport{
		Timestamp: startTime,
		Results:   make([]EndpointResult, 0, len(endpoints)),
	}
	
	// Check for proxy configuration
	if proxyURL := getProxyURL(); proxyURL != "" {
		report.ProxyEnabled = true
		report.ProxyURL = proxyURL
	}
	
	// Get DNS servers
	report.DNSServers = c.getDNSServers()
	
	// Test each endpoint
	for _, endpoint := range endpoints {
		result := EndpointResult{
			Endpoint: endpoint,
		}
		
		// Parse URL
		u, err := url.Parse(endpoint)
		if err != nil {
			result.Error = err
			report.Results = append(report.Results, result)
			continue
		}
		
		// DNS check
		if err := c.checkDNS(ctx, u.Hostname()); err == nil {
			result.DNSResolved = true
		}
		
		// TCP check
		if err := c.checkTCP(ctx, u.Host); err == nil {
			result.TCPConnected = true
		}
		
		// Measure latency
		if latency, err := c.MeasureLatency(ctx, endpoint); err == nil {
			result.Latency = latency
		}
		
		// Full connectivity check
		if err := c.checkEndpoint(ctx, endpoint); err != nil {
			result.Error = err
			result.Reachable = false
		} else {
			result.Reachable = true
		}
		
		report.Results = append(report.Results, result)
	}
	
	report.Duration = time.Since(startTime)
	return report, nil
}

// getProxyURL gets the system proxy configuration
func getProxyURL() string {
	// Check environment variables
	for _, env := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"} {
		if proxy := os.Getenv(env); proxy != "" {
			return proxy
		}
	}
	return ""
}

// getDNSServers gets the system DNS servers
func (c *ConnectivityChecker) getDNSServers() []string {
	// This is a simplified implementation
	// Real implementation would parse /etc/resolv.conf on Unix
	// or use Windows API on Windows
	return []string{"8.8.8.8", "8.8.4.4"}
}