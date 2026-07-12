// Package auth implements authentication, user sessions, JWT handling, and middleware.
//
// File: middleware.go
// This file contains implementation and helper structures for authentication, user sessions, JWT handling, and middleware.

package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"protean-provider/internal/domain"
)

// AuthMiddleware intercepts HTTP requests to authenticate and authorize users/api keys
func (s *AuthService) AuthMiddleware(bypassInDev bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Exclude routes that don't need auth (e.g. login endpoint, health checks, Swagger UI)
			path := r.URL.Path
			if path == "/api/v1/auth/login" || path == "/healthz" || path == "/docs" || path == "/swagger.yaml" {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Check Authorization header or token query parameter
			authHeader := r.Header.Get("Authorization")
			tokenStr := ""
			if authHeader != "" {
				parts := strings.Split(authHeader, " ")
				if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
					tokenStr = parts[1]
				}
			} else {
				tokenStr = r.URL.Query().Get("token")
			}

			var userInfo UserInfo
			authenticated := false

			if tokenStr != "" {
				claims, err := s.jwtManager.VerifyToken(tokenStr)
				if err == nil {
					userInfo = UserInfo{
						ID:     claims.UserID,
						Email:  claims.Email,
						Role:   claims.Role,
						Groups: claims.Groups,
					}
					authenticated = true
				} else {
					slog.Warn("auth: invalid JWT token", "err", err)
				}
			}

			// 2. Fallback to API Key header if not authenticated via JWT
			if !authenticated {
				apiKey := r.Header.Get("X-API-Key")
				if apiKey != "" {
					user, err := s.VerifyApiKey(apiKey)
					if err == nil {
						// Fetch groups
						groups, _ := s.db.GetUserGroups(user.ID)
						var groupNames []string
						for _, g := range groups {
							groupNames = append(groupNames, g.Name)
						}

						userInfo = UserInfo{
							ID:     user.ID,
							Email:  user.Email,
							Role:   string(user.Role),
							Groups: groupNames,
						}
						authenticated = true
					} else {
						slog.Warn("auth: invalid API Key", "err", err)
					}
				}
			}

			// 3. Handle authentication check
			if !authenticated {
				if bypassInDev {
					// Backward compatibility bypass: inject mock super admin user
					mockUser := UserInfo{
						ID:     "00000000-0000-0000-0000-000000000000",
						Email:  "admin@domain.com",
						Role:   string(domain.RoleAdmin),
						Groups: []string{"Public"},
					}
					slog.Debug("auth: bypass active, injecting mock admin user")
					ctx := NewContext(r.Context(), mockUser)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				if path == "/api/v1/auth/register" {
					// Allow unauthenticated requests to reach register handler,
					// which enforces its own check on whether DB is empty.
					next.ServeHTTP(w, r)
					return
				}

				// Return 401 Unauthorized
				http.Error(w, "Unauthorized: valid session token or API key required", http.StatusUnauthorized)
				return
			}

			// 4. Inject authenticated user into request context
			ctx := NewContext(r.Context(), userInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
