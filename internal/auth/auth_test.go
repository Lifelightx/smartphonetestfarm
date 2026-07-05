package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestJWTManagerLocalTokens(t *testing.T) {
	secret := "super-secret-key-123456"
	issuer := "protean-test"
	manager := NewJWTManager(secret, issuer, "")

	userID := "user-123"
	email := "user@example.com"
	role := "user"
	groups := []string{"group-A", "group-B"}

	// 1. Generate token
	tokenStr, err := manager.GenerateToken(userID, email, role, groups, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// 2. Verify token
	claims, err := manager.VerifyToken(tokenStr)
	if err != nil {
		t.Fatalf("failed to verify token: %v", err)
	}

	// 3. Assert claims values
	if claims.UserID != userID {
		t.Errorf("expected UserID %s, got %s", userID, claims.UserID)
	}
	if claims.Email != email {
		t.Errorf("expected Email %s, got %s", email, claims.Email)
	}
	if claims.Role != role {
		t.Errorf("expected Role %s, got %s", role, claims.Role)
	}
	if len(claims.Groups) != 2 || claims.Groups[0] != "group-A" || claims.Groups[1] != "group-B" {
		t.Errorf("unexpected groups: %v", claims.Groups)
	}
	if claims.Issuer != issuer {
		t.Errorf("expected issuer %s, got %s", issuer, claims.Issuer)
	}
}

func TestJWTManagerInvalidToken(t *testing.T) {
	manager := NewJWTManager("secret1", "issuer", "")
	otherManager := NewJWTManager("secret2", "issuer", "")

	tokenStr, _ := manager.GenerateToken("123", "a@b.com", "user", nil, 10*time.Minute)

	// Verify with wrong key
	_, err := otherManager.VerifyToken(tokenStr)
	if err == nil {
		t.Error("expected verification error with wrong key, got nil")
	}

	// Verify expired token
	expiredStr, _ := manager.GenerateToken("123", "a@b.com", "user", nil, -10*time.Minute)
	_, err = manager.VerifyToken(expiredStr)
	if err == nil {
		t.Error("expected verification error with expired token, got nil")
	}
}

func TestPasswordHashing(t *testing.T) {
	password := "my-secure-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	// Correct password
	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	if err != nil {
		t.Errorf("expected match, got error: %v", err)
	}

	// Incorrect password
	err = bcrypt.CompareHashAndPassword(hash, []byte("wrong-password"))
	if err == nil {
		t.Error("expected error for incorrect password comparison, got nil")
	}
}

func TestApiKeyHashing(t *testing.T) {
	rawToken := "pt_live_abcdef123456"
	h := sha256.New()
	h.Write([]byte(rawToken))
	tokenHash := hex.EncodeToString(h.Sum(nil))

	// Verify hash match
	h2 := sha256.New()
	h2.Write([]byte(rawToken))
	expectedHash := hex.EncodeToString(h2.Sum(nil))

	if tokenHash != expectedHash {
		t.Errorf("hash mismatch: got %s, want %s", tokenHash, expectedHash)
	}
}
