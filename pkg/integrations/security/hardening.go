package security

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/logging"
	"github.com/yairfalse/tapio/pkg/monitoring"
)

// SecurityHardening provides comprehensive security hardening for production deployments
type SecurityHardening struct {
	config      *SecurityConfig
	logger      *logging.Logger
	metrics     *monitoring.MetricsCollector
	rateLimiter *RateLimiter
	authManager *AuthManager
	tlsManager  *TLSManager
	auditor     *SecurityAuditor
}

// SecurityConfig defines security configuration
type SecurityConfig struct {
	// TLS Configuration
	TLS TLSConfig `yaml:"tls"`

	// Authentication and Authorization
	Auth AuthConfig `yaml:"auth"`

	// Rate Limiting
	RateLimit RateLimitConfig `yaml:"rate_limit"`

	// Security Headers
	Headers SecurityHeaders `yaml:"headers"`

	// Input Validation
	Validation InputValidation `yaml:"validation"`

	// Audit Logging
	Audit AuditConfig `yaml:"audit"`

	// Network Security
	Network NetworkSecurity `yaml:"network"`

	// Compliance
	Compliance ComplianceConfig `yaml:"compliance"`
}

// TLSConfig defines TLS security configuration
type TLSConfig struct {
	Enabled            bool       `yaml:"enabled"`
	CertFile           string     `yaml:"cert_file"`
	KeyFile            string     `yaml:"key_file"`
	CAFile             string     `yaml:"ca_file"`
	MinVersion         string     `yaml:"min_version"`
	MaxVersion         string     `yaml:"max_version"`
	CipherSuites       []string   `yaml:"cipher_suites"`
	PreferServerCipher bool       `yaml:"prefer_server_cipher"`
	InsecureSkipVerify bool       `yaml:"insecure_skip_verify"`
	ClientAuth         string     `yaml:"client_auth"` // none, request, require, verify, require-verify
	HSTS               HSTSConfig `yaml:"hsts"`
}

// HSTSConfig defines HTTP Strict Transport Security
type HSTSConfig struct {
	Enabled           bool `yaml:"enabled"`
	MaxAge            int  `yaml:"max_age"`
	IncludeSubDomains bool `yaml:"include_subdomains"`
	Preload           bool `yaml:"preload"`
}

// AuthConfig defines authentication configuration
type AuthConfig struct {
	Enabled          bool              `yaml:"enabled"`
	Method           string            `yaml:"method"` // jwt, oauth2, api-key, mtls
	JWTSecret        string            `yaml:"jwt_secret"`
	JWTExpiration    time.Duration     `yaml:"jwt_expiration"`
	JWTIssuer        string            `yaml:"jwt_issuer"`
	OAuth2Config     OAuth2Config      `yaml:"oauth2"`
	APIKeyHeader     string            `yaml:"api_key_header"`
	APIKeys          map[string]string `yaml:"api_keys"`
	SessionTimeout   time.Duration     `yaml:"session_timeout"`
	MaxSessions      int               `yaml:"max_sessions"`
	BruteForceConfig BruteForceConfig  `yaml:"brute_force"`
}

// OAuth2Config defines OAuth2 configuration
type OAuth2Config struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	RedirectURL  string   `yaml:"redirect_url"`
	Scopes       []string `yaml:"scopes"`
	AuthURL      string   `yaml:"auth_url"`
	TokenURL     string   `yaml:"token_url"`
}

// BruteForceConfig defines brute force protection
type BruteForceConfig struct {
	Enabled      bool          `yaml:"enabled"`
	MaxAttempts  int           `yaml:"max_attempts"`
	LockoutTime  time.Duration `yaml:"lockout_time"`
	WindowSize   time.Duration `yaml:"window_size"`
	WhitelistIPs []string      `yaml:"whitelist_ips"`
}

// RateLimitConfig defines rate limiting configuration
type RateLimitConfig struct {
	Enabled      bool                     `yaml:"enabled"`
	Global       RateLimitRule            `yaml:"global"`
	PerEndpoint  map[string]RateLimitRule `yaml:"per_endpoint"`
	PerUser      RateLimitRule            `yaml:"per_user"`
	PerIP        RateLimitRule            `yaml:"per_ip"`
	BurstAllowed bool                     `yaml:"burst_allowed"`
	WhitelistIPs []string                 `yaml:"whitelist_ips"`
	BlacklistIPs []string                 `yaml:"blacklist_ips"`
}

// RateLimitRule defines a single rate limit rule
type RateLimitRule struct {
	RequestsPerSecond int           `yaml:"requests_per_second"`
	Burst             int           `yaml:"burst"`
	WindowSize        time.Duration `yaml:"window_size"`
}

// SecurityHeaders defines security headers configuration
type SecurityHeaders struct {
	Enabled                 bool              `yaml:"enabled"`
	ContentSecurityPolicy   string            `yaml:"content_security_policy"`
	XFrameOptions           string            `yaml:"x_frame_options"`
	XContentTypeOptions     string            `yaml:"x_content_type_options"`
	XSSProtection           string            `yaml:"xss_protection"`
	ReferrerPolicy          string            `yaml:"referrer_policy"`
	PermissionsPolicy       string            `yaml:"permissions_policy"`
	StrictTransportSecurity string            `yaml:"strict_transport_security"`
	CustomHeaders           map[string]string `yaml:"custom_headers"`
}

// InputValidation defines input validation configuration
type InputValidation struct {
	Enabled          bool              `yaml:"enabled"`
	MaxRequestSize   int64             `yaml:"max_request_size"`
	MaxHeaderSize    int64             `yaml:"max_header_size"`
	AllowedMimeTypes []string          `yaml:"allowed_mime_types"`
	DenyPatterns     []string          `yaml:"deny_patterns"`
	SanitizeInput    bool              `yaml:"sanitize_input"`
	ValidateJSON     bool              `yaml:"validate_json"`
	ValidateXML      bool              `yaml:"validate_xml"`
	CustomValidators map[string]string `yaml:"custom_validators"`
}

// AuditConfig defines audit logging configuration
type AuditConfig struct {
	Enabled           bool       `yaml:"enabled"`
	LogAuthentication bool       `yaml:"log_authentication"`
	LogAuthorization  bool       `yaml:"log_authorization"`
	LogDataAccess     bool       `yaml:"log_data_access"`
	LogConfigChanges  bool       `yaml:"log_config_changes"`
	LogErrors         bool       `yaml:"log_errors"`
	RetentionDays     int        `yaml:"retention_days"`
	EncryptLogs       bool       `yaml:"encrypt_logs"`
	SignLogs          bool       `yaml:"sign_logs"`
	ExternalSIEM      SIEMConfig `yaml:"external_siem"`
}

// SIEMConfig defines SIEM integration
type SIEMConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Endpoint  string `yaml:"endpoint"`
	Format    string `yaml:"format"` // cef, leef, json
	BatchSize int    `yaml:"batch_size"`
}

// NetworkSecurity defines network security configuration
type NetworkSecurity struct {
	AllowedCIDRs      []string       `yaml:"allowed_cidrs"`
	DeniedCIDRs       []string       `yaml:"denied_cidrs"`
	TrustedProxies    []string       `yaml:"trusted_proxies"`
	MaxConnections    int            `yaml:"max_connections"`
	ConnectionTimeout time.Duration  `yaml:"connection_timeout"`
	IdleTimeout       time.Duration  `yaml:"idle_timeout"`
	DDoSProtection    DDoSProtection `yaml:"ddos_protection"`
}

// DDoSProtection defines DDoS protection configuration
type DDoSProtection struct {
	Enabled           bool          `yaml:"enabled"`
	RequestsPerSecond int           `yaml:"requests_per_second"`
	ConcurrentLimit   int           `yaml:"concurrent_limit"`
	SlidingWindow     time.Duration `yaml:"sliding_window"`
	BanDuration       time.Duration `yaml:"ban_duration"`
	GeoBlocking       []string      `yaml:"geo_blocking"`
}

// ComplianceConfig defines compliance requirements
type ComplianceConfig struct {
	Standards          []string            `yaml:"standards"` // SOC2, ISO27001, PCI-DSS, GDPR
	DataClassification DataClassification  `yaml:"data_classification"`
	Encryption         EncryptionConfig    `yaml:"encryption"`
	AccessControl      AccessControlConfig `yaml:"access_control"`
	DataRetention      DataRetentionConfig `yaml:"data_retention"`
	PrivacyControls    PrivacyControls     `yaml:"privacy_controls"`
}

// DataClassification defines data classification
type DataClassification struct {
	Enabled      bool                  `yaml:"enabled"`
	DefaultLevel string                `yaml:"default_level"`
	Levels       map[string]string     `yaml:"levels"`
	Policies     map[string]DataPolicy `yaml:"policies"`
}

// DataPolicy defines data handling policy
type DataPolicy struct {
	Encryption bool   `yaml:"encryption"`
	Masking    bool   `yaml:"masking"`
	Retention  string `yaml:"retention"`
	Access     string `yaml:"access"`
}

// EncryptionConfig defines encryption requirements
type EncryptionConfig struct {
	AtRest    EncryptionPolicy `yaml:"at_rest"`
	InTransit EncryptionPolicy `yaml:"in_transit"`
	InMemory  EncryptionPolicy `yaml:"in_memory"`
}

// EncryptionPolicy defines encryption policy
type EncryptionPolicy struct {
	Required    bool          `yaml:"required"`
	Algorithm   string        `yaml:"algorithm"`
	KeySize     int           `yaml:"key_size"`
	KeyRotation time.Duration `yaml:"key_rotation"`
}

// AccessControlConfig defines access control
type AccessControlConfig struct {
	RBAC        bool                `yaml:"rbac"`
	ABAC        bool                `yaml:"abac"`
	DefaultDeny bool                `yaml:"default_deny"`
	Permissions map[string]string   `yaml:"permissions"`
	Roles       map[string][]string `yaml:"roles"`
}

// DataRetentionConfig defines data retention policies
type DataRetentionConfig struct {
	Enabled       bool                     `yaml:"enabled"`
	DefaultPeriod time.Duration            `yaml:"default_period"`
	Policies      map[string]time.Duration `yaml:"policies"`
	AutoPurge     bool                     `yaml:"auto_purge"`
}

// PrivacyControls defines privacy controls
type PrivacyControls struct {
	Anonymization     bool `yaml:"anonymization"`
	Pseudonymization  bool `yaml:"pseudonymization"`
	RightToErasure    bool `yaml:"right_to_erasure"`
	DataPortability   bool `yaml:"data_portability"`
	ConsentManagement bool `yaml:"consent_management"`
}

// NewSecurityHardening creates a new security hardening instance
func NewSecurityHardening(config *SecurityConfig) (*SecurityHardening, error) {
	if config == nil {
		config = DefaultSecurityConfig()
	}

	logger := logging.Production.WithComponent("security-hardening")
	metrics := monitoring.NewMetricsCollector(monitoring.DefaultMetricsConfig())

	sh := &SecurityHardening{
		config:      config,
		logger:      logger,
		metrics:     metrics,
		rateLimiter: NewRateLimiter(config.RateLimit, logger),
		authManager: NewAuthManager(config.Auth, logger),
		tlsManager:  NewTLSManager(config.TLS, logger),
		auditor:     NewSecurityAuditor(config.Audit, logger),
	}

	return sh, nil
}

// DefaultSecurityConfig returns production-ready security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		TLS: TLSConfig{
			Enabled:    true,
			MinVersion: "1.2",
			CipherSuites: []string{
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305",
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			PreferServerCipher: true,
			ClientAuth:         "require-verify",
			HSTS: HSTSConfig{
				Enabled:           true,
				MaxAge:            31536000, // 1 year
				IncludeSubDomains: true,
				Preload:           true,
			},
		},
		Auth: AuthConfig{
			Enabled:        true,
			Method:         "jwt",
			JWTExpiration:  24 * time.Hour,
			SessionTimeout: 8 * time.Hour,
			MaxSessions:    5,
			BruteForceConfig: BruteForceConfig{
				Enabled:     true,
				MaxAttempts: 5,
				LockoutTime: 30 * time.Minute,
				WindowSize:  15 * time.Minute,
			},
		},
		RateLimit: RateLimitConfig{
			Enabled: true,
			Global: RateLimitRule{
				RequestsPerSecond: 1000,
				Burst:             2000,
				WindowSize:        time.Minute,
			},
			PerIP: RateLimitRule{
				RequestsPerSecond: 100,
				Burst:             200,
				WindowSize:        time.Minute,
			},
		},
		Headers: SecurityHeaders{
			Enabled:                 true,
			ContentSecurityPolicy:   "default-src 'self'",
			XFrameOptions:           "DENY",
			XContentTypeOptions:     "nosniff",
			XSSProtection:           "1; mode=block",
			ReferrerPolicy:          "strict-origin-when-cross-origin",
			StrictTransportSecurity: "max-age=31536000; includeSubDomains; preload",
		},
		Validation: InputValidation{
			Enabled:        true,
			MaxRequestSize: 10 * 1024 * 1024, // 10MB
			MaxHeaderSize:  1024 * 1024,      // 1MB
			SanitizeInput:  true,
			ValidateJSON:   true,
		},
		Audit: AuditConfig{
			Enabled:           true,
			LogAuthentication: true,
			LogAuthorization:  true,
			LogDataAccess:     true,
			LogConfigChanges:  true,
			LogErrors:         true,
			RetentionDays:     90,
			EncryptLogs:       true,
		},
		Network: NetworkSecurity{
			MaxConnections:    10000,
			ConnectionTimeout: 30 * time.Second,
			IdleTimeout:       300 * time.Second,
			DDoSProtection: DDoSProtection{
				Enabled:           true,
				RequestsPerSecond: 10000,
				ConcurrentLimit:   1000,
				SlidingWindow:     time.Minute,
				BanDuration:       time.Hour,
			},
		},
		Compliance: ComplianceConfig{
			Standards: []string{"SOC2", "ISO27001"},
			Encryption: EncryptionConfig{
				AtRest: EncryptionPolicy{
					Required:    true,
					Algorithm:   "AES-256",
					KeySize:     256,
					KeyRotation: 90 * 24 * time.Hour,
				},
				InTransit: EncryptionPolicy{
					Required:  true,
					Algorithm: "TLS",
					KeySize:   256,
				},
			},
		},
	}
}

// Initialize sets up security hardening
func (sh *SecurityHardening) Initialize(ctx context.Context) error {
	sh.logger.Info("Initializing security hardening")

	// Initialize TLS manager
	if err := sh.tlsManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize TLS manager: %w", err)
	}

	// Initialize auth manager
	if err := sh.authManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize auth manager: %w", err)
	}

	// Initialize rate limiter
	if err := sh.rateLimiter.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize rate limiter: %w", err)
	}

	// Initialize auditor
	if err := sh.auditor.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize auditor: %w", err)
	}

	sh.logger.Info("Security hardening initialized successfully")
	return nil
}

// SecureHTTPServer applies security hardening to HTTP server
func (sh *SecurityHardening) SecureHTTPServer(server *http.Server) error {
	// Apply TLS configuration
	if sh.config.TLS.Enabled {
		tlsConfig, err := sh.tlsManager.GetTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to get TLS config: %w", err)
		}
		server.TLSConfig = tlsConfig
	}

	// Set security timeouts
	server.ReadTimeout = 15 * time.Second
	server.WriteTimeout = 15 * time.Second
	server.IdleTimeout = 60 * time.Second
	server.ReadHeaderTimeout = 5 * time.Second

	// Set maximum header bytes
	server.MaxHeaderBytes = int(sh.config.Validation.MaxHeaderSize)

	// Add security middleware
	originalHandler := server.Handler
	server.Handler = sh.securityMiddleware(originalHandler)

	sh.logger.Info("HTTP server security hardening applied")
	return nil
}

// securityMiddleware applies security middleware stack
func (sh *SecurityHardening) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security audit logging
		sh.auditor.LogRequest(r)

		// Rate limiting
		if !sh.rateLimiter.Allow(r) {
			sh.auditor.LogSecurityEvent("rate_limit_exceeded", r.RemoteAddr)
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Authentication
		if sh.config.Auth.Enabled {
			if !sh.authManager.Authenticate(r) {
				sh.auditor.LogSecurityEvent("authentication_failed", r.RemoteAddr)
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}
		}

		// Input validation
		if err := sh.validateRequest(r); err != nil {
			sh.auditor.LogSecurityEvent("input_validation_failed", r.RemoteAddr)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Apply security headers
		sh.applySecurityHeaders(w)

		// Continue to next handler
		next.ServeHTTP(w, r)
	})
}

// validateRequest validates incoming requests
func (sh *SecurityHardening) validateRequest(r *http.Request) error {
	if !sh.config.Validation.Enabled {
		return nil
	}

	// Check request size
	if r.ContentLength > sh.config.Validation.MaxRequestSize {
		return fmt.Errorf("request size exceeds limit")
	}

	// Check Content-Type if specified
	if len(sh.config.Validation.AllowedMimeTypes) > 0 {
		contentType := r.Header.Get("Content-Type")
		allowed := false
		for _, mimeType := range sh.config.Validation.AllowedMimeTypes {
			if strings.Contains(contentType, mimeType) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("content type not allowed")
		}
	}

	// Check for malicious patterns
	for _, pattern := range sh.config.Validation.DenyPatterns {
		if strings.Contains(r.URL.String(), pattern) {
			return fmt.Errorf("request contains denied pattern")
		}
	}

	return nil
}

// applySecurityHeaders applies security headers to response
func (sh *SecurityHardening) applySecurityHeaders(w http.ResponseWriter) {
	if !sh.config.Headers.Enabled {
		return
	}

	headers := sh.config.Headers

	if headers.ContentSecurityPolicy != "" {
		w.Header().Set("Content-Security-Policy", headers.ContentSecurityPolicy)
	}
	if headers.XFrameOptions != "" {
		w.Header().Set("X-Frame-Options", headers.XFrameOptions)
	}
	if headers.XContentTypeOptions != "" {
		w.Header().Set("X-Content-Type-Options", headers.XContentTypeOptions)
	}
	if headers.XSSProtection != "" {
		w.Header().Set("X-XSS-Protection", headers.XSSProtection)
	}
	if headers.ReferrerPolicy != "" {
		w.Header().Set("Referrer-Policy", headers.ReferrerPolicy)
	}
	if headers.StrictTransportSecurity != "" {
		w.Header().Set("Strict-Transport-Security", headers.StrictTransportSecurity)
	}

	// Apply custom headers
	for key, value := range headers.CustomHeaders {
		w.Header().Set(key, value)
	}
}

// ValidateCompliance validates compliance requirements
func (sh *SecurityHardening) ValidateCompliance() (*ComplianceReport, error) {
	report := &ComplianceReport{
		Timestamp: time.Now(),
		Standards: sh.config.Compliance.Standards,
		Checks:    make(map[string]ComplianceCheck),
	}

	// SOC2 compliance checks
	if contains(sh.config.Compliance.Standards, "SOC2") {
		report.Checks["SOC2"] = sh.validateSOC2Compliance()
	}

	// ISO27001 compliance checks
	if contains(sh.config.Compliance.Standards, "ISO27001") {
		report.Checks["ISO27001"] = sh.validateISO27001Compliance()
	}

	// Calculate overall compliance score
	totalChecks := 0
	passedChecks := 0
	for _, check := range report.Checks {
		for _, result := range check.Results {
			totalChecks++
			if result.Passed {
				passedChecks++
			}
		}
	}

	if totalChecks > 0 {
		report.OverallScore = float64(passedChecks) / float64(totalChecks)
	}

	return report, nil
}

// ComplianceReport represents compliance validation results
type ComplianceReport struct {
	Timestamp    time.Time                  `json:"timestamp"`
	Standards    []string                   `json:"standards"`
	OverallScore float64                    `json:"overall_score"`
	Checks       map[string]ComplianceCheck `json:"checks"`
}

// ComplianceCheck represents compliance check results
type ComplianceCheck struct {
	Standard string             `json:"standard"`
	Results  []ComplianceResult `json:"results"`
}

// ComplianceResult represents a single compliance check result
type ComplianceResult struct {
	Control     string `json:"control"`
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	Message     string `json:"message"`
}

// validateSOC2Compliance validates SOC2 compliance
func (sh *SecurityHardening) validateSOC2Compliance() ComplianceCheck {
	check := ComplianceCheck{
		Standard: "SOC2",
		Results:  []ComplianceResult{},
	}

	// CC6.1 - Encryption of data in transit
	check.Results = append(check.Results, ComplianceResult{
		Control:     "CC6.1",
		Description: "Data in transit is encrypted",
		Passed:      sh.config.TLS.Enabled,
		Message:     "TLS encryption enabled for data in transit",
	})

	// CC6.2 - Authentication controls
	check.Results = append(check.Results, ComplianceResult{
		Control:     "CC6.2",
		Description: "Authentication controls are implemented",
		Passed:      sh.config.Auth.Enabled,
		Message:     "Authentication controls enabled",
	})

	// CC7.1 - Logging and monitoring
	check.Results = append(check.Results, ComplianceResult{
		Control:     "CC7.1",
		Description: "Security events are logged and monitored",
		Passed:      sh.config.Audit.Enabled,
		Message:     "Security audit logging enabled",
	})

	return check
}

// validateISO27001Compliance validates ISO27001 compliance
func (sh *SecurityHardening) validateISO27001Compliance() ComplianceCheck {
	check := ComplianceCheck{
		Standard: "ISO27001",
		Results:  []ComplianceResult{},
	}

	// A.12.6.1 - Management of technical vulnerabilities
	check.Results = append(check.Results, ComplianceResult{
		Control:     "A.12.6.1",
		Description: "Technical vulnerabilities are managed",
		Passed:      sh.config.Validation.Enabled,
		Message:     "Input validation and security controls enabled",
	})

	// A.13.1.1 - Network controls
	check.Results = append(check.Results, ComplianceResult{
		Control:     "A.13.1.1",
		Description: "Network security controls are implemented",
		Passed:      len(sh.config.Network.AllowedCIDRs) > 0 || sh.config.Network.DDoSProtection.Enabled,
		Message:     "Network security controls configured",
	})

	return check
}

// GetSecurityMetrics returns security-related metrics
func (sh *SecurityHardening) GetSecurityMetrics() map[string]interface{} {
	return map[string]interface{}{
		"auth_enabled":         sh.config.Auth.Enabled,
		"tls_enabled":          sh.config.TLS.Enabled,
		"rate_limit_enabled":   sh.config.RateLimit.Enabled,
		"audit_enabled":        sh.config.Audit.Enabled,
		"ddos_protection":      sh.config.Network.DDoSProtection.Enabled,
		"compliance_standards": len(sh.config.Compliance.Standards),
	}
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
