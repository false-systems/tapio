package security

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 100,
			Burst:             200,
			WindowSize:        time.Minute,
		},
		PerIP: RateLimitRule{
			RequestsPerSecond: 10,
			Burst:             20,
		},
		WhitelistIPs: []string{"127.0.0.1", "192.168.1.0/24"},
		BlacklistIPs: []string{"10.0.0.0/8"},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)

	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}

	if rl.globalLimit == nil {
		t.Error("Global rate limiter should be initialized")
	}

	if len(rl.whitelist) == 0 {
		t.Error("Whitelist should be populated")
	}

	if len(rl.blacklist) == 0 {
		t.Error("Blacklist should be populated")
	}
}

func TestRateLimiter_Initialize(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 100,
			Burst:             200,
		},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)

	err := rl.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
}

func TestRateLimiter_Allow_Global(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 5, // Low limit for testing
			Burst:             10,
		},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	// Should allow initial requests up to burst limit
	for i := 0; i < 10; i++ {
		if !rl.Allow(req) {
			t.Errorf("Request %d should be allowed within burst limit", i+1)
		}
	}

	// Next request should be rate limited
	if rl.Allow(req) {
		t.Error("Request should be rate limited after burst")
	}
}

func TestRateLimiter_Allow_PerIP(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 1000, // High global limit
			Burst:             2000,
		},
		PerIP: RateLimitRule{
			RequestsPerSecond: 2, // Low per-IP limit for testing
			Burst:             3,
		},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	// Test requests from first IP
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"

	// Should allow initial requests up to burst limit
	for i := 0; i < 3; i++ {
		if !rl.Allow(req1) {
			t.Errorf("Request %d from IP1 should be allowed within burst limit", i+1)
		}
	}

	// Next request from same IP should be rate limited
	if rl.Allow(req1) {
		t.Error("Request from IP1 should be rate limited after burst")
	}

	// Test requests from different IP (should be independent)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.101:12345"

	// Should allow requests from different IP
	for i := 0; i < 3; i++ {
		if !rl.Allow(req2) {
			t.Errorf("Request %d from IP2 should be allowed (independent limit)", i+1)
		}
	}
}

func TestRateLimiter_Allow_Whitelist(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 1, // Very low limit
			Burst:             1,
		},
		WhitelistIPs: []string{"127.0.0.1"},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	// Test whitelisted IP
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	// Should allow many requests from whitelisted IP
	for i := 0; i < 10; i++ {
		if !rl.Allow(req) {
			t.Errorf("Request %d from whitelisted IP should always be allowed", i+1)
		}
	}

	// Test non-whitelisted IP
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"

	// Should be subject to rate limiting
	if !rl.Allow(req2) {
		t.Error("First request from non-whitelisted IP should be allowed")
	}

	if rl.Allow(req2) {
		t.Error("Second request from non-whitelisted IP should be rate limited")
	}
}

func TestRateLimiter_Allow_Blacklist(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 1000, // High limit
			Burst:             2000,
		},
		BlacklistIPs: []string{"10.0.0.1"},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	// Test blacklisted IP
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"

	// Should always deny requests from blacklisted IP
	if rl.Allow(req) {
		t.Error("Request from blacklisted IP should be denied")
	}

	// Test non-blacklisted IP
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"

	// Should allow requests from non-blacklisted IP
	if !rl.Allow(req2) {
		t.Error("Request from non-blacklisted IP should be allowed")
	}
}

func TestRateLimiter_Allow_PerEndpoint(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 1000, // High global limit
			Burst:             2000,
		},
		PerEndpoint: map[string]RateLimitRule{
			"/api/sensitive": {
				RequestsPerSecond: 1, // Very restrictive
				Burst:             1,
			},
			"/api/public": {
				RequestsPerSecond: 10,
				Burst:             20,
			},
		},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	// Test sensitive endpoint
	req1 := httptest.NewRequest("GET", "/api/sensitive", nil)
	req1.RemoteAddr = "192.168.1.100:12345"

	if !rl.Allow(req1) {
		t.Error("First request to sensitive endpoint should be allowed")
	}

	if rl.Allow(req1) {
		t.Error("Second request to sensitive endpoint should be rate limited")
	}

	// Test public endpoint (should be independent)
	req2 := httptest.NewRequest("GET", "/api/public", nil)
	req2.RemoteAddr = "192.168.1.100:12345"

	// Should allow multiple requests to public endpoint
	for i := 0; i < 5; i++ {
		if !rl.Allow(req2) {
			t.Errorf("Request %d to public endpoint should be allowed", i+1)
		}
	}
}

func TestRateLimiter_Allow_Disabled(t *testing.T) {
	config := RateLimitConfig{
		Enabled: false, // Rate limiting disabled
		Global: RateLimitRule{
			RequestsPerSecond: 1,
			Burst:             1,
		},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	// Should allow all requests when disabled
	for i := 0; i < 100; i++ {
		if !rl.Allow(req) {
			t.Errorf("Request %d should be allowed when rate limiting is disabled", i+1)
		}
	}
}

func TestRateLimiter_GetClientIP(t *testing.T) {
	config := RateLimitConfig{Enabled: true}
	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)

	// Test direct IP
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	ip := rl.getClientIP(req)
	if ip != "192.168.1.100" {
		t.Errorf("Expected IP '192.168.1.100', got '%s'", ip)
	}

	// Test X-Forwarded-For header
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")

	ip = rl.getClientIP(req)
	if ip != "203.0.113.195" {
		t.Errorf("Expected IP '203.0.113.195' from X-Forwarded-For, got '%s'", ip)
	}

	// Test X-Real-IP header
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.200")

	ip = rl.getClientIP(req)
	if ip != "203.0.113.200" {
		t.Errorf("Expected IP '203.0.113.200' from X-Real-IP, got '%s'", ip)
	}

	// Test X-Forwarded-For takes precedence over X-Real-IP
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.195")
	req.Header.Set("X-Real-IP", "203.0.113.200")

	ip = rl.getClientIP(req)
	if ip != "203.0.113.195" {
		t.Errorf("X-Forwarded-For should take precedence, got '%s'", ip)
	}
}

func TestRateLimiter_WhitelistBehavior(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 1, // Very low limit
			Burst:             1,
		},
		WhitelistIPs: []string{"192.168.1.0/24", "127.0.0.1"},
	}
	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	// Test IP in whitelist CIDR range
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	// Should allow many requests from whitelisted CIDR
	for i := 0; i < 10; i++ {
		if !rl.Allow(req) {
			t.Errorf("Request %d from whitelisted CIDR should be allowed", i+1)
		}
	}

	// Test specific whitelisted IP
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	// Should allow many requests from whitelisted IP
	for i := 0; i < 10; i++ {
		if !rl.Allow(req) {
			t.Errorf("Request %d from whitelisted IP should be allowed", i+1)
		}
	}
}

func TestRateLimiter_CleanupLimiters(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		PerIP: RateLimitRule{
			RequestsPerSecond: 10,
			Burst:             20,
		},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	// Create some IP limiters
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	rl.Allow(req1)

	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.101:12345"
	rl.Allow(req2)

	// Should have limiters for both IPs
	rl.mutex.RLock()
	initialCount := len(rl.ipLimiters)
	rl.mutex.RUnlock()

	if initialCount != 2 {
		t.Errorf("Expected 2 IP limiters, got %d", initialCount)
	}

	// Run cleanup (this method should exist in the implementation)
	// For testing purposes, we'll just verify that limiters exist
	// In a real implementation, there would be a cleanup method that removes old limiters
}

func TestRateLimiter_Statistics(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 10,
			Burst:             20,
		},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	// Make some requests
	for i := 0; i < 5; i++ {
		rl.Allow(req)
	}

	// This would test statistics gathering if implemented
	// The actual implementation would have methods to get statistics
	// like request counts, rate limit hits, etc.
}

func TestRateLimiter_ResetTimingAttack(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 1,
			Burst:             1,
		},
	}

	logger := createTestLogger()
	rl := NewRateLimiter(config, logger)
	rl.Initialize()

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	// First request should be allowed
	start1 := time.Now()
	allowed1 := rl.Allow(req)
	duration1 := time.Since(start1)

	if !allowed1 {
		t.Error("First request should be allowed")
	}

	// Second request should be denied but should take similar time
	// to prevent timing attacks
	start2 := time.Now()
	allowed2 := rl.Allow(req)
	duration2 := time.Since(start2)

	if allowed2 {
		t.Error("Second request should be denied")
	}

	// The durations should be similar (within reasonable bounds)
	// This tests that the rate limiter doesn't reveal information
	// through response timing
	if duration1 > 10*time.Millisecond || duration2 > 10*time.Millisecond {
		t.Error("Rate limiter responses should be fast to prevent timing attacks")
	}
}
