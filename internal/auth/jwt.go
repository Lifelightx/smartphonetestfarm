// Package auth implements authentication, user sessions, JWT handling, and middleware.
//
// File: jwt.go
// This file contains implementation and helper structures for authentication, user sessions, JWT handling, and middleware.

package auth

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	jwt.RegisteredClaims
	UserID string   `json:"user_id"`
	Email  string   `json:"email"`
	Role   string   `json:"role"`
	Groups []string `json:"groups"`
}

type JWTManager struct {
	secret      []byte
	issuer      string
	oidcJWKSURL string

	// JWKS cache
	mu        sync.RWMutex
	jwkCache  map[string]*rsa.PublicKey
	lastFetch time.Time
}

type JWK struct {
	Kty string   `json:"kty"`
	Kid string   `json:"kid"`
	Use string   `json:"use"`
	Alg string   `json:"alg"`
	N   string   `json:"n"`
	E   string   `json:"e"`
	X5c []string `json:"x5c"`
}

type JWKS struct {
	Keys []JWK `json:"keys"`
}

// NewJWTManager initializes a new jwtmanager.
func NewJWTManager(secret string, issuer string, oidcJWKSURL string) *JWTManager {
	return &JWTManager{
		secret:      []byte(secret),
		issuer:      issuer,
		oidcJWKSURL: oidcJWKSURL,
		jwkCache:    make(map[string]*rsa.PublicKey),
	}
}

// GenerateToken generates a standard signed HS256 JWT for local authentications
func (m *JWTManager) GenerateToken(userID string, email string, role string, groups []string, duration time.Duration) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
		UserID: userID,
		Email:  email,
		Role:   role,
		Groups: groups,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// VerifyToken checks the token. Supports both Local HS256 and OIDC RS256 tokens.
func (m *JWTManager) VerifyToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// 1. Detect algorithm
		if token.Method.Alg() == "HS256" {
			// Local token
			return m.secret, nil
		}

		if token.Method.Alg() == "RS256" {
			// OIDC/SSO token
			if m.oidcJWKSURL == "" {
				return nil, errors.New("OIDC RS256 token received but OIDC JWKS URL is not configured")
			}

			kidVal, ok := token.Header["kid"]
			if !ok {
				return nil, errors.New("missing kid header in RS256 token")
			}
			kid, ok := kidVal.(string)
			if !ok {
				return nil, errors.New("kid header is not a string")
			}

			pubKey, err := m.getOIDCPublicKey(kid)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch public key for kid %s: %w", kid, err)
			}
			return pubKey, nil
		}

		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid JWT claims or token is expired")
	}

	return claims, nil
}

// getOIDCPublicKey retrieves the oidcpublic key.
func (m *JWTManager) getOIDCPublicKey(kid string) (*rsa.PublicKey, error) {
	m.mu.RLock()
	key, exists := m.jwkCache[kid]
	lastFetched := m.lastFetch
	m.mu.RUnlock()

	// Cache hit, and key is less than 1 hour old
	if exists && time.Since(lastFetched) < 1*time.Hour {
		return key, nil
	}

	// Cache miss or expired cache, fetch JWKS from provider
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check under write lock
	if key, exists = m.jwkCache[kid]; exists && time.Since(m.lastFetch) < 1*time.Hour {
		return key, nil
	}

	if err := m.fetchJWKS(); err != nil {
		// If fetch fails but we have the cached key, return it as a fallback
		if exists {
			return key, nil
		}
		return nil, err
	}

	key, exists = m.jwkCache[kid]
	if !exists {
		return nil, fmt.Errorf("public key not found in JWKS for kid: %s", kid)
	}

	return key, nil
}

// fetchJWKS performs the fetch jwks operation.
func (m *JWTManager) fetchJWKS() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(m.oidcJWKSURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected JWKS status code: %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}

	newCache := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}
		// Convert JWK to rsa.PublicKey
		if len(key.X5c) > 0 {
			// Parsing using the first certificate in X5c
			certStr := fmt.Sprintf("-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----", key.X5c[0])
			block, err := jwt.ParseRSAPublicKeyFromPEM([]byte(certStr))
			if err == nil {
				newCache[key.Kid] = block
			}
		}
	}

	if len(newCache) == 0 {
		return errors.New("no valid RSA public keys found in JWKS")
	}

	m.jwkCache = newCache
	m.lastFetch = time.Now()
	return nil
}
