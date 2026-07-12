// Package domain implements core domain types, entities, and interfaces.
//
// File: auth.go
// This file contains implementation and helper structures for core domain types, entities, and interfaces.

package domain

import "time"

type UserRole string

const (
	RoleAdmin      UserRole = "admin"
	RoleGroupAdmin UserRole = "group_admin"
	RoleUser       UserRole = "user"
	RoleViewer     UserRole = "viewer"
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	AuthProvider string    `json:"auth_provider"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Group struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type ApiKey struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Name      string     `json:"name"`
	TokenHash string     `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
