package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/altasci/network-storage/backend/internal/repository"
)

type apiError struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	var body apiError
	body.Error.Code = code
	body.Error.Message = message
	body.Error.RequestID = requestID(r)
	writeJSON(w, status, body)
}
func writeRepoError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "The requested resource was not found.")
	case errors.Is(err, repository.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "You do not have permission to perform this operation.")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "The requested operation conflicts with current state.")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "An internal error occurred.")
	}
}
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "MALFORMED_JSON", "The JSON request body is invalid.")
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, http.StatusBadRequest, "MALFORMED_JSON", "The request body must contain one JSON value.")
		return false
	}
	return true
}
func rfc(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano) }
func nullRFC(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return rfc(v.Int64)
}
func presentUser(u repository.User) map[string]any {
	return map[string]any{"id": u.ID, "email": u.Email, "role": u.Role, "write_enabled": u.WriteEnabled, "status": u.Status, "created_at": rfc(u.CreatedAt), "updated_at": rfc(u.UpdatedAt), "last_login_at": nullRFC(u.LastLoginAt)}
}
func presentProject(p repository.Project) map[string]any {
	return map[string]any{"id": p.ID, "name": p.Name, "description": p.Description, "storage_backend_id": p.StorageBackendID, "created_by": p.CreatedBy, "status": p.Status, "permission": p.Permission, "created_at": rfc(p.CreatedAt), "updated_at": rfc(p.UpdatedAt)}
}
func presentNode(n repository.Node) map[string]any {
	var parent any
	if n.ParentID.Valid {
		parent = n.ParentID.String
	}
	return map[string]any{"id": n.ID, "project_id": n.ProjectID, "parent_id": parent, "node_type": n.NodeType, "name": n.Name, "size": n.Size, "mime_type": n.MIMEType, "created_by": n.CreatedBy, "created_at": rfc(n.CreatedAt), "updated_at": rfc(n.UpdatedAt)}
}
