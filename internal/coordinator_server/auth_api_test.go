package coordinator_server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestAuthenticationFlows(t *testing.T) {
	cfg := LoadConfig()
	cfg.BypassAuthInDev = false // Force authentication enforcement for this test

	db, err := OpenDB(cfg.PostgresURI)
	if err != nil {
		t.Fatalf("failed to open database for test: %v", err)
	}

	// Clean up existing tables to ensure a clean state
	_, _ = db.RawDB().Exec("DELETE FROM api_keys")
	_, _ = db.RawDB().Exec("DELETE FROM user_groups")
	_, _ = db.RawDB().Exec("DELETE FROM groups")
	_, _ = db.RawDB().Exec("DELETE FROM users")

	server := NewServer(cfg, db)

	// Build the router exactly as in server.go
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/devices", server.handleListDevices)
	server.authService.RegisterHandlers(mux)

	// Wrap with authentication middleware
	handler := server.authService.AuthMiddleware(cfg.BypassAuthInDev)(mux)

	// 1. Verify access without token is blocked (401 Unauthorized)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized without credentials, got %d", w.Result().StatusCode)
	}

	// 2. Register first admin user (allowed via userless bootstrap)
	adminEmail := "admin-" + uuid.New().String() + "@apmosys.com"
	regBody := map[string]interface{}{
		"email":    adminEmail,
		"password": "super-secure-admin-password",
		"role":     "admin",
	}
	bodyBytes, _ := json.Marshal(regBody)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on bootstrap register, got %d", w.Result().StatusCode)
	}

	// 3. Register second user without admin authorization (must be Forbidden)
	userEmail := "user-" + uuid.New().String() + "@apmosys.com"
	regBodyUser := map[string]interface{}{
		"email":    userEmail,
		"password": "user-password",
		"role":     "user",
	}
	bodyBytesUser, _ := json.Marshal(regBodyUser)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytesUser))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for anonymous register when users exist, got %d", w.Result().StatusCode)
	}

	// 4. Log in as admin and acquire JWT token
	loginBody := map[string]interface{}{
		"email":    adminEmail,
		"password": "super-secure-admin-password",
	}
	loginBytes, _ := json.Marshal(loginBody)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBytes))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on login, got %d", w.Result().StatusCode)
	}

	var loginResp map[string]string
	_ = json.NewDecoder(w.Result().Body).Decode(&loginResp)
	adminToken := loginResp["token"]
	if adminToken == "" {
		t.Fatal("login response did not contain token")
	}

	// 5. Query protected API with admin JWT token (must be allowed)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK with valid JWT token, got %d", w.Result().StatusCode)
	}

	// 6. Generate an API Key using admin JWT token
	keyReqBody := map[string]interface{}{
		"name":            "CI Runner Key",
		"expires_in_days": 30,
	}
	keyReqBytes, _ := json.Marshal(keyReqBody)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/keys", bytes.NewReader(keyReqBytes))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on key creation, got %d", w.Result().StatusCode)
	}

	var keyResp map[string]interface{}
	_ = json.NewDecoder(w.Result().Body).Decode(&keyResp)
	apiKey := keyResp["key"].(string)
	if apiKey == "" {
		t.Fatal("API key creation response did not contain key")
	}

	// 7. Query protected API using the X-API-Key header (must be allowed)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("X-API-Key", apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK with valid API key, got %d", w.Result().StatusCode)
	}
}
