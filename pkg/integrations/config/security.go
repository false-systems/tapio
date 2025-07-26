package config

import "time"

// SecurityConfig provides common security configuration
type SecurityConfig struct {
	BaseConfig `yaml:",inline" json:",inline"`

	// TLS settings
	TLS TLSConfig `yaml:"tls" json:"tls"`

	// Authentication
	Auth AuthConfig `yaml:"auth" json:"auth"`

	// Rate limiting
	RateLimit RateLimitConfig `yaml:"rate_limit" json:"rate_limit"`

	// Network security
	Network NetworkSecurityConfig `yaml:"network" json:"network"`
}

// TLSConfig defines TLS settings
type TLSConfig struct {
	Enabled            bool     `yaml:"enabled" json:"enabled"`
	CertFile           string   `yaml:"cert_file" json:"cert_file"`
	KeyFile            string   `yaml:"key_file" json:"key_file"`
	CAFile             string   `yaml:"ca_file" json:"ca_file"`
	MinVersion         string   `yaml:"min_version" json:"min_version"`
	CipherSuites       []string `yaml:"cipher_suites" json:"cipher_suites"`
	InsecureSkipVerify bool     `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
	ClientAuth         string   `yaml:"client_auth" json:"client_auth"`
}

// AuthConfig defines authentication settings
type AuthConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Method  string `yaml:"method" json:"method"` // jwt, oauth2, api-key, mtls, basic

	// JWT settings
	JWT JWTConfig `yaml:"jwt" json:"jwt"`

	// OAuth2 settings
	OAuth2 OAuth2Config `yaml:"oauth2" json:"oauth2"`

	// API Key settings
	APIKey APIKeyConfig `yaml:"api_key" json:"api_key"`

	// Basic auth settings
	Basic BasicAuthConfig `yaml:"basic" json:"basic"`
}

// JWTConfig defines JWT authentication settings
type JWTConfig struct {
	Secret       string        `yaml:"secret" json:"secret"`
	Issuer       string        `yaml:"issuer" json:"issuer"`
	Audience     []string      `yaml:"audience" json:"audience"`
	Expiration   time.Duration `yaml:"expiration" json:"expiration"`
	ClockSkew    time.Duration `yaml:"clock_skew" json:"clock_skew"`
	PublicKeyURL string        `yaml:"public_key_url" json:"public_key_url"`
}

// OAuth2Config defines OAuth2 settings
type OAuth2Config struct {
	ClientID     string   `yaml:"client_id" json:"client_id"`
	ClientSecret string   `yaml:"client_secret" json:"client_secret"`
	AuthURL      string   `yaml:"auth_url" json:"auth_url"`
	TokenURL     string   `yaml:"token_url" json:"token_url"`
	UserInfoURL  string   `yaml:"userinfo_url" json:"userinfo_url"`
	RedirectURL  string   `yaml:"redirect_url" json:"redirect_url"`
	Scopes       []string `yaml:"scopes" json:"scopes"`
}

// APIKeyConfig defines API key settings
type APIKeyConfig struct {
	Header         string        `yaml:"header" json:"header"`
	QueryParam     string        `yaml:"query_param" json:"query_param"`
	Cookie         string        `yaml:"cookie" json:"cookie"`
	HashedKeys     bool          `yaml:"hashed_keys" json:"hashed_keys"`
	RotationPeriod time.Duration `yaml:"rotation_period" json:"rotation_period"`
}

// BasicAuthConfig defines basic authentication settings
type BasicAuthConfig struct {
	Realm        string `yaml:"realm" json:"realm"`
	HashedPasswd bool   `yaml:"hashed_passwd" json:"hashed_passwd"`
}

// RateLimitConfig defines rate limiting settings
type RateLimitConfig struct {
	Enabled      bool                     `yaml:"enabled" json:"enabled"`
	Global       RateLimitRule            `yaml:"global" json:"global"`
	PerEndpoint  map[string]RateLimitRule `yaml:"per_endpoint" json:"per_endpoint"`
	PerUser      RateLimitRule            `yaml:"per_user" json:"per_user"`
	PerIP        RateLimitRule            `yaml:"per_ip" json:"per_ip"`
	WhitelistIPs []string                 `yaml:"whitelist_ips" json:"whitelist_ips"`
	BlacklistIPs []string                 `yaml:"blacklist_ips" json:"blacklist_ips"`
	Distributed  bool                     `yaml:"distributed" json:"distributed"`
	RedisURL     string                   `yaml:"redis_url" json:"redis_url"`
}

// RateLimitRule defines a rate limit rule
type RateLimitRule struct {
	Rate       int           `yaml:"rate" json:"rate"`
	Burst      int           `yaml:"burst" json:"burst"`
	Period     time.Duration `yaml:"period" json:"period"`
	PenaltyBox time.Duration `yaml:"penalty_box" json:"penalty_box"`
}

// NetworkSecurityConfig defines network security settings
type NetworkSecurityConfig struct {
	AllowedCIDRs   []string      `yaml:"allowed_cidrs" json:"allowed_cidrs"`
	DeniedCIDRs    []string      `yaml:"denied_cidrs" json:"denied_cidrs"`
	TrustedProxies []string      `yaml:"trusted_proxies" json:"trusted_proxies"`
	RealIPHeader   string        `yaml:"real_ip_header" json:"real_ip_header"`
	MaxConnections int           `yaml:"max_connections" json:"max_connections"`
	ReadTimeout    time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout" json:"write_timeout"`
}

// DefaultSecurityConfig returns default security configuration
func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		BaseConfig: DefaultBaseConfig(),
		TLS: TLSConfig{
			Enabled:    true,
			MinVersion: "TLS1.2",
		},
		RateLimit: RateLimitConfig{
			Enabled: true,
			Global: RateLimitRule{
				Rate:   100,
				Burst:  200,
				Period: time.Second,
			},
		},
		Network: NetworkSecurityConfig{
			MaxConnections: 1000,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
		},
	}
}
