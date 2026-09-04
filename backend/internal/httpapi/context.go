package httpapi

import (
	"context"
	"net/http"

	"github.com/altasci/network-storage/backend/internal/repository"
)

type contextKey string

const (
	userKey      contextKey = "user"
	sessionKey   contextKey = "session"
	requestIDKey contextKey = "request_id"
	clientIPKey  contextKey = "client_ip"
)

func userFrom(r *http.Request) (repository.User, bool) {
	u, ok := r.Context().Value(userKey).(repository.User)
	return u, ok
}
func sessionFrom(r *http.Request) (repository.Session, bool) {
	s, ok := r.Context().Value(sessionKey).(repository.Session)
	return s, ok
}
func requestID(r *http.Request) string { v, _ := r.Context().Value(requestIDKey).(string); return v }
func clientIP(r *http.Request) string  { v, _ := r.Context().Value(clientIPKey).(string); return v }
func withAuth(ctx context.Context, u repository.User, s repository.Session) context.Context {
	return context.WithValue(context.WithValue(ctx, userKey, u), sessionKey, s)
}
