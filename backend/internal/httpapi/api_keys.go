package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/go-chi/chi/v5"
)

const apiKeyContextKey contextKey = "api_key"

var apiScopes = []string{"projects:read", "projects:create", "projects:write", "projects:delete", "files:read", "files:write", "files:delete"}

func apiKeyFrom(r *http.Request) (repository.APIKey, bool) {
	k, ok := r.Context().Value(apiKeyContextKey).(repository.APIKey)
	return k, ok
}
func (s *Server) authenticateAPIKey(w http.ResponseWriter, r *http.Request, next http.Handler) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(r.Header.Values("Authorization")) != 1 || len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || !strings.HasPrefix(fields[1], "altasci_") || len(fields[1]) != 51 {
		writeError(w, r, 401, "UNAUTHENTICATED", "A valid Bearer API key is required.")
		return
	}
	now := repository.NowMS()
	k, err := s.store.APIKeyByTokenHash(r.Context(), security.TokenHash(fields[1]), now)
	if err != nil {
		writeError(w, r, 401, "UNAUTHENTICATED", "The API key is invalid or expired.")
		return
	}
	u, err := s.store.UserByID(r.Context(), k.UserID)
	if err != nil || u.Status != "active" {
		writeError(w, r, 401, "UNAUTHENTICATED", "Authentication is required.")
		return
	}
	if !k.LastUsedAt.Valid || now-k.LastUsedAt.Int64 > time.Minute.Milliseconds() {
		_ = s.store.TouchAPIKey(r.Context(), k.ID, now)
	}
	ctx := context.WithValue(context.WithValue(r.Context(), userKey, u), apiKeyContextKey, k)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// API access is opt-in on each route. Everything outside these routes requires a session.
func (s *Server) apiAccess(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k, ok := apiKeyFrom(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if !k.HasScope(scope) {
				writeError(w, r, 403, "API_SCOPE_FORBIDDEN", "The API key does not permit this operation.")
				return
			}
			if scope == "projects:create" {
				u, _ := userFrom(r)
				if err := s.authorization.CanCreateProject(u); err != nil {
					writeRepoError(w, r, err)
					return
				}
			}
			projectID := chi.URLParam(r, "projectID")
			if id := chi.URLParam(r, "nodeID"); id != "" {
				n, err := s.store.NodeByID(r.Context(), id)
				if err != nil {
					writeRepoError(w, r, err)
					return
				}
				projectID = n.ProjectID
			}
			if id := chi.URLParam(r, "uploadID"); id != "" {
				up, err := s.store.UploadByID(r.Context(), id)
				if err != nil {
					writeRepoError(w, r, err)
					return
				}
				projectID = up.ProjectID
			}
			if (projectID != "" && !k.AllowsProject(projectID)) || (scope == "projects:create" && !k.AllProjects) {
				writeError(w, r, 403, "API_PROJECT_FORBIDDEN", "The project is outside this API key's access range.")
				return
			}
			// Re-check live membership even on handlers that only enforce administrator/ownership rules.
			if projectID != "" {
				u, _ := userFrom(r)
				var err error
				if scope == "files:write" || scope == "files:delete" {
					err = s.authorization.CanWriteProject(r.Context(), u, projectID)
				} else {
					err = s.authorization.CanReadProject(r.Context(), u, projectID)
				}
				if err != nil {
					writeRepoError(w, r, err)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
func (s *Server) sessionOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := sessionFrom(r); !ok {
			writeError(w, r, 403, "SESSION_REQUIRED", "Sign in to manage account settings, sharing, and permissions.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func presentAPIKey(k repository.APIKey) map[string]any {
	return map[string]any{"id": k.ID, "name": k.Name, "token_prefix": k.TokenPrefix, "scopes": k.Scopes, "all_projects": k.AllProjects, "project_ids": k.ProjectIDs, "created_at": rfc(k.CreatedAt), "updated_at": rfc(k.UpdatedAt), "expires_at": rfc(k.ExpiresAt), "last_used_at": nullRFC(k.LastUsedAt), "revoked_at": nullRFC(k.RevokedAt)}
}

type apiKeyInput struct {
	Name        string   `json:"name"`
	Scopes      []string `json:"scopes"`
	AllProjects bool     `json:"all_projects"`
	ProjectIDs  []string `json:"project_ids"`
	ExpiresAt   string   `json:"expires_at"`
}

func (s *Server) validateAPIKey(w http.ResponseWriter, r *http.Request, in apiKeyInput, k *repository.APIKey) bool {
	in.Name = strings.TrimSpace(in.Name)
	expiry, err := time.Parse(time.RFC3339, in.ExpiresAt)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || err != nil || !expiry.After(time.Now()) || expiry.After(time.Now().Add(366*24*time.Hour)) || len(in.Scopes) == 0 || len(in.Scopes) > len(apiScopes) || len(in.ProjectIDs) > 100 || (in.AllProjects && len(in.ProjectIDs) > 0) || (!in.AllProjects && len(in.ProjectIDs) == 0) {
		writeError(w, r, 422, "API_KEY_INVALID", "Provide a name, scopes, project range, and an expiry within 366 days.")
		return false
	}
	u, _ := userFrom(r)
	seen := map[string]bool{}
	for _, scope := range in.Scopes {
		if !slices.Contains(apiScopes, scope) || seen[scope] || (scope == "projects:create" && !in.AllProjects) {
			writeError(w, r, 422, "API_SCOPE_INVALID", "Scopes must be unique and supported; project creation requires all projects.")
			return false
		}
		seen[scope] = true
		if (scope == "projects:write" || scope == "projects:delete") && s.authorization.CanManageProject(u) != nil {
			writeRepoError(w, r, repository.ErrForbidden)
			return false
		}
		if (scope == "projects:create" || scope == "files:write" || scope == "files:delete") && s.authorization.CanCreateProject(u) != nil {
			writeRepoError(w, r, repository.ErrForbidden)
			return false
		}
	}
	seen = map[string]bool{}
	for _, id := range in.ProjectIDs {
		if seen[id] {
			writeError(w, r, 422, "API_KEY_INVALID", "Project IDs must be unique.")
			return false
		}
		seen[id] = true
		if err := s.authorization.CanReadProject(r.Context(), u, id); err != nil {
			writeRepoError(w, r, err)
			return false
		}
		if (slices.Contains(in.Scopes, "files:write") || slices.Contains(in.Scopes, "files:delete")) && s.authorization.CanWriteProject(r.Context(), u, id) != nil {
			writeRepoError(w, r, repository.ErrForbidden)
			return false
		}
	}
	if in.ProjectIDs == nil {
		in.ProjectIDs = []string{}
	}
	k.Name = in.Name
	k.Scopes = in.Scopes
	k.AllProjects = in.AllProjects
	k.ProjectIDs = in.ProjectIDs
	k.ExpiresAt = expiry.UnixMilli()
	k.UpdatedAt = repository.NowMS()
	return true
}
func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	keys, err := s.store.ListAPIKeys(r.Context(), u.ID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	items := make([]any, 0, len(keys))
	for _, k := range keys {
		items = append(items, presentAPIKey(k))
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var in apiKeyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	u, _ := userFrom(r)
	k := repository.APIKey{ID: security.NewID(), UserID: u.ID, CreatedAt: repository.NowMS()}
	if !s.validateAPIKey(w, r, in, &k) {
		return
	}
	secret, err := security.RandomToken(32)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	secret = "altasci_" + secret
	k.TokenHash = security.TokenHash(secret)
	k.TokenPrefix = secret[:15]
	if err := s.store.CreateAPIKey(r.Context(), k); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "api_key.created", "", k.ID, presentAPIKey(k))
	out := presentAPIKey(k)
	out["token"] = secret
	writeJSON(w, 201, out)
}

// PUT replaces the editable policy atomically; secrets cannot be read or changed.
func (s *Server) updateAPIKey(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	k, err := s.store.APIKeyByID(r.Context(), chi.URLParam(r, "keyID"), u.ID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if k.RevokedAt.Valid {
		writeRepoError(w, r, repository.ErrConflict)
		return
	}
	var in apiKeyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.validateAPIKey(w, r, in, &k) {
		return
	}
	if err := s.store.UpdateAPIKey(r.Context(), k); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "api_key.updated", "", k.ID, presentAPIKey(k))
	writeJSON(w, 200, presentAPIKey(k))
}
func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "keyID")
	if err := s.store.RevokeAPIKey(r.Context(), id, u.ID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "api_key.revoked", "", id, nil)
	w.WriteHeader(204)
}
