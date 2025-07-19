package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/yairfalse/tapio/pkg/interfaces/logging"
)

// AuthManager provides comprehensive authentication and authorization
type AuthManager struct {
	config       AuthConfig
	logger       *logging.Logger
	jwtSecret    []byte
	sessions     map[string]*Session
	failedLogins map[string]*LoginAttempts
	mutex        sync.RWMutex
}

// Session represents an authenticated session
type Session struct {
	ID          string            `json:"id"`
	UserID      string            `json:"user_id"`
	Username    string            `json:"username"`
	Roles       []string          `json:"roles"`
	Permissions []string          `json:"permissions"`
	CreatedAt   time.Time         `json:"created_at"`
	LastAccess  time.Time         `json:"last_access"`
	ExpiresAt   time.Time         `json:"expires_at"`
	IPAddress   string            `json:"ip_address"`
	UserAgent   string            `json:"user_agent"`
	Metadata    map[string]string `json:"metadata"`
}

// LoginAttempts tracks failed login attempts for brute force protection
type LoginAttempts struct {
	Count        int       `json:"count"`
	FirstAttempt time.Time `json:"first_attempt"`
	LastAttempt  time.Time `json:"last_attempt"`
	LockedUntil  time.Time `json:"locked_until"`
}

// JWTClaims represents JWT token claims
type JWTClaims struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	SessionID   string   `json:"session_id"`
	jwt.RegisteredClaims
}

// NewAuthManager creates a new authentication manager
func NewAuthManager(config AuthConfig, logger *logging.Logger) *AuthManager {
	jwtSecret := []byte(config.JWTSecret)
	if len(jwtSecret) == 0 {
		// Generate a random secret if none provided (not recommended for production)
		jwtSecret = make([]byte, 32)
		rand.Read(jwtSecret)
		logger.Warn("JWT secret not provided, generated random secret (not suitable for production)")
	}

	return &AuthManager{
		config:       config,
		logger:       logger.WithComponent("auth-manager"),
		jwtSecret:    jwtSecret,
		sessions:     make(map[string]*Session),
		failedLogins: make(map[string]*LoginAttempts),
	}
}

// Initialize sets up the authentication manager
func (am *AuthManager) Initialize() error {
	am.logger.Info("Initializing authentication manager",
		"method", am.config.Method,
		"session_timeout", am.config.SessionTimeout,
	)

	// Start session cleanup goroutine
	go am.sessionCleanup()

	// Start failed login cleanup goroutine
	if am.config.BruteForceConfig.Enabled {
		go am.failedLoginCleanup()
	}

	return nil
}

// Authenticate validates an incoming request
func (am *AuthManager) Authenticate(r *http.Request) bool {
	if !am.config.Enabled {
		return true
	}

	switch am.config.Method {
	case "jwt":
		return am.authenticateJWT(r)
	case "api-key":
		return am.authenticateAPIKey(r)
	case "oauth2":
		return am.authenticateOAuth2(r)
	case "mtls":
		return am.authenticateMTLS(r)
	default:
		am.logger.Error("Unknown authentication method", "method", am.config.Method)
		return false
	}
}

// authenticateJWT validates JWT tokens
func (am *AuthManager) authenticateJWT(r *http.Request) bool {
	// Extract token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	if len(authHeader) < 7 || authHeader[:7] != "Bearer " {
		return false
	}

	tokenString := authHeader[7:]

	// Parse and validate token
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return am.jwtSecret, nil
	})

	if err != nil {
		am.logger.Debug("JWT validation failed", "error", err)
		return false
	}

	if !token.Valid {
		return false
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return false
	}

	// Check if session exists and is valid
	am.mutex.RLock()
	session, exists := am.sessions[claims.SessionID]
	am.mutex.RUnlock()

	if !exists {
		return false
	}

	// Check session expiration
	if time.Now().After(session.ExpiresAt) {
		am.removeSession(claims.SessionID)
		return false
	}

	// Update last access time
	am.mutex.Lock()
	session.LastAccess = time.Now()
	am.mutex.Unlock()

	// Add user context to request
	ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
	ctx = context.WithValue(ctx, "username", claims.Username)
	ctx = context.WithValue(ctx, "roles", claims.Roles)
	ctx = context.WithValue(ctx, "session_id", claims.SessionID)
	*r = *r.WithContext(ctx)

	return true
}

// authenticateAPIKey validates API keys
func (am *AuthManager) authenticateAPIKey(r *http.Request) bool {
	headerName := am.config.APIKeyHeader
	if headerName == "" {
		headerName = "X-API-Key"
	}

	providedKey := r.Header.Get(headerName)
	if providedKey == "" {
		return false
	}

	// Check against configured API keys
	for keyName, expectedKey := range am.config.APIKeys {
		if subtle.ConstantTimeCompare([]byte(providedKey), []byte(expectedKey)) == 1 {
			// Add API key context to request
			ctx := context.WithValue(r.Context(), "api_key_name", keyName)
			*r = *r.WithContext(ctx)
			return true
		}
	}

	return false
}

// authenticateOAuth2 validates OAuth2 tokens
func (am *AuthManager) authenticateOAuth2(r *http.Request) bool {
	// Implementation would validate OAuth2 access tokens
	// This is a placeholder for OAuth2 integration
	return false
}

// authenticateMTLS validates mutual TLS certificates
func (am *AuthManager) authenticateMTLS(r *http.Request) bool {
	// Check if client certificate is present
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return false
	}

	cert := r.TLS.PeerCertificates[0]

	// Add certificate context to request
	ctx := context.WithValue(r.Context(), "client_cert_subject", cert.Subject.String())
	ctx = context.WithValue(ctx, "client_cert_serial", cert.SerialNumber.String())
	*r = *r.WithContext(ctx)

	return true
}

// Login creates a new authenticated session
func (am *AuthManager) Login(username, password, ipAddress, userAgent string) (*Session, error) {
	// Check brute force protection
	if am.config.BruteForceConfig.Enabled {
		if am.isIPLocked(ipAddress) {
			am.logger.Security("login_blocked_ip", "high",
				"username", username,
				"ip_address", ipAddress,
				"reason", "brute_force_protection",
			)
			return nil, fmt.Errorf("IP address is temporarily locked due to failed login attempts")
		}
	}

	// Validate credentials (this would integrate with actual user store)
	valid, userID, roles, permissions, err := am.validateCredentials(username, password)
	if err != nil {
		return nil, fmt.Errorf("credential validation failed: %w", err)
	}

	if !valid {
		// Record failed login attempt
		if am.config.BruteForceConfig.Enabled {
			am.recordFailedLogin(ipAddress)
		}

		am.logger.Security("login_failed", "medium",
			"username", username,
			"ip_address", ipAddress,
		)
		return nil, fmt.Errorf("invalid credentials")
	}

	// Clear failed login attempts on successful login
	if am.config.BruteForceConfig.Enabled {
		am.clearFailedLogins(ipAddress)
	}

	// Check maximum sessions per user
	if am.config.MaxSessions > 0 {
		am.enforceMaxSessions(userID)
	}

	// Create new session
	session := &Session{
		ID:          am.generateSessionID(),
		UserID:      userID,
		Username:    username,
		Roles:       roles,
		Permissions: permissions,
		CreatedAt:   time.Now(),
		LastAccess:  time.Now(),
		ExpiresAt:   time.Now().Add(am.config.SessionTimeout),
		IPAddress:   ipAddress,
		UserAgent:   userAgent,
		Metadata:    make(map[string]string),
	}

	// Store session
	am.mutex.Lock()
	am.sessions[session.ID] = session
	am.mutex.Unlock()

	am.logger.Audit("login_successful", username,
		"user_id", userID,
		"ip_address", ipAddress,
		"session_id", session.ID,
	)

	return session, nil
}

// GenerateJWT creates a JWT token for a session
func (am *AuthManager) GenerateJWT(session *Session) (string, error) {
	claims := &JWTClaims{
		UserID:      session.UserID,
		Username:    session.Username,
		Roles:       session.Roles,
		Permissions: session.Permissions,
		SessionID:   session.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    am.config.JWTIssuer,
			Subject:   session.UserID,
			ExpiresAt: jwt.NewNumericDate(session.ExpiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(am.jwtSecret)
}

// Logout invalidates a session
func (am *AuthManager) Logout(sessionID string) error {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	session, exists := am.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found")
	}

	delete(am.sessions, sessionID)

	am.logger.Audit("logout", session.Username,
		"user_id", session.UserID,
		"session_id", sessionID,
	)

	return nil
}

// validateCredentials validates user credentials
func (am *AuthManager) validateCredentials(username, password string) (bool, string, []string, []string, error) {
	// This is a placeholder implementation
	// In production, this would integrate with your user store (database, LDAP, etc.)

	// For demo purposes, accept any non-empty credentials
	if username != "" && password != "" {
		return true, "user-" + username, []string{"user"}, []string{"read"}, nil
	}

	return false, "", nil, nil, nil
}

// isIPLocked checks if an IP address is locked due to failed login attempts
func (am *AuthManager) isIPLocked(ipAddress string) bool {
	// Check if IP is whitelisted
	for _, whitelistIP := range am.config.BruteForceConfig.WhitelistIPs {
		if ipAddress == whitelistIP {
			return false
		}
	}

	am.mutex.RLock()
	attempts, exists := am.failedLogins[ipAddress]
	am.mutex.RUnlock()

	if !exists {
		return false
	}

	return time.Now().Before(attempts.LockedUntil)
}

// recordFailedLogin records a failed login attempt
func (am *AuthManager) recordFailedLogin(ipAddress string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	now := time.Now()
	attempts, exists := am.failedLogins[ipAddress]

	if !exists {
		attempts = &LoginAttempts{
			FirstAttempt: now,
		}
		am.failedLogins[ipAddress] = attempts
	}

	// Reset counter if window has expired
	if now.Sub(attempts.FirstAttempt) > am.config.BruteForceConfig.WindowSize {
		attempts.Count = 0
		attempts.FirstAttempt = now
	}

	attempts.Count++
	attempts.LastAttempt = now

	// Lock IP if threshold exceeded
	if attempts.Count >= am.config.BruteForceConfig.MaxAttempts {
		attempts.LockedUntil = now.Add(am.config.BruteForceConfig.LockoutTime)
		am.logger.Security("ip_locked", "high",
			"ip_address", ipAddress,
			"failed_attempts", attempts.Count,
			"locked_until", attempts.LockedUntil,
		)
	}
}

// clearFailedLogins clears failed login attempts for an IP
func (am *AuthManager) clearFailedLogins(ipAddress string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	delete(am.failedLogins, ipAddress)
}

// enforceMaxSessions enforces maximum sessions per user
func (am *AuthManager) enforceMaxSessions(userID string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	var userSessions []*Session
	for _, session := range am.sessions {
		if session.UserID == userID {
			userSessions = append(userSessions, session)
		}
	}

	// Remove oldest sessions if over limit
	if len(userSessions) >= am.config.MaxSessions {
		// Sort by creation time (oldest first)
		for i := 0; i < len(userSessions)-1; i++ {
			for j := i + 1; j < len(userSessions); j++ {
				if userSessions[i].CreatedAt.After(userSessions[j].CreatedAt) {
					userSessions[i], userSessions[j] = userSessions[j], userSessions[i]
				}
			}
		}

		// Remove oldest sessions
		toRemove := len(userSessions) - am.config.MaxSessions + 1
		for i := 0; i < toRemove; i++ {
			delete(am.sessions, userSessions[i].ID)
			am.logger.Info("Session removed due to max sessions limit",
				"user_id", userID,
				"session_id", userSessions[i].ID,
			)
		}
	}
}

// removeSession removes a session
func (am *AuthManager) removeSession(sessionID string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	delete(am.sessions, sessionID)
}

// generateSessionID generates a unique session ID
func (am *AuthManager) generateSessionID() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)
}

// sessionCleanup periodically removes expired sessions
func (am *AuthManager) sessionCleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		am.mutex.Lock()
		now := time.Now()
		for sessionID, session := range am.sessions {
			if now.After(session.ExpiresAt) {
				delete(am.sessions, sessionID)
			}
		}
		am.mutex.Unlock()
	}
}

// failedLoginCleanup periodically removes old failed login records
func (am *AuthManager) failedLoginCleanup() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		am.mutex.Lock()
		now := time.Now()
		for ip, attempts := range am.failedLogins {
			// Remove records older than lockout time
			if now.Sub(attempts.LastAttempt) > am.config.BruteForceConfig.LockoutTime {
				delete(am.failedLogins, ip)
			}
		}
		am.mutex.Unlock()
	}
}

// GetActiveSessions returns all active sessions
func (am *AuthManager) GetActiveSessions() []*Session {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	sessions := make([]*Session, 0, len(am.sessions))
	for _, session := range am.sessions {
		sessions = append(sessions, session)
	}

	return sessions
}

// GetSessionCount returns the number of active sessions
func (am *AuthManager) GetSessionCount() int {
	am.mutex.RLock()
	defer am.mutex.RUnlock()
	return len(am.sessions)
}

// AuthorizeRequest checks if a request is authorized for a specific operation
func (am *AuthManager) AuthorizeRequest(r *http.Request, requiredPermission string) bool {
	permissions, ok := r.Context().Value("permissions").([]string)
	if !ok {
		return false
	}

	for _, permission := range permissions {
		if permission == requiredPermission || permission == "admin" {
			return true
		}
	}

	return false
}

// HasRole checks if the authenticated user has a specific role
func (am *AuthManager) HasRole(r *http.Request, requiredRole string) bool {
	roles, ok := r.Context().Value("roles").([]string)
	if !ok {
		return false
	}

	for _, role := range roles {
		if role == requiredRole || role == "admin" {
			return true
		}
	}

	return false
}
