package httpapi

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/go-chi/chi/v5"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	items, err := s.store.ListProjects(r.Context(), u)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, 0, len(items))
	key, apiRequest := apiKeyFrom(r)
	for _, p := range items {
		if apiRequest && !key.AllowsProject(p.ID) {
			continue
		}
		out = append(out, presentProject(p))
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
func (s *Server) listAvailableStorage(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListStorageBackends(r.Context())
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, 0, len(items))
	for _, b := range items {
		if b.Enabled {
			out = append(out, map[string]any{"id": b.ID, "name": b.Name, "type": b.Type, "enabled": true})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	if err := s.authorization.CanCreateProject(u); err != nil {
		writeRepoError(w, r, err)
		return
	}
	var in struct {
		Name             string `json:"name"`
		Description      string `json:"description"`
		StorageBackendID string `json:"storage_backend_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || in.StorageBackendID == "" {
		writeError(w, r, 422, "PROJECT_INVALID", "Name and storage_backend_id are required.")
		return
	}
	now := repository.NowMS()
	p := repository.Project{ID: security.NewID(), Name: strings.TrimSpace(in.Name), Description: in.Description, StorageBackendID: in.StorageBackendID, CreatedBy: u.ID, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateProject(r.Context(), p, u.Role == "admin"); err != nil {
		writeRepoError(w, r, err)
		return
	}
	if u.Role == "admin" {
		p.Permission = "admin"
	} else {
		p.Permission = "write"
	}
	_ = s.audit(r, "project.created", p.ID, p.ID, nil)
	writeJSON(w, 201, presentProject(p))
}
func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "projectID")
	if err := s.authorization.CanReadProject(r.Context(), u, id); err != nil {
		writeRepoError(w, r, err)
		return
	}
	p, err := s.store.ProjectByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if u.Role == "admin" {
		p.Permission = "admin"
	} else {
		p.Permission, _ = s.store.ProjectPermission(r.Context(), id, u.ID)
	}
	writeJSON(w, 200, presentProject(p))
}
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	if err := s.authorization.CanManageProject(u); err != nil {
		writeRepoError(w, r, err)
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, r, 422, "PROJECT_INVALID", "Name is required.")
		return
	}
	p, err := s.store.UpdateProject(r.Context(), chi.URLParam(r, "projectID"), strings.TrimSpace(in.Name), in.Description)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	p.Permission = "admin"
	writeJSON(w, 200, presentProject(p))
}
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	if err := s.authorization.CanManageProject(u); err != nil {
		writeRepoError(w, r, err)
		return
	}
	id := chi.URLParam(r, "projectID")
	if err := s.store.MarkProjectDeleting(r.Context(), id); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "project.deleted", id, id, nil)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "deleting"})
}
func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	if err := s.authorization.CanManageProject(u); err != nil {
		writeRepoError(w, r, err)
		return
	}
	items, err := s.store.ListMembers(r.Context(), chi.URLParam(r, "projectID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, len(items))
	for i, m := range items {
		out[i] = map[string]any{"project_id": m.ProjectID, "user_id": m.UserID, "permission": m.Permission, "granted_by": m.GrantedBy, "created_at": rfc(m.CreatedAt), "updated_at": rfc(m.UpdatedAt)}
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
func (s *Server) putMember(w http.ResponseWriter, r *http.Request) {
	actor, _ := userFrom(r)
	if err := s.authorization.CanManageProject(actor); err != nil {
		writeRepoError(w, r, err)
		return
	}
	var in struct {
		Permission string `json:"permission"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Permission != "read" && in.Permission != "write" {
		writeError(w, r, 422, "PERMISSION_INVALID", "Permission must be read or write.")
		return
	}
	now := repository.NowMS()
	m := repository.ProjectMember{ProjectID: chi.URLParam(r, "projectID"), UserID: chi.URLParam(r, "userID"), Permission: in.Permission, GrantedBy: actor.ID, CreatedAt: now, UpdatedAt: now}
	if err := s.store.PutMember(r.Context(), m); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "project.member_updated", m.ProjectID, m.UserID, map[string]string{"permission": m.Permission})
	writeJSON(w, 200, map[string]any{"project_id": m.ProjectID, "user_id": m.UserID, "permission": m.Permission})
}
func (s *Server) deleteMember(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	if err := s.authorization.CanManageProject(u); err != nil {
		writeRepoError(w, r, err)
		return
	}
	projectID, userID := chi.URLParam(r, "projectID"), chi.URLParam(r, "userID")
	if err := s.store.DeleteMember(r.Context(), projectID, userID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "project.member_deleted", projectID, userID, nil)
	w.WriteHeader(204)
}

var _ = sql.ErrNoRows
