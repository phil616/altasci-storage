package httpapi

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)
var sharePathPattern = regexp.MustCompile(`/public/shares/[^/]+`)

func (s *Server) baseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rid := r.Header.Get("X-Request-ID")
		if !s.isTrustedProxy(r) || !requestIDPattern.MatchString(rid) {
			rid, _ = security.RandomToken(12)
		}
		ip := s.resolveClientIP(r)
		ctx := context.WithValue(context.WithValue(r.Context(), requestIDKey, rid), clientIPKey, ip)
		w.Header().Set("X-Request-ID", rid)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r.WithContext(ctx))
		path := sharePathPattern.ReplaceAllString(r.URL.Path, "/public/shares/[REDACTED]")
		s.log.Info("http request", "request_id", rid, "method", r.Method, "path", path, "status", rec.status, "duration_ms", time.Since(started).Milliseconds(), "client_ip", ip)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic recovered", "request_id", requestID(r), "panic", v)
				writeError(w, r, 500, "INTERNAL", "An internal error occurred.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Vary", "Origin")
		}
		if origin != "" && s.originAllowed(r.Context(), origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Authorization, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if origin == "" || !s.originAllowed(r.Context(), origin) {
				writeError(w, r, http.StatusForbidden, "ORIGIN_FORBIDDEN", "The request origin is not allowed.")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) originAllowed(ctx context.Context, origin string) bool {
	var allowed []string
	if err := s.store.Setting(ctx, "cors.allowed_origins", &allowed); err != nil {
		return false
	}
	for _, v := range allowed {
		if subtle.ConstantTimeCompare([]byte(v), []byte(origin)) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.")
			return
		}
		now := repository.NowMS()
		ss, u, err := s.store.SessionByTokenHash(r.Context(), security.TokenHash(cookie.Value), now)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.")
			return
		}
		if now-ss.LastSeenAt > 5*time.Minute.Milliseconds() {
			idle := s.durationSetting(r.Context(), "auth.session_idle_timeout", s.sessionIdle, 5*time.Minute, 30*24*time.Hour)
			expiry := now + idle.Milliseconds()
			if expiry > ss.AbsoluteExpiresAt {
				expiry = ss.AbsoluteExpiresAt
			}
			_ = s.store.TouchSession(r.Context(), ss.ID, expiry)
		}
		next.ServeHTTP(w, r.WithContext(withAuth(r.Context(), u, ss)))
	})
}
func (s *Server) admin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r)
		if !ok || u.Role != "admin" {
			writeError(w, r, http.StatusForbidden, "ADMIN_REQUIRED", "Administrator access is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" || !s.originAllowed(r.Context(), origin) {
			writeError(w, r, http.StatusForbidden, "CSRF_ORIGIN", "The request origin is not allowed.")
			return
		}
		ss, ok := sessionFrom(r)
		if !ok || subtle.ConstantTimeCompare([]byte(security.TokenHash(r.Header.Get("X-CSRF-Token"))), []byte(ss.CSRFTokenHash)) != 1 {
			writeError(w, r, http.StatusForbidden, "CSRF_INVALID", "The CSRF token is invalid.")
			return
		}
		isLocalUpload := r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/uploads/") && strings.HasSuffix(r.URL.Path, "/content")
		if ct := r.Header.Get("Content-Type"); r.Method != http.MethodDelete && !isLocalUpload && !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "application/octet-stream") {
			writeError(w, r, http.StatusUnsupportedMediaType, "CONTENT_TYPE_INVALID", "A supported Content-Type is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) resolveClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "unknown"
	}
	if s.isTrustedProxy(r) {
		for _, header := range []string{"CF-Connecting-IP", "X-Real-IP", "X-Forwarded-For"} {
			v := strings.TrimSpace(strings.Split(r.Header.Get(header), ",")[0])
			if parsed := net.ParseIP(v); parsed != nil {
				return parsed.String()
			}
		}
	}
	return ip.String()
}
func (s *Server) isTrustedProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	var cidrs []string
	return s.store.Setting(r.Context(), "security.trusted_proxy_cidrs", &cidrs) == nil && inCIDRs(ip, cidrs)
}
func inCIDRs(ip net.IP, cidrs []string) bool {
	for _, raw := range cidrs {
		_, network, err := net.ParseCIDR(raw)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

var _ = slog.LevelInfo
