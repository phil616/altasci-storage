package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/altasci/network-storage/backend/internal/database"
	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/go-chi/chi/v5"
)

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := database.Ready(ctx, s.store.DB); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "NOT_READY", "The metadata database is not ready.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" || !s.originAllowed(r.Context(), origin) {
		writeError(w, r, http.StatusForbidden, "CSRF_ORIGIN", "The request origin is not allowed.")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	key := clientIP(r) + "|" + repository.NormalizeEmail(in.Email)
	limit := s.loginRateLimit(r.Context())
	if retry, _ := s.store.BanRemaining(r.Context(), "login", key); retry > 0 {
		w.Header().Set("Retry-After", seconds(retry))
		writeError(w, r, http.StatusTooManyRequests, "LOGIN_RATE_LIMITED", "Too many login attempts. Try again later.")
		return
	}
	u, err := s.store.UserByEmail(r.Context(), in.Email)
	encoded := dummyPasswordHash
	if err == nil && u.PasswordHash.Valid {
		encoded = u.PasswordHash.String
	}
	passwordValid, verifyErr := security.VerifyPassword(encoded, in.Password)
	valid := err == nil && verifyErr == nil && u.Status == "active" && u.PasswordHash.Valid && passwordValid
	if !valid {
		retry, _ := s.store.RecordFailure(r.Context(), "login", key, limit.Attempts, time.Duration(limit.WindowSeconds)*time.Second, time.Duration(limit.CooldownSeconds)*time.Second, 0, 0)
		if retry > 0 {
			w.Header().Set("Retry-After", seconds(retry))
		}
		_ = s.audit(r, "login.failed", "", "", map[string]any{"email_normalized": repository.NormalizeEmail(in.Email)})
		writeError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "The email or password is incorrect.")
		return
	}
	_ = s.store.ClearFailures(r.Context(), "login", key)
	csrf, err := s.establishSession(w, r, u, in.Remember)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.store.MarkLogin(r.Context(), u.ID)
	_ = s.auditUser(r, u.ID, "login.succeeded", "", u.ID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"user": presentUser(u), "csrf_token": csrf})
}

func (s *Server) establishSession(w http.ResponseWriter, r *http.Request, u repository.User, persistent bool) (string, error) {
	token, err := security.RandomToken(32)
	if err != nil {
		return "", err
	}
	csrf, err := security.RandomToken(32)
	if err != nil {
		return "", err
	}
	now := repository.NowMS()
	idle := s.durationSetting(r.Context(), "auth.session_idle_timeout", s.sessionIdle, 5*time.Minute, 30*24*time.Hour)
	absolute := s.durationSetting(r.Context(), "auth.session_absolute_timeout", s.sessionAbsolute, time.Hour, 90*24*time.Hour)
	ss := repository.Session{ID: security.NewID(), UserID: u.ID, TokenHash: security.TokenHash(token), CSRFTokenHash: security.TokenHash(csrf), CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now + idle.Milliseconds(), AbsoluteExpiresAt: now + absolute.Milliseconds(), IP: clientIP(r), UserAgent: truncate(r.UserAgent(), 512)}
	if err := s.store.CreateSession(r.Context(), ss); err != nil {
		return "", err
	}
	cookie := &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	if persistent {
		cookie.MaxAge = int(absolute.Seconds())
	}
	http.SetCookie(w, cookie)
	return csrf, nil
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	ss, _ := sessionFrom(r)
	_ = s.store.RevokeSession(r.Context(), ss.ID)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	writeJSON(w, http.StatusOK, presentUser(u))
}
func (s *Server) csrfToken(w http.ResponseWriter, r *http.Request) {
	ss, _ := sessionFrom(r)
	token, err := security.RandomToken(32)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if err := s.store.RotateCSRF(r.Context(), ss.ID, security.TokenHash(token)); err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"csrf_token": token})
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !u.PasswordHash.Valid {
		writeError(w, r, http.StatusConflict, "PASSWORD_NOT_SET", "This account does not have a local password.")
		return
	}
	ok, _ := security.VerifyPassword(u.PasswordHash.String, in.CurrentPassword)
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "The current password is incorrect.")
		return
	}
	hash, err := security.HashPassword(in.NewPassword)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "PASSWORD_INVALID", err.Error())
		return
	}
	if err := s.store.SetPassword(r.Context(), u.ID, hash); err != nil {
		writeRepoError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" {
		returnTo = s.webURL + "/projects"
	}
	if !safeReturnTo(returnTo, s.webURL) {
		writeError(w, r, http.StatusBadRequest, "RETURN_TO_INVALID", "The return URL is invalid.")
		return
	}
	remember := r.URL.Query().Get("remember") == "true"
	target, err := s.oidc.Start(r.Context(), chi.URLParam(r, "providerID"), returnTo, remember)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}
func (s *Server) oidcProviders(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListOIDCProviders(r.Context(), true)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, len(items))
	for i, provider := range items {
		out[i] = map[string]string{"id": provider.ID, "name": provider.Name}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	result, err := s.oidc.Callback(r.Context(), chi.URLParam(r, "providerID"), r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "OIDC_FAILED", "OpenID Connect authentication failed.")
		return
	}
	if _, err := s.establishSession(w, r, result.User, result.RememberSession); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.store.MarkLogin(r.Context(), result.User.ID)
	_ = s.auditUser(r, result.User.ID, "login.oidc_succeeded", "", result.User.ID, map[string]string{"provider_id": chi.URLParam(r, "providerID")})
	http.Redirect(w, r, result.ReturnTo, http.StatusFound)
}

func safeReturnTo(candidate, base string) bool {
	c, err := url.Parse(candidate)
	if err != nil {
		return false
	}
	b, err := url.Parse(base)
	return err == nil && c.Scheme == b.Scheme && c.Host == b.Host
}
func seconds(d time.Duration) string {
	n := int(d.Seconds())
	if n < 1 {
		n = 1
	}
	return strconv.Itoa(n)
}
func truncate(v string, max int) string {
	if len(v) > max {
		return v[:max]
	}
	return v
}
func (s *Server) audit(r *http.Request, action, projectID, targetID string, metadata any) error {
	u, _ := userFrom(r)
	actorType := "user"
	actorID := u.ID
	if actorID == "" {
		actorType = "public"
	}
	if key, ok := apiKeyFrom(r); ok {
		metadata = map[string]any{"api_key_id": key.ID, "details": metadata}
	}
	return s.store.Audit(r.Context(), security.NewID(), actorType, actorID, action, projectID, targetID, clientIP(r), requestID(r), metadata)
}
func (s *Server) auditUser(r *http.Request, userID, action, projectID, targetID string, metadata any) error {
	return s.store.Audit(r.Context(), security.NewID(), "user", userID, action, projectID, targetID, clientIP(r), requestID(r), metadata)
}

var _ = sql.ErrNoRows
var _ = errors.Is
var _ = strings.TrimSpace
