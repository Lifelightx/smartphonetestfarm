package coordinator_server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"protean-provider/internal/auth"
	"protean-provider/internal/domain"
)

func (s *Server) checkAdmin(w http.ResponseWriter, r *http.Request) bool {
	userInfo, ok := auth.FromContext(r.Context())
	if !ok || userInfo.Role != string(domain.RoleAdmin) {
		http.Error(w, "Forbidden: administrator privileges required", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdmin(w, r) {
		return
	}

	if r.Method == http.MethodGet {
		users, err := s.db.ListUsers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(users)
		return
	}

	if r.Method == http.MethodDelete {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id query parameter is required", http.StatusBadRequest)
			return
		}
		if err := s.db.DeleteUser(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleAdminGroups(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdmin(w, r) {
		return
	}

	if r.Method == http.MethodGet {
		groups, err := s.db.ListGroups()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(groups)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Name        string     `json:"name"`
			Description string     `json:"description"`
			ExpiresAt   *time.Time `json:"expires_at,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		g := &domain.Group{
			ID:          uuid.New().String(),
			Name:        req.Name,
			Description: req.Description,
			CreatedAt:   time.Now(),
			ExpiresAt:   req.ExpiresAt,
		}
		if err := s.db.CreateGroup(g); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(g)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleAdminGroupAction(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdmin(w, r) {
		return
	}

	relPath := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/groups/")
	parts := strings.Split(relPath, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Group ID is required", http.StatusBadRequest)
		return
	}
	groupID := parts[0]

	// 1. DELETE /api/v1/admin/groups/{group_id}
	if len(parts) == 1 {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.db.DeleteGroup(groupID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	subResource := parts[1]

	// 2. Users resource
	if subResource == "users" {
		if len(parts) == 2 {
			if r.Method == http.MethodGet {
				users, err := s.db.GetGroupUsers(groupID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(users)
				return
			}

			// POST /api/v1/admin/groups/{group_id}/users
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				UserID string `json:"user_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			if req.UserID == "" {
				http.Error(w, "user_id is required", http.StatusBadRequest)
				return
			}
			if err := s.db.AddUserToGroup(req.UserID, groupID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
			return
		}

		if len(parts) == 3 {
			// DELETE /api/v1/admin/groups/{group_id}/users/{user_id}
			if r.Method != http.MethodDelete {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			userID := parts[2]
			if err := s.db.RemoveUserFromGroup(userID, groupID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
			return
		}
	}

	// 3. Devices resource
	if subResource == "devices" {
		if len(parts) == 2 {
			if r.Method == http.MethodGet {
				devices, err := s.db.GetGroupDevices(groupID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(devices)
				return
			}

			// POST /api/v1/admin/groups/{group_id}/devices
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				Serial string `json:"serial"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			if req.Serial == "" {
				http.Error(w, "serial is required", http.StatusBadRequest)
				return
			}
			if err := s.db.AddDeviceToGroup(req.Serial, groupID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
			return
		}

		if len(parts) == 3 {
			// DELETE /api/v1/admin/groups/{group_id}/devices/{serial}
			if r.Method != http.MethodDelete {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			serial := parts[2]
			if err := s.db.RemoveDeviceFromGroup(serial, groupID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
			return
		}
	}

	http.Error(w, "Not found", http.StatusNotFound)
}
