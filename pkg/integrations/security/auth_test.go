package security

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthManager_NewAuthManager(t *testing.T) {
	config := AuthConfig{
		Enabled:        true,
		Method:         "jwt",
		JWTSecret:      "test-secret",
		JWTIssuer:      "tapio-test",
		SessionTimeout: 1 * time.Hour,
		MaxSessions:    5,
		BruteForceConfig: BruteForceConfig{
			Enabled:     true,
			MaxAttempts: 3,
			LockoutTime: 15 * time.Minute,
			WindowSize:  5 * time.Minute,
		},
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)

	if authManager == nil {
		t.Fatal("NewAuthManager returned nil")
	}

	if authManager.config.Method != "jwt" {
		t.Errorf("Expected method 'jwt', got '%s'", authManager.config.Method)
	}

	if len(authManager.jwtSecret) == 0 {
		t.Error("JWT secret not set")
	}
}

func TestAuthManager_Initialize(t *testing.T) {
	config := AuthConfig{
		Enabled:   true,
		Method:    "jwt",
		JWTSecret: "test-secret",
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)

	err := authManager.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
}

func TestAuthManager_Authenticate_NoToken(t *testing.T) {
	config := AuthConfig{
		Enabled:   true,
		Method:    "jwt",
		JWTSecret: "test-secret",
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)
	authManager.Initialize()

	// Test request without authorization header
	req := httptest.NewRequest("GET", "/test", nil)

	if authManager.Authenticate(req) {
		t.Error("Authentication should fail with missing JWT token")
	}
}

func TestAuthManager_Authenticate_APIKey(t *testing.T) {
	apiKeys := map[string]string{
		"test-key": "test-key-hash",
	}

	config := AuthConfig{
		Enabled:      true,
		Method:       "api-key",
		APIKeyHeader: "X-API-Key",
		APIKeys:      apiKeys,
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)
	authManager.Initialize()

	// Test valid API key
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "test-key")

	if !authManager.Authenticate(req) {
		t.Error("Authentication should succeed with valid API key")
	}

	// Test invalid API key
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "invalid-key")

	if authManager.Authenticate(req) {
		t.Error("Authentication should fail with invalid API key")
	}
}

func TestAuthManager_Login(t *testing.T) {
	config := AuthConfig{
		Enabled:        true,
		Method:         "jwt",
		JWTSecret:      "test-secret",
		JWTIssuer:      "tapio-test",
		JWTExpiration:  1 * time.Hour,
		SessionTimeout: 1 * time.Hour,
		MaxSessions:    5,
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)
	authManager.Initialize()

	// Test login with valid credentials (using simulated user store)
	session, err := authManager.Login("admin", "admin123", "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if session == nil {
		t.Fatal("Login returned nil session")
	}

	if session.Username != "admin" {
		t.Errorf("Expected username 'admin', got '%s'", session.Username)
	}

	if session.IPAddress != "127.0.0.1" {
		t.Errorf("Expected IP '127.0.0.1', got '%s'", session.IPAddress)
	}

	// Test login with invalid credentials
	_, err = authManager.Login("invalid", "invalid", "127.0.0.1", "test-agent")
	if err == nil {
		t.Error("Login should fail with invalid credentials")
	}
}

func TestAuthManager_GenerateJWT(t *testing.T) {
	config := AuthConfig{
		Enabled:       true,
		Method:        "jwt",
		JWTSecret:     "test-secret",
		JWTIssuer:     "tapio-test",
		JWTExpiration: 1 * time.Hour,
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)
	authManager.Initialize()

	session := &Session{
		ID:        "test-session",
		UserID:    "user123",
		Username:  "testuser",
		Roles:     []string{"admin"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	token, err := authManager.GenerateJWT(session)
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}

	if token == "" {
		t.Error("Generated token is empty")
	}

	// Token should be a valid JWT format (three parts separated by dots)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("Expected JWT with 3 parts, got %d", len(parts))
	}
}

func TestAuthManager_GetActiveSessions(t *testing.T) {
	config := AuthConfig{
		Enabled:        true,
		SessionTimeout: 1 * time.Hour,
		MaxSessions:    5,
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)
	authManager.Initialize()

	// Initially should have no sessions
	sessions := authManager.GetActiveSessions()
	if len(sessions) != 0 {
		t.Errorf("Expected 0 active sessions initially, got %d", len(sessions))
	}

	// Test session count
	count := authManager.GetSessionCount()
	if count != 0 {
		t.Errorf("Expected session count 0, got %d", count)
	}
}

func TestAuthManager_AuthorizeRequest(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Method:  "jwt",
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)
	authManager.Initialize()

	// Test authorization without session (should fail)
	req := httptest.NewRequest("GET", "/admin", nil)

	if authManager.AuthorizeRequest(req, "admin") {
		t.Error("Authorization should fail without valid session")
	}
}

func TestAuthManager_HasRole(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Method:  "jwt",
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)
	authManager.Initialize()

	// Test role check without session (should fail)
	req := httptest.NewRequest("GET", "/admin", nil)

	if authManager.HasRole(req, "admin") {
		t.Error("HasRole should return false without valid session")
	}
}

func TestAuthManager_Logout(t *testing.T) {
	config := AuthConfig{
		Enabled:        true,
		SessionTimeout: 1 * time.Hour,
	}

	logger := createTestLogger()
	authManager := NewAuthManager(config, logger)
	authManager.Initialize()

	// Test logout with non-existent session
	err := authManager.Logout("non-existent-session")
	if err == nil {
		t.Error("Logout should return error for non-existent session")
	}
}
