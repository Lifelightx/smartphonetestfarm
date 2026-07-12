// Package auth implements authentication, user sessions, JWT handling, and middleware.
//
// File: context.go
// This file contains implementation and helper structures for authentication, user sessions, JWT handling, and middleware.

package auth

import "context"

type contextKey string

const UserContextKey contextKey = "user_info"

type UserInfo struct {
	ID     string
	Email  string
	Role   string
	Groups []string
}

// FromContext extracts UserInfo from a context if present.
func FromContext(ctx context.Context) (UserInfo, bool) {
	u, ok := ctx.Value(UserContextKey).(UserInfo)
	return u, ok
}

// NewContext returns a new context with the given UserInfo.
func NewContext(ctx context.Context, u UserInfo) context.Context {
	return context.WithValue(ctx, UserContextKey, u)
}
