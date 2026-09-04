package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/altasci/network-storage/backend/internal/config"
	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/go-chi/chi/v5"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, len(users))
	for i, u := range users {
		out[i] = presentUser(u)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		WriteEnabled bool   `json:"write_enabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if repository.NormalizeEmail(in.Email) == "" || !strings.Contains(in.Email, "@") {
		writeError(w, r, 422, "EMAIL_INVALID", "A valid email address is required.")
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		writeError(w, r, 422, "PASSWORD_INVALID", err.Error())
		return
	}
	now := repository.NowMS()
	u := repository.User{ID: security.NewID(), Email: strings.TrimSpace(in.Email), PasswordHash: sql.NullString{String: hash, Valid: true}, Role: "user", WriteEnabled: in.WriteEnabled, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "user.created", "", u.ID, map[string]any{"write_enabled": u.WriteEnabled})
	writeJSON(w, http.StatusCreated, presentUser(u))
}
func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.UserByID(r.Context(), chi.URLParam(r, "userID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, presentUser(u))
}
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email        *string `json:"email"`
		WriteEnabled *bool   `json:"write_enabled"`
		Status       *string `json:"status"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "disabled" {
		writeError(w, r, 422, "STATUS_INVALID", "Status must be active or disabled.")
		return
	}
	if in.Email != nil && (repository.NormalizeEmail(*in.Email) == "" || !strings.Contains(*in.Email, "@")) {
		writeError(w, r, 422, "EMAIL_INVALID", "A valid email address is required.")
		return
	}
	u, err := s.store.UpdateUser(r.Context(), chi.URLParam(r, "userID"), in.Email, in.WriteEnabled, in.Status)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "user.updated", "", u.ID, map[string]any{"write_enabled": u.WriteEnabled, "status": u.Status})
	writeJSON(w, http.StatusOK, presentUser(u))
}
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		writeError(w, r, 422, "PASSWORD_INVALID", err.Error())
		return
	}
	id := chi.URLParam(r, "userID")
	if err := s.store.SetPassword(r.Context(), id, hash); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "user.password_reset", "", id, nil)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) revokeSessions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	if err := s.store.RevokeUserSessions(r.Context(), id); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "user.sessions_revoked", "", id, nil)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) transferAdmin(w http.ResponseWriter, r *http.Request) {
	current, _ := userFrom(r)
	var in struct {
		NewAdminID      string `json:"new_admin_id"`
		CurrentPassword string `json:"current_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ok := false
	if current.PasswordHash.Valid {
		ok, _ = security.VerifyPassword(current.PasswordHash.String, in.CurrentPassword)
	}
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "The current password is incorrect.")
		return
	}
	if err := s.store.TransferAdmin(r.Context(), current.ID, in.NewAdminID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "admin.transferred", "", in.NewAdminID, map[string]string{"previous_admin_id": current.ID})
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

type storageInput struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config"`
	Secret  json.RawMessage `json:"secret"`
}

func presentStorage(b repository.StorageBackend) map[string]any {
	var cfg any
	_ = json.Unmarshal([]byte(b.ConfigJSON), &cfg)
	return map[string]any{"id": b.ID, "name": b.Name, "type": b.Type, "enabled": b.Enabled, "config": cfg, "has_secret": b.SecretCiphertext.Valid, "project_count": b.ProjectCount, "blob_count": b.BlobCount, "created_at": rfc(b.CreatedAt), "updated_at": rfc(b.UpdatedAt), "last_test_at": nullRFC(b.LastTestAt), "last_test_status": nullable(b.LastTestStatus), "last_test_message": nullable(b.LastTestMessage)}
}
func nullable(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}
func validateStorageInput(in storageInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name is required")
	}
	if in.Type != "local" && in.Type != "s3" && in.Type != "aliyun_oss" {
		return errors.New("unsupported storage type")
	}
	if !json.Valid(in.Config) {
		return errors.New("config must be valid JSON")
	}
	if in.Type != "local" && len(in.Secret) == 0 {
		return errors.New("credentials are required")
	}
	return nil
}
func (s *Server) listStorage(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListStorageBackends(r.Context())
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, len(items))
	for i, b := range items {
		out[i] = presentStorage(b)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
func (s *Server) createStorage(w http.ResponseWriter, r *http.Request) {
	var in storageInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := validateStorageInput(in); err != nil {
		writeError(w, r, 422, "STORAGE_INVALID", err.Error())
		return
	}
	id := security.NewID()
	var cipher sql.NullString
	if len(in.Secret) > 0 && string(in.Secret) != "null" {
		value, err := s.box.Encrypt(in.Secret, "storage-backend:"+id+":v1")
		if err != nil {
			writeRepoError(w, r, err)
			return
		}
		cipher = sql.NullString{String: value, Valid: true}
	}
	now := repository.NowMS()
	b := repository.StorageBackend{ID: id, Name: strings.TrimSpace(in.Name), Type: in.Type, Enabled: in.Enabled, ConfigJSON: string(in.Config), SecretCiphertext: cipher, CreatedAt: now, UpdatedAt: now}
	if _, err := s.factory.FromBackend(r.Context(), b); err != nil {
		writeError(w, r, 422, "STORAGE_CONFIG_INVALID", err.Error())
		return
	}
	if err := s.store.CreateStorageBackend(r.Context(), b); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "storage.created", "", b.ID, map[string]string{"type": b.Type})
	writeJSON(w, 201, presentStorage(b))
}
func (s *Server) getStorage(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.StorageBackendByID(r.Context(), chi.URLParam(r, "backendID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, 200, presentStorage(b))
}
func (s *Server) updateStorage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "backendID")
	existing, err := s.store.StorageBackendByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	var in struct {
		Name    string          `json:"name"`
		Enabled bool            `json:"enabled"`
		Config  json.RawMessage `json:"config"`
		Secret  json.RawMessage `json:"secret,omitempty"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || !json.Valid(in.Config) {
		writeError(w, r, 422, "STORAGE_INVALID", "Name and a valid config are required.")
		return
	}
	var cipher *string
	if len(in.Secret) > 0 && string(in.Secret) != "null" {
		v, err := s.box.Encrypt(in.Secret, "storage-backend:"+id+":v1")
		if err != nil {
			writeRepoError(w, r, err)
			return
		}
		cipher = &v
	}
	candidate := existing
	candidate.Name, candidate.ConfigJSON, candidate.Enabled = in.Name, string(in.Config), in.Enabled
	if cipher != nil {
		candidate.SecretCiphertext = sql.NullString{String: *cipher, Valid: true}
	}
	if _, err := s.factory.FromBackend(r.Context(), candidate); err != nil {
		writeError(w, r, 422, "STORAGE_CONFIG_INVALID", err.Error())
		return
	}
	updated, err := s.store.UpdateStorageBackend(r.Context(), id, in.Name, string(in.Config), in.Enabled, cipher)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "storage.updated", "", id, nil)
	writeJSON(w, 200, presentStorage(updated))
}
func (s *Server) deleteStorage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "backendID")
	backend, err := s.store.StorageBackendByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if backend.ProjectCount > 0 || backend.BlobCount > 0 {
		writeError(w, r, http.StatusConflict, "STORAGE_BACKEND_IN_USE", fmt.Sprintf("Storage backend is referenced by %d project(s) and %d object record(s).", backend.ProjectCount, backend.BlobCount))
		return
	}
	if err := s.store.DeleteStorageBackend(r.Context(), id); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "storage.deleted", "", id, nil)
	w.WriteHeader(204)
}
func (s *Server) testStorage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "backendID")
	adapter, _, err := s.factory.ByID(r.Context(), id)
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		err = adapter.Test(ctx)
	}
	if err != nil {
		_ = s.store.SetStorageTest(r.Context(), id, "failed", truncate(err.Error(), 512))
		writeError(w, r, 502, "STORAGE_TEST_FAILED", "The storage connection test failed.")
		return
	}
	_ = s.store.SetStorageTest(r.Context(), id, "ok", "PUT, HEAD, GET and DELETE succeeded")
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

var allowedSettings = map[string]bool{"site.public_web_url": true, "site.public_api_url": true, "cors.allowed_origins": true, "auth.session_idle_timeout": true, "auth.session_absolute_timeout": true, "storage.upload_presign_ttl": true, "storage.download_presign_ttl": true, "share.default_expiration": true, "share.download_presign_ttl": true, "security.trusted_proxy_cidrs": true, "security.share_rate_limit": true, "security.login_rate_limit": true}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.AllSettings(r.Context())
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]json.RawMessage
	if !decodeJSON(w, r, &in) {
		return
	}
	u, _ := userFrom(r)
	keys := make([]string, 0, len(in))
	for key, raw := range in {
		if !allowedSettings[key] || !json.Valid(raw) {
			writeError(w, r, 422, "SETTING_INVALID", "An unknown or invalid setting was supplied.")
			return
		}
		if err := validateSetting(key, raw); err != nil {
			writeError(w, r, 422, "SETTING_INVALID", err.Error())
			return
		}
		if strings.HasPrefix(key, "site.public_") {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				writeError(w, r, 422, "SETTING_INVALID", "Public URLs must be strings.")
				return
			}
			if err := config.ValidatePublicURL(value); err != nil {
				writeError(w, r, 422, "SETTING_INVALID", "Public URLs must be exact HTTPS origins without a path or trailing slash.")
				return
			}
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			writeError(w, r, 422, "SETTING_INVALID", "Invalid setting value.")
			return
		}
		if err := s.store.SetSetting(r.Context(), key, value, u.ID); err != nil {
			writeRepoError(w, r, err)
			return
		}
		keys = append(keys, key)
	}
	_ = s.audit(r, "settings.updated", "", "", map[string]any{"keys": keys})
	w.WriteHeader(204)
}

func validateSetting(key string, raw json.RawMessage) error {
	switch key {
	case "cors.allowed_origins":
		var values []string
		if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
			return errors.New("cors.allowed_origins must be a non-empty string array")
		}
		for _, value := range values {
			u, err := url.Parse(value)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				return fmt.Errorf("invalid HTTPS CORS origin %q", value)
			}
		}
	case "security.trusted_proxy_cidrs":
		var values []string
		if json.Unmarshal(raw, &values) != nil {
			return errors.New("trusted proxy CIDRs must be a string array")
		}
		for _, value := range values {
			if _, _, err := net.ParseCIDR(value); err != nil {
				return fmt.Errorf("invalid trusted proxy CIDR %q", value)
			}
		}
	case "auth.session_idle_timeout", "auth.session_absolute_timeout", "storage.upload_presign_ttl", "storage.download_presign_ttl", "share.default_expiration", "share.download_presign_ttl":
		var seconds float64
		if json.Unmarshal(raw, &seconds) != nil || seconds < 1 {
			return errors.New("duration settings must be positive JSON seconds")
		}
	case "security.login_rate_limit":
		var value loginRateLimit
		if json.Unmarshal(raw, &value) != nil || value.Attempts < 1 || value.WindowSeconds < 10 || value.CooldownSeconds < 10 {
			return errors.New("invalid login rate limit")
		}
	case "security.share_rate_limit":
		var value shareRateLimit
		if json.Unmarshal(raw, &value) != nil || value.IPAttempts < 1 || value.IPWindowSeconds < 10 || value.IPBanSeconds < 10 || value.EscalatedBanSeconds < value.IPBanSeconds || value.ShareAttempts < 1 || value.ShareWindowSeconds < 10 || value.ShareBanSeconds < 10 {
			return errors.New("invalid share rate limit")
		}
	}
	return nil
}

type oidcInput struct {
	Name                  string   `json:"name"`
	Issuer                string   `json:"issuer"`
	ClientID              string   `json:"client_id"`
	ClientSecret          string   `json:"client_secret,omitempty"`
	Scopes                string   `json:"scopes"`
	Enabled               bool     `json:"enabled"`
	AutoCreateUser        bool     `json:"auto_create_user"`
	AutoLinkVerifiedEmail bool     `json:"auto_link_verified_email"`
	AllowedEmailDomains   []string `json:"allowed_email_domains"`
}

func presentOIDC(p repository.OIDCProvider) map[string]any {
	var domains []string
	_ = json.Unmarshal([]byte(p.AllowedEmailDomains), &domains)
	return map[string]any{"id": p.ID, "name": p.Name, "issuer": p.Issuer, "client_id": p.ClientID, "has_client_secret": p.ClientSecretCiphertext.Valid, "scopes": p.Scopes, "enabled": p.Enabled, "auto_create_user": p.AutoCreateUser, "auto_link_verified_email": p.AutoLinkVerifiedEmail, "allowed_email_domains": domains, "created_at": rfc(p.CreatedAt), "updated_at": rfc(p.UpdatedAt)}
}
func makeOIDC(in oidcInput, id string) (repository.OIDCProvider, error) {
	name := strings.TrimSpace(in.Name)
	issuer := strings.TrimRight(strings.TrimSpace(in.Issuer), "/")
	clientID := strings.TrimSpace(in.ClientID)
	if name == "" {
		return repository.OIDCProvider{}, errors.New("provider name is required")
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return repository.OIDCProvider{}, errors.New("issuer must be an HTTPS URL")
	}
	if in.Enabled && clientID == "" {
		return repository.OIDCProvider{}, errors.New("client_id is required before enabling the provider")
	}
	domains, _ := json.Marshal(in.AllowedEmailDomains)
	scopes := defaultString(in.Scopes, "openid email profile")
	if !slices.Contains(strings.Fields(scopes), "openid") {
		return repository.OIDCProvider{}, errors.New("OIDC scopes must include openid")
	}
	now := repository.NowMS()
	return repository.OIDCProvider{ID: id, Name: name, Issuer: issuer, ClientID: clientID, Scopes: scopes, Enabled: in.Enabled, AutoCreateUser: in.AutoCreateUser, AutoLinkVerifiedEmail: in.AutoLinkVerifiedEmail, AllowedEmailDomains: string(domains), CreatedAt: now, UpdatedAt: now}, nil
}
func (s *Server) listOIDC(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListOIDCProviders(r.Context(), false)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, len(items))
	for i, p := range items {
		out[i] = presentOIDC(p)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
func (s *Server) createOIDC(w http.ResponseWriter, r *http.Request) {
	var in oidcInput
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := makeOIDC(in, security.NewID())
	if err != nil {
		writeError(w, r, 422, "OIDC_INVALID", err.Error())
		return
	}
	if in.ClientSecret != "" {
		v, err := s.box.Encrypt([]byte(in.ClientSecret), "oidc-provider:"+p.ID+":v1")
		if err != nil {
			writeRepoError(w, r, err)
			return
		}
		p.ClientSecretCiphertext = sql.NullString{String: v, Valid: true}
	}
	if err := s.store.CreateOIDCProvider(r.Context(), p); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "oidc.created", "", p.ID, nil)
	writeJSON(w, 201, presentOIDC(p))
}
func (s *Server) getOIDC(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.OIDCProviderByID(r.Context(), chi.URLParam(r, "providerID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, 200, presentOIDC(p))
}
func (s *Server) updateOIDC(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerID")
	var in oidcInput
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := makeOIDC(in, id)
	if err != nil {
		writeError(w, r, 422, "OIDC_INVALID", err.Error())
		return
	}
	var secret *string
	if in.ClientSecret != "" {
		v, err := s.box.Encrypt([]byte(in.ClientSecret), "oidc-provider:"+id+":v1")
		if err != nil {
			writeRepoError(w, r, err)
			return
		}
		secret = &v
	}
	if err := s.store.UpdateOIDCProvider(r.Context(), p, secret); err != nil {
		writeRepoError(w, r, err)
		return
	}
	p, err = s.store.OIDCProviderByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "oidc.updated", "", id, nil)
	writeJSON(w, 200, presentOIDC(p))
}
func (s *Server) deleteOIDC(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerID")
	p, err := s.store.OIDCProviderByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if p.Enabled {
		writeError(w, r, http.StatusConflict, "OIDC_PROVIDER_ENABLED", "Disable the OIDC provider before deleting it.")
		return
	}
	if err := s.store.DeleteOIDCProvider(r.Context(), id); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "oidc.deleted", "", id, nil)
	w.WriteHeader(204)
}
func (s *Server) testOIDC(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.OIDCProviderByID(r.Context(), chi.URLParam(r, "providerID"))
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		err = s.oidc.Test(ctx, p)
	}
	if err != nil {
		writeError(w, r, 502, "OIDC_TEST_FAILED", "OIDC discovery failed.")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
