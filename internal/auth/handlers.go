package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"protean-provider/internal/domain"
)

func (s *AuthService) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("/api/v1/auth/keys", s.handleKeys)
}

func (s *AuthService) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	token, err := s.Login(req.Email, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
	})
}

func (s *AuthService) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Check if database has users
	// We can count users or check if any user exists
	usersCount := 0
	row := s.db.RawDB().QueryRow("SELECT COUNT(*) FROM users")
	_ = row.Scan(&usersCount)

	// If users exist, enforce admin authentication
	if usersCount > 0 {
		userInfo, ok := FromContext(r.Context())
		if !ok || userInfo.Role != string(domain.RoleAdmin) {
			http.Error(w, "Forbidden: only administrator can register new users", http.StatusForbidden)
			return
		}
	}

	var req struct {
		Email    string   `json:"email"`
		Password string   `json:"password"`
		Role     string   `json:"role"`
		Groups   []string `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Role == "" {
		req.Role = string(domain.RoleUser)
	}

	// Enforce role constraints (only admin can create admins)
	if req.Role == string(domain.RoleAdmin) && usersCount > 0 {
		userInfo, _ := FromContext(r.Context())
		if userInfo.Role != string(domain.RoleAdmin) {
			http.Error(w, "Forbidden: only admins can create admin users", http.StatusForbidden)
			return
		}
	}

	if req.Groups == nil {
		req.Groups = []string{"Public"}
	}

	user, err := s.RegisterLocalUser(req.Email, req.Password, domain.UserRole(req.Role), req.Groups)
	if err != nil {
		http.Error(w, "registration failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"user":    user,
	})
}

func (s *AuthService) handleKeys(w http.ResponseWriter, r *http.Request) {
	userInfo, ok := FromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Name      string `json:"name"`
			ExpiresIn int    `json:"expires_in_days"` // optional
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		var expiration *time.Time
		if req.ExpiresIn > 0 {
			exp := time.Now().AddDate(0, 0, req.ExpiresIn)
			expiration = &exp
		}

		rawToken, err := s.CreateApiKeyForUser(userInfo.ID, req.Name, expiration)
		if err != nil {
			http.Error(w, "failed to create api key: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    req.Name,
			"key":     rawToken,
			"message": "Write this key down. It will not be shown again.",
		})
		return
	}

	if r.Method == http.MethodGet {
		keys, err := s.db.ListApiKeys(userInfo.ID)
		if err != nil {
			http.Error(w, "failed to list keys: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(keys)
		return
	}

	if r.Method == http.MethodDelete {
		keyID := r.URL.Query().Get("id")
		if keyID == "" {
			http.Error(w, "id parameter required", http.StatusBadRequest)
			return
		}

		// Delete key (make sure it belongs to the active user, or user is admin)
		// Since we delete by ID, we can do a simple verification check:
		// Actually, let's verify key ownership or let database/query handle it.
		// Let's implement a secure delete in auth service or just query and verify ownership:
		keys, err := s.db.ListApiKeys(userInfo.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		owned := false
		for _, k := range keys {
			if k.ID == keyID {
				owned = true
				break
			}
		}

		if !owned && userInfo.Role != string(domain.RoleAdmin) {
			http.Error(w, "Forbidden: you do not own this API key", http.StatusForbidden)
			return
		}

		if err := s.db.DeleteApiKey(keyID); err != nil {
			http.Error(w, "failed to delete api key: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
