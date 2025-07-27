package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSecurityHardening(t *testing.T) {
	// Test with nil config (should use default)
	sh, err := NewSecurityHardening(nil)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed with nil config: %v", err)
	}

	if sh == nil {
		t.Fatal("NewSecurityHardening returned nil")
	}

	if sh.config == nil {
		t.Fatal("SecurityHardening config is nil")
	}

	// Test with custom config
	config := &SecurityConfig{
		TLS: TLSConfig{
			Enabled:    true,
			MinVersion: "1.2",
		},
		Auth: AuthConfig{
			Enabled: true,
			Method:  "jwt",
		},
	}

	sh, err = NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed with custom config: %v", err)
	}

	if sh.config.TLS.MinVersion != "1.2" {
		t.Errorf("Expected TLS min version '1.2', got '%s'", sh.config.TLS.MinVersion)
	}
}

func TestDefaultSecurityConfig(t *testing.T) {
	config := DefaultSecurityConfig()

	if config == nil {
		t.Fatal("DefaultSecurityConfig returned nil")
	}

	// Test TLS defaults
	if !config.TLS.Enabled {
		t.Error("TLS should be enabled by default")
	}

	if config.TLS.MinVersion != "1.2" {
		t.Errorf("Expected TLS min version '1.2', got '%s'", config.TLS.MinVersion)
	}

	if !config.TLS.PreferServerCipher {
		t.Error("PreferServerCipher should be enabled by default")
	}

	// Test authentication defaults
	if !config.Auth.Enabled {
		t.Error("Auth should be enabled by default")
	}

	if config.Auth.Method != "jwt" {
		t.Errorf("Expected auth method 'jwt', got '%s'", config.Auth.Method)
	}

	// Test rate limiting defaults
	if !config.RateLimit.Enabled {
		t.Error("Rate limiting should be enabled by default")
	}

	if config.RateLimit.Global.RequestsPerSecond != 1000 {
		t.Errorf("Expected global rate limit 1000 req/s, got %d", config.RateLimit.Global.RequestsPerSecond)
	}

	// Test security headers defaults
	if !config.Headers.Enabled {
		t.Error("Security headers should be enabled by default")
	}

	if config.Headers.XFrameOptions != "DENY" {
		t.Errorf("Expected X-Frame-Options 'DENY', got '%s'", config.Headers.XFrameOptions)
	}

	// Test input validation defaults
	if !config.Validation.Enabled {
		t.Error("Input validation should be enabled by default")
	}

	if config.Validation.MaxRequestSize != 10*1024*1024 {
		t.Errorf("Expected max request size 10MB, got %d", config.Validation.MaxRequestSize)
	}

	// Test audit defaults
	if !config.Audit.Enabled {
		t.Error("Audit should be enabled by default")
	}

	if config.Audit.RetentionDays != 90 {
		t.Errorf("Expected audit retention 90 days, got %d", config.Audit.RetentionDays)
	}

	// Test compliance defaults
	if len(config.Compliance.Standards) != 2 {
		t.Errorf("Expected 2 compliance standards, got %d", len(config.Compliance.Standards))
	}

	expectedStandards := []string{"SOC2", "ISO27001"}
	for i, standard := range expectedStandards {
		if i >= len(config.Compliance.Standards) || config.Compliance.Standards[i] != standard {
			t.Errorf("Expected compliance standard '%s', got '%s'", standard, config.Compliance.Standards[i])
		}
	}
}

func TestSecurityHardening_Initialize(t *testing.T) {
	config := DefaultSecurityConfig()
	sh, err := NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed: %v", err)
	}

	ctx := context.Background()
	err = sh.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
}

func TestSecurityHardening_SecureHTTPServer(t *testing.T) {
	config := DefaultSecurityConfig()
	sh, err := NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed: %v", err)
	}

	ctx := context.Background()
	err = sh.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Create a test HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	server := &http.Server{
		Handler: mux,
	}

	err = sh.SecureHTTPServer(server)
	if err != nil {
		t.Fatalf("SecureHTTPServer failed: %v", err)
	}

	// Verify security settings are applied
	if server.ReadTimeout != 15*time.Second {
		t.Errorf("Expected ReadTimeout 15s, got %v", server.ReadTimeout)
	}

	if server.WriteTimeout != 15*time.Second {
		t.Errorf("Expected WriteTimeout 15s, got %v", server.WriteTimeout)
	}

	if server.IdleTimeout != 60*time.Second {
		t.Errorf("Expected IdleTimeout 60s, got %v", server.IdleTimeout)
	}

	if server.MaxHeaderBytes != int(config.Validation.MaxHeaderSize) {
		t.Errorf("Expected MaxHeaderBytes %d, got %d", config.Validation.MaxHeaderSize, server.MaxHeaderBytes)
	}
}

func TestSecurityHardening_SecurityMiddleware(t *testing.T) {
	config := DefaultSecurityConfig()
	// Disable auth for middleware testing
	config.Auth.Enabled = false
	config.RateLimit.Enabled = false

	sh, err := NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed: %v", err)
	}

	ctx := context.Background()
	err = sh.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Create test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	// Wrap with security middleware
	securedHandler := sh.securityMiddleware(testHandler)

	// Test normal request
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	securedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check security headers are applied
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("Expected X-Frame-Options 'DENY', got '%s'", w.Header().Get("X-Frame-Options"))
	}

	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Expected X-Content-Type-Options 'nosniff', got '%s'", w.Header().Get("X-Content-Type-Options"))
	}
}

func TestSecurityHardening_ValidateRequest(t *testing.T) {
	config := DefaultSecurityConfig()
	config.Validation.MaxRequestSize = 1024 // 1KB for testing
	config.Validation.AllowedMimeTypes = []string{"application/json", "text/plain"}
	config.Validation.DenyPatterns = []string{"/admin", "script"}

	sh, err := NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed: %v", err)
	}

	// Test valid request
	req := httptest.NewRequest("POST", "/api/data", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 2

	err = sh.validateRequest(req)
	if err != nil {
		t.Errorf("Valid request should pass validation: %v", err)
	}

	// Test oversized request
	req = httptest.NewRequest("POST", "/api/data", strings.NewReader(strings.Repeat("x", 2048)))
	req.ContentLength = 2048

	err = sh.validateRequest(req)
	if err == nil {
		t.Error("Oversized request should fail validation")
	}

	// Test invalid content type
	req = httptest.NewRequest("POST", "/api/data", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/xml")
	req.ContentLength = 2

	err = sh.validateRequest(req)
	if err == nil {
		t.Error("Invalid content type should fail validation")
	}

	// Test denied pattern
	req = httptest.NewRequest("GET", "/admin/users", nil)
	req.Header.Set("Content-Type", "application/json")

	err = sh.validateRequest(req)
	if err == nil {
		t.Error("Request with denied pattern should fail validation")
	}

	// Test another denied pattern
	req = httptest.NewRequest("GET", "/api/script", nil)
	req.Header.Set("Content-Type", "application/json")

	err = sh.validateRequest(req)
	if err == nil {
		t.Error("Request with script pattern should fail validation")
	}
}

func TestSecurityHardening_ApplySecurityHeaders(t *testing.T) {
	config := DefaultSecurityConfig()
	config.Headers.CustomHeaders = map[string]string{
		"X-Custom-Header": "custom-value",
	}

	sh, err := NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed: %v", err)
	}

	w := httptest.NewRecorder()
	sh.applySecurityHeaders(w)

	headers := w.Header()

	// Check standard security headers
	if headers.Get("Content-Security-Policy") != "default-src 'self'" {
		t.Errorf("Unexpected CSP header: %s", headers.Get("Content-Security-Policy"))
	}

	if headers.Get("X-Frame-Options") != "DENY" {
		t.Errorf("Unexpected X-Frame-Options: %s", headers.Get("X-Frame-Options"))
	}

	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Unexpected X-Content-Type-Options: %s", headers.Get("X-Content-Type-Options"))
	}

	if headers.Get("X-XSS-Protection") != "1; mode=block" {
		t.Errorf("Unexpected X-XSS-Protection: %s", headers.Get("X-XSS-Protection"))
	}

	if headers.Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Errorf("Unexpected Referrer-Policy: %s", headers.Get("Referrer-Policy"))
	}

	// Check custom header
	if headers.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("Custom header not applied: %s", headers.Get("X-Custom-Header"))
	}

	// Test with headers disabled
	config.Headers.Enabled = false
	sh.config = config

	w = httptest.NewRecorder()
	sh.applySecurityHeaders(w)

	if len(w.Header()) > 0 {
		t.Error("No headers should be applied when disabled")
	}
}

func TestSecurityHardening_ValidateCompliance(t *testing.T) {
	config := DefaultSecurityConfig()
	sh, err := NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed: %v", err)
	}

	ctx := context.Background()
	err = sh.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	report, err := sh.ValidateCompliance()
	if err != nil {
		t.Fatalf("ValidateCompliance failed: %v", err)
	}

	if report == nil {
		t.Fatal("Compliance report is nil")
	}

	if len(report.Standards) != 2 {
		t.Errorf("Expected 2 standards, got %d", len(report.Standards))
	}

	// Check SOC2 compliance
	soc2Check, exists := report.Checks["SOC2"]
	if !exists {
		t.Error("SOC2 compliance check missing")
	} else {
		if soc2Check.Standard != "SOC2" {
			t.Errorf("Expected SOC2 standard, got %s", soc2Check.Standard)
		}

		if len(soc2Check.Results) == 0 {
			t.Error("SOC2 check should have results")
		}

		// Verify specific controls
		controls := make(map[string]bool)
		for _, result := range soc2Check.Results {
			controls[result.Control] = result.Passed
		}

		// CC6.1 should pass (TLS enabled)
		if !controls["CC6.1"] {
			t.Error("CC6.1 control should pass (TLS enabled)")
		}

		// CC6.2 should pass (Auth enabled)
		if !controls["CC6.2"] {
			t.Error("CC6.2 control should pass (Auth enabled)")
		}

		// CC7.1 should pass (Audit enabled)
		if !controls["CC7.1"] {
			t.Error("CC7.1 control should pass (Audit enabled)")
		}
	}

	// Check ISO27001 compliance
	iso27001Check, exists := report.Checks["ISO27001"]
	if !exists {
		t.Error("ISO27001 compliance check missing")
	} else {
		if iso27001Check.Standard != "ISO27001" {
			t.Errorf("Expected ISO27001 standard, got %s", iso27001Check.Standard)
		}

		if len(iso27001Check.Results) == 0 {
			t.Error("ISO27001 check should have results")
		}
	}

	// Check overall score calculation
	if report.OverallScore < 0 || report.OverallScore > 1 {
		t.Errorf("Overall score should be between 0 and 1, got %f", report.OverallScore)
	}

	// With default config, most checks should pass
	if report.OverallScore < 0.8 {
		t.Errorf("Expected high compliance score with default config, got %f", report.OverallScore)
	}
}

func TestSecurityHardening_GetSecurityMetrics(t *testing.T) {
	config := DefaultSecurityConfig()
	sh, err := NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed: %v", err)
	}

	metrics := sh.GetSecurityMetrics()

	if metrics == nil {
		t.Fatal("Security metrics is nil")
	}

	// Check expected metrics
	expectedMetrics := []string{
		"auth_enabled",
		"tls_enabled",
		"rate_limit_enabled",
		"audit_enabled",
		"ddos_protection",
		"compliance_standards",
	}

	for _, metric := range expectedMetrics {
		if _, exists := metrics[metric]; !exists {
			t.Errorf("Expected metric '%s' not found", metric)
		}
	}

	// Verify values
	if metrics["auth_enabled"] != true {
		t.Error("auth_enabled should be true")
	}

	if metrics["tls_enabled"] != true {
		t.Error("tls_enabled should be true")
	}

	if metrics["compliance_standards"] != 2 {
		t.Errorf("Expected 2 compliance standards, got %v", metrics["compliance_standards"])
	}
}

func TestSecurityHardening_DisabledFeatures(t *testing.T) {
	// Test with all security features disabled
	config := &SecurityConfig{
		TLS: TLSConfig{
			Enabled: false,
		},
		Auth: AuthConfig{
			Enabled: false,
		},
		RateLimit: RateLimitConfig{
			Enabled: false,
		},
		Headers: SecurityHeaders{
			Enabled: false,
		},
		Validation: InputValidation{
			Enabled: false,
		},
		Audit: AuditConfig{
			Enabled: false,
		},
		Network: NetworkSecurity{
			DDoSProtection: DDoSProtection{
				Enabled: false,
			},
		},
		Compliance: ComplianceConfig{
			Standards: []string{},
		},
	}

	sh, err := NewSecurityHardening(config)
	if err != nil {
		t.Fatalf("NewSecurityHardening failed: %v", err)
	}

	ctx := context.Background()
	err = sh.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Test compliance with disabled features
	report, err := sh.ValidateCompliance()
	if err != nil {
		t.Fatalf("ValidateCompliance failed: %v", err)
	}

	// Should have low compliance score
	if report.OverallScore > 0.5 {
		t.Errorf("Expected low compliance score with disabled features, got %f", report.OverallScore)
	}

	// Test security metrics
	metrics := sh.GetSecurityMetrics()
	if metrics["auth_enabled"] != false {
		t.Error("auth_enabled should be false")
	}

	if metrics["tls_enabled"] != false {
		t.Error("tls_enabled should be false")
	}

	if metrics["compliance_standards"] != 0 {
		t.Errorf("Expected 0 compliance standards, got %v", metrics["compliance_standards"])
	}
}
