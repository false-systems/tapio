package security

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/logging"
	"golang.org/x/time/rate"
)

// RateLimiter provides comprehensive rate limiting functionality
type RateLimiter struct {
	config      RateLimitConfig
	logger      *logging.Logger
	globalLimit *rate.Limiter
	ipLimiters  map[string]*rate.Limiter
	userLimiters map[string]*rate.Limiter
	endpointLimiters map[string]*rate.Limiter
	mutex       sync.RWMutex
	whitelist   map[string]bool
	blacklist   map[string]bool
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimitConfig, logger *logging.Logger) *RateLimiter {
	rl := &RateLimiter{
		config:           config,
		logger:           logger.WithComponent("rate-limiter"),
		ipLimiters:       make(map[string]*rate.Limiter),
		userLimiters:     make(map[string]*rate.Limiter),
		endpointLimiters: make(map[string]*rate.Limiter),
		whitelist:        make(map[string]bool),
		blacklist:        make(map[string]bool),
	}

	// Initialize global rate limiter
	if config.Global.RequestsPerSecond > 0 {
		rl.globalLimit = rate.NewLimiter(
			rate.Limit(config.Global.RequestsPerSecond),
			config.Global.Burst,
		)
	}

	// Initialize whitelist
	for _, ip := range config.WhitelistIPs {
		rl.whitelist[ip] = true
	}

	// Initialize blacklist
	for _, ip := range config.BlacklistIPs {
		rl.blacklist[ip] = true
	}

	return rl
}

// Initialize sets up the rate limiter
func (rl *RateLimiter) Initialize() error {
	rl.logger.Info("Initializing rate limiter",
		"global_rps", rl.config.Global.RequestsPerSecond,
		"per_ip_rps", rl.config.PerIP.RequestsPerSecond,
	)

	// Start cleanup goroutine for unused limiters
	go rl.cleanupLimiters()

	return nil
}

// Allow checks if a request should be allowed based on rate limiting rules
func (rl *RateLimiter) Allow(r *http.Request) bool {
	if !rl.config.Enabled {
		return true
	}

	clientIP := rl.getClientIP(r)
	
	// Check blacklist first
	if rl.blacklist[clientIP] {
		rl.logger.Security("request_blocked", "high",
			"client_ip", clientIP,
			"reason", "blacklisted",
		)
		return false
	}

	// Check whitelist - bypass rate limiting for whitelisted IPs
	if rl.whitelist[clientIP] {
		return true
	}

	// Check global rate limit
	if rl.globalLimit != nil && !rl.globalLimit.Allow() {
		rl.logger.Warn("Global rate limit exceeded",
			"client_ip", clientIP,
			"path", r.URL.Path,
		)
		return false
	}

	// Check per-IP rate limit
	if !rl.checkIPRateLimit(clientIP) {
		rl.logger.Warn("IP rate limit exceeded",
			"client_ip", clientIP,
			"path", r.URL.Path,
		)
		return false
	}

	// Check per-user rate limit (if authenticated)
	if userID := rl.getUserID(r); userID != "" {
		if !rl.checkUserRateLimit(userID) {
			rl.logger.Warn("User rate limit exceeded",
				"user_id", userID,
				"client_ip", clientIP,
				"path", r.URL.Path,
			)
			return false
		}
	}

	// Check per-endpoint rate limit
	if !rl.checkEndpointRateLimit(r.URL.Path) {
		rl.logger.Warn("Endpoint rate limit exceeded",
			"endpoint", r.URL.Path,
			"client_ip", clientIP,
		)
		return false
	}

	return true
}

// checkIPRateLimit checks the rate limit for a specific IP address
func (rl *RateLimiter) checkIPRateLimit(ip string) bool {
	if rl.config.PerIP.RequestsPerSecond <= 0 {
		return true
	}

	rl.mutex.Lock()
	limiter, exists := rl.ipLimiters[ip]
	if !exists {
		limiter = rate.NewLimiter(
			rate.Limit(rl.config.PerIP.RequestsPerSecond),
			rl.config.PerIP.Burst,
		)
		rl.ipLimiters[ip] = limiter
	}
	rl.mutex.Unlock()

	return limiter.Allow()
}

// checkUserRateLimit checks the rate limit for a specific user
func (rl *RateLimiter) checkUserRateLimit(userID string) bool {
	if rl.config.PerUser.RequestsPerSecond <= 0 {
		return true
	}

	rl.mutex.Lock()
	limiter, exists := rl.userLimiters[userID]
	if !exists {
		limiter = rate.NewLimiter(
			rate.Limit(rl.config.PerUser.RequestsPerSecond),
			rl.config.PerUser.Burst,
		)
		rl.userLimiters[userID] = limiter
	}
	rl.mutex.Unlock()

	return limiter.Allow()
}

// checkEndpointRateLimit checks the rate limit for a specific endpoint
func (rl *RateLimiter) checkEndpointRateLimit(endpoint string) bool {
	// Find matching endpoint configuration
	var endpointConfig *RateLimitRule
	for pattern, config := range rl.config.PerEndpoint {
		if rl.matchEndpoint(endpoint, pattern) {
			endpointConfig = &config
			break
		}
	}

	if endpointConfig == nil || endpointConfig.RequestsPerSecond <= 0 {
		return true
	}

	rl.mutex.Lock()
	limiter, exists := rl.endpointLimiters[endpoint]
	if !exists {
		limiter = rate.NewLimiter(
			rate.Limit(endpointConfig.RequestsPerSecond),
			endpointConfig.Burst,
		)
		rl.endpointLimiters[endpoint] = limiter
	}
	rl.mutex.Unlock()

	return limiter.Allow()
}

// matchEndpoint checks if an endpoint matches a pattern
func (rl *RateLimiter) matchEndpoint(endpoint, pattern string) bool {
	// Simple pattern matching - in production you might want more sophisticated matching
	if pattern == "*" {
		return true
	}
	
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(endpoint, prefix)
	}
	
	return endpoint == pattern
}

// getClientIP extracts the client IP address from the request
func (rl *RateLimiter) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (proxy/load balancer)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Get the first IP (original client)
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header (Nginx)
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// getUserID extracts the user ID from the request context
func (rl *RateLimiter) getUserID(r *http.Request) string {
	if userID := r.Context().Value("user_id"); userID != nil {
		if uid, ok := userID.(string); ok {
			return uid
		}
	}
	return ""
}

// cleanupLimiters periodically removes unused rate limiters to prevent memory leaks
func (rl *RateLimiter) cleanupLimiters() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		rl.mutex.Lock()

		// Clean up IP limiters
		for ip, limiter := range rl.ipLimiters {
			// Remove limiters that haven't been used recently
			if limiter.Tokens() == float64(rl.config.PerIP.Burst) {
				delete(rl.ipLimiters, ip)
			}
		}

		// Clean up user limiters
		for userID, limiter := range rl.userLimiters {
			if limiter.Tokens() == float64(rl.config.PerUser.Burst) {
				delete(rl.userLimiters, userID)
			}
		}

		// Clean up endpoint limiters
		for endpoint, limiter := range rl.endpointLimiters {
			// Get the configured burst for this endpoint
			var burstSize int
			for pattern, config := range rl.config.PerEndpoint {
				if rl.matchEndpoint(endpoint, pattern) {
					burstSize = config.Burst
					break
				}
			}
			
			if limiter.Tokens() == float64(burstSize) {
				delete(rl.endpointLimiters, endpoint)
			}
		}

		rl.mutex.Unlock()
	}
}

// AddToWhitelist adds an IP address to the whitelist
func (rl *RateLimiter) AddToWhitelist(ip string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	rl.whitelist[ip] = true
	rl.logger.Info("IP added to whitelist", "ip", ip)
}

// RemoveFromWhitelist removes an IP address from the whitelist
func (rl *RateLimiter) RemoveFromWhitelist(ip string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	delete(rl.whitelist, ip)
	rl.logger.Info("IP removed from whitelist", "ip", ip)
}

// AddToBlacklist adds an IP address to the blacklist
func (rl *RateLimiter) AddToBlacklist(ip string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	rl.blacklist[ip] = true
	rl.logger.Security("ip_blacklisted", "high", "ip", ip)
}

// RemoveFromBlacklist removes an IP address from the blacklist
func (rl *RateLimiter) RemoveFromBlacklist(ip string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	delete(rl.blacklist, ip)
	rl.logger.Info("IP removed from blacklist", "ip", ip)
}

// GetStats returns rate limiter statistics
func (rl *RateLimiter) GetStats() *RateLimiterStats {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()

	return &RateLimiterStats{
		ActiveIPLimiters:       len(rl.ipLimiters),
		ActiveUserLimiters:     len(rl.userLimiters),
		ActiveEndpointLimiters: len(rl.endpointLimiters),
		WhitelistedIPs:         len(rl.whitelist),
		BlacklistedIPs:         len(rl.blacklist),
		GlobalLimitEnabled:     rl.globalLimit != nil,
	}
}

// RateLimiterStats contains statistics about the rate limiter
type RateLimiterStats struct {
	ActiveIPLimiters       int  `json:"active_ip_limiters"`
	ActiveUserLimiters     int  `json:"active_user_limiters"`
	ActiveEndpointLimiters int  `json:"active_endpoint_limiters"`
	WhitelistedIPs         int  `json:"whitelisted_ips"`
	BlacklistedIPs         int  `json:"blacklisted_ips"`
	GlobalLimitEnabled     bool `json:"global_limit_enabled"`
}

// ResetIPLimiter resets the rate limiter for a specific IP
func (rl *RateLimiter) ResetIPLimiter(ip string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	delete(rl.ipLimiters, ip)
	rl.logger.Info("IP rate limiter reset", "ip", ip)
}

// ResetUserLimiter resets the rate limiter for a specific user
func (rl *RateLimiter) ResetUserLimiter(userID string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	delete(rl.userLimiters, userID)
	rl.logger.Info("User rate limiter reset", "user_id", userID)
}

// IsIPWhitelisted checks if an IP is whitelisted
func (rl *RateLimiter) IsIPWhitelisted(ip string) bool {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()
	return rl.whitelist[ip]
}

// IsIPBlacklisted checks if an IP is blacklisted
func (rl *RateLimiter) IsIPBlacklisted(ip string) bool {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()
	return rl.blacklist[ip]
}

// UpdateConfig updates the rate limiter configuration
func (rl *RateLimiter) UpdateConfig(config RateLimitConfig) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	rl.config = config

	// Update global limiter
	if config.Global.RequestsPerSecond > 0 {
		rl.globalLimit = rate.NewLimiter(
			rate.Limit(config.Global.RequestsPerSecond),
			config.Global.Burst,
		)
	} else {
		rl.globalLimit = nil
	}

	// Clear existing limiters to apply new configuration
	rl.ipLimiters = make(map[string]*rate.Limiter)
	rl.userLimiters = make(map[string]*rate.Limiter)
	rl.endpointLimiters = make(map[string]*rate.Limiter)

	// Update whitelist
	rl.whitelist = make(map[string]bool)
	for _, ip := range config.WhitelistIPs {
		rl.whitelist[ip] = true
	}

	// Update blacklist
	rl.blacklist = make(map[string]bool)
	for _, ip := range config.BlacklistIPs {
		rl.blacklist[ip] = true
	}

	rl.logger.Info("Rate limiter configuration updated")
}

// GetIPLimiterInfo returns information about an IP's rate limiter
func (rl *RateLimiter) GetIPLimiterInfo(ip string) *LimiterInfo {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()

	limiter, exists := rl.ipLimiters[ip]
	if !exists {
		return &LimiterInfo{
			Exists:         false,
			Limit:          float64(rl.config.PerIP.RequestsPerSecond),
			Burst:          rl.config.PerIP.Burst,
			Tokens:         float64(rl.config.PerIP.Burst),
			IsWhitelisted:  rl.whitelist[ip],
			IsBlacklisted:  rl.blacklist[ip],
		}
	}

	return &LimiterInfo{
		Exists:         true,
		Limit:          float64(limiter.Limit()),
		Burst:          limiter.Burst(),
		Tokens:         limiter.Tokens(),
		IsWhitelisted:  rl.whitelist[ip],
		IsBlacklisted:  rl.blacklist[ip],
	}
}

// LimiterInfo contains information about a specific rate limiter
type LimiterInfo struct {
	Exists         bool    `json:"exists"`
	Limit          float64 `json:"limit"`
	Burst          int     `json:"burst"`
	Tokens         float64 `json:"tokens"`
	IsWhitelisted  bool    `json:"is_whitelisted"`
	IsBlacklisted  bool    `json:"is_blacklisted"`
}

// TLSManager provides TLS configuration management
type TLSManager struct {
	config TLSConfig
	logger *logging.Logger
}

// NewTLSManager creates a new TLS manager
func NewTLSManager(config TLSConfig, logger *logging.Logger) *TLSManager {
	return &TLSManager{
		config: config,
		logger: logger.WithComponent("tls-manager"),
	}
}

// Initialize sets up the TLS manager
func (tm *TLSManager) Initialize() error {
	if !tm.config.Enabled {
		tm.logger.Info("TLS is disabled")
		return nil
	}

	tm.logger.Info("Initializing TLS manager",
		"min_version", tm.config.MinVersion,
		"client_auth", tm.config.ClientAuth,
	)

	// Validate certificate files exist
	if tm.config.CertFile == "" || tm.config.KeyFile == "" {
		return fmt.Errorf("TLS certificate and key files must be specified")
	}

	// Additional TLS validation could be added here

	return nil
}

// GetTLSConfig returns a configured TLS config
func (tm *TLSManager) GetTLSConfig() (*tls.Config, error) {
	config := &tls.Config{
		MinVersion:               tm.getTLSVersion(tm.config.MinVersion),
		MaxVersion:               tm.getTLSVersion(tm.config.MaxVersion),
		PreferServerCipherSuites: tm.config.PreferServerCipher,
		InsecureSkipVerify:       tm.config.InsecureSkipVerify,
		ClientAuth:               tm.getClientAuthType(tm.config.ClientAuth),
	}

	// Set cipher suites if specified
	if len(tm.config.CipherSuites) > 0 {
		config.CipherSuites = tm.getCipherSuites(tm.config.CipherSuites)
	}

	return config, nil
}

// getTLSVersion converts string version to tls constant
func (tm *TLSManager) getTLSVersion(version string) uint16 {
	switch version {
	case "1.0":
		return tls.VersionTLS10
	case "1.1":
		return tls.VersionTLS11
	case "1.2":
		return tls.VersionTLS12
	case "1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12 // Default to TLS 1.2
	}
}

// getClientAuthType converts string to ClientAuthType
func (tm *TLSManager) getClientAuthType(authType string) tls.ClientAuthType {
	switch authType {
	case "none":
		return tls.NoClientCert
	case "request":
		return tls.RequestClientCert
	case "require":
		return tls.RequireAnyClientCert
	case "verify":
		return tls.VerifyClientCertIfGiven
	case "require-verify":
		return tls.RequireAndVerifyClientCert
	default:
		return tls.NoClientCert
	}
}

// getCipherSuites converts string names to cipher suite constants
func (tm *TLSManager) getCipherSuites(suites []string) []uint16 {
	var cipherSuites []uint16
	
	cipherMap := map[string]uint16{
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":   tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384": tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305":  tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	}
	
	for _, suite := range suites {
		if cipherSuite, exists := cipherMap[suite]; exists {
			cipherSuites = append(cipherSuites, cipherSuite)
		}
	}
	
	return cipherSuites
}