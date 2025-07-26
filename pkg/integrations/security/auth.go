package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// OAuth2UserInfo represents OAuth2 user information
type OAuth2UserInfo struct {
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
	Provider string   `json:"provider"`
}

// UserAccount represents a user account in the simulated store
type UserAccount struct {
	UserID       string     `json:"user_id"`
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"password_hash"`
	Roles        []string   `json:"roles"`
	Permissions  []string   `json:"permissions"`
	Active       bool       `json:"active"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLogin    *time.Time `json:"last_login"`
	LockedUntil  *time.Time `json:"locked_until"`
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
	// Extract token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	if len(authHeader) < 7 || authHeader[:7] != "Bearer " {
		return false
	}

	tokenString := authHeader[7:]
	if tokenString == "" {
		return false
	}

	// Validate token with OAuth2 provider
	userInfo, err := am.validateOAuth2Token(tokenString)
	if err != nil {
		am.logger.Debug("OAuth2 token validation failed", "error", err)
		return false
	}

	// Add OAuth2 user context to request
	ctx := context.WithValue(r.Context(), "user_id", userInfo.UserID)
	ctx = context.WithValue(ctx, "username", userInfo.Username)
	ctx = context.WithValue(ctx, "email", userInfo.Email)
	ctx = context.WithValue(ctx, "roles", userInfo.Roles)
	ctx = context.WithValue(ctx, "oauth2_provider", userInfo.Provider)
	*r = *r.WithContext(ctx)

	return true
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
	// Basic input validation
	if username == "" || password == "" {
		return false, "", nil, nil, fmt.Errorf("username and password are required")
	}

	// Length validation
	if len(username) < 3 || len(username) > 50 {
		return false, "", nil, nil, fmt.Errorf("username must be between 3 and 50 characters")
	}
	if len(password) < 8 || len(password) > 128 {
		return false, "", nil, nil, fmt.Errorf("password must be between 8 and 128 characters")
	}

	// Simulate user store lookup
	// In production, this would query your user database, LDAP, etc.
	userStore := am.getSimulatedUserStore()
	
	user, exists := userStore[username]
	if !exists {
		return false, "", nil, nil, nil // User not found
	}

	// Validate password (in production, use proper password hashing)
	if !am.validatePassword(password, user.PasswordHash) {
		return false, "", nil, nil, nil // Invalid password
	}

	// Check if user account is active
	if !user.Active {
		return false, "", nil, nil, fmt.Errorf("user account is disabled")
	}

	// Check if account is locked
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		return false, "", nil, nil, fmt.Errorf("user account is temporarily locked")
	}

	// Return user information
	return true, user.UserID, user.Roles, user.Permissions, nil
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

// validateOAuth2Token validates an OAuth2 token with the provider
func (am *AuthManager) validateOAuth2Token(tokenString string) (*OAuth2UserInfo, error) {
	// Create request to OAuth2 provider's userinfo endpoint
	req, err := http.NewRequest("GET", am.config.OAuth2Config.UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo request: %w", err)
	}

	// Add bearer token
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req.Header.Set("Accept", "application/json")

	// Make request to OAuth2 provider
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to validate token with OAuth2 provider: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OAuth2 provider returned status %d", resp.StatusCode)
	}

	// Parse response
	var userInfo struct {
		Sub      string   `json:"sub"`
		Username string   `json:"preferred_username"`
		Email    string   `json:"email"`
		Name     string   `json:"name"`
		Groups   []string `json:"groups"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to decode OAuth2 userinfo response: %w", err)
	}

	// Map to internal user info structure
	roles := am.mapOAuth2GroupsToRoles(userInfo.Groups)
	username := userInfo.Username
	if username == "" {
		username = userInfo.Name
	}
	if username == "" {
		username = strings.Split(userInfo.Email, "@")[0]
	}

	return &OAuth2UserInfo{
		UserID:   userInfo.Sub,
		Username: username,
		Email:    userInfo.Email,
		Roles:    roles,
		Provider: "oauth2",
	}, nil
}

// mapOAuth2GroupsToRoles maps OAuth2 groups to internal roles
func (am *AuthManager) mapOAuth2GroupsToRoles(groups []string) []string {
	roleMapping := map[string]string{
		"admin":     "admin",
		"user":      "user",
		"readonly":  "readonly",
		"developer": "developer",
		"operator":  "operator",
	}

	var roles []string
	roleSet := make(map[string]bool)

	for _, group := range groups {
		if role, exists := roleMapping[strings.ToLower(group)]; exists {
			if !roleSet[role] {
				roles = append(roles, role)
				roleSet[role] = true
			}
		}
	}

	// Default to user role if no roles mapped
	if len(roles) == 0 {
		roles = []string{"user"}
	}

	return roles
}

// getSimulatedUserStore returns a simulated user store for demonstration
func (am *AuthManager) getSimulatedUserStore() map[string]*UserAccount {
	// In production, this would be replaced with actual database/LDAP queries
	return map[string]*UserAccount{
		"admin": {
			UserID:       "admin-001",
			Username:     "admin",
			Email:        "admin@example.com",
			PasswordHash: am.hashPassword("admin123"),
			Roles:        []string{"admin"},
			Permissions:  []string{"read", "write", "delete", "admin"},
			Active:       true,
			CreatedAt:    time.Now().Add(-30 * 24 * time.Hour),
		},
		"user": {
			UserID:       "user-001",
			Username:     "user",
			Email:        "user@example.com",
			PasswordHash: am.hashPassword("user123"),
			Roles:        []string{"user"},
			Permissions:  []string{"read"},
			Active:       true,
			CreatedAt:    time.Now().Add(-15 * 24 * time.Hour),
		},
		"operator": {
			UserID:       "operator-001",
			Username:     "operator",
			Email:        "operator@example.com",
			PasswordHash: am.hashPassword("operator123"),
			Roles:        []string{"operator"},
			Permissions:  []string{"read", "write"},
			Active:       true,
			CreatedAt:    time.Now().Add(-7 * 24 * time.Hour),
		},
		"readonly": {
			UserID:       "readonly-001",
			Username:     "readonly",
			Email:        "readonly@example.com",
			PasswordHash: am.hashPassword("readonly123"),
			Roles:        []string{"readonly"},
			Permissions:  []string{"read"},
			Active:       true,
			CreatedAt:    time.Now().Add(-3 * 24 * time.Hour),
		},
		"disabled": {
			UserID:       "disabled-001",
			Username:     "disabled",
			Email:        "disabled@example.com",
			PasswordHash: am.hashPassword("disabled123"),
			Roles:        []string{"user"},
			Permissions:  []string{"read"},
			Active:       false, // Disabled account
			CreatedAt:    time.Now().Add(-60 * 24 * time.Hour),
		},
	}
}

// hashPassword creates a simple hash of the password (use bcrypt in production)
func (am *AuthManager) hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password + "tapio-salt"))
	return hex.EncodeToString(hash[:])
}

// validatePassword validates a password against its hash
func (am *AuthManager) validatePassword(password, passwordHash string) bool {
	expectedHash := am.hashPassword(password)
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(passwordHash)) == 1
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
