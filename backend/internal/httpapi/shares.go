package httpapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/sharing"
	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/go-chi/chi/v5"
)

func presentShare(sh repository.Share) map[string]any {
	return map[string]any{"id": sh.ID, "project_id": sh.ProjectID, "target_node_id": sh.TargetNodeID, "target_node_ids": sh.TargetNodeIDs, "created_by": sh.CreatedBy, "require_code": sh.RequireCode, "code_length": sh.CodeLength, "expires_at": nullRFC(sh.ExpiresAt), "disabled_at": nullRFC(sh.DisabledAt), "created_at": rfc(sh.CreatedAt)}
}
func (s *Server) presentOwnedShare(sh repository.Share) map[string]any {
	out := presentShare(sh)
	if sh.CredentialsCiphertext.Valid {
		plain, err := s.box.Decrypt(sh.CredentialsCiphertext.String, "share:"+sh.ID)
		var credentials struct {
			Token string
			Code  string
		}
		if err == nil && json.Unmarshal(plain, &credentials) == nil {
			out["url"] = strings.TrimRight(s.webURL, "/") + "/s/" + security.FormatShareToken(credentials.Token)
			if sh.RequireCode {
				out["code"] = credentials.Code
			}
		}
	}
	return out
}
func (s *Server) listShares(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	items, err := s.store.ListShares(r.Context(), u)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, len(items))
	for i, sh := range items {
		out[i] = s.presentOwnedShare(sh)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	var in struct {
		NodeIDs     []string `json:"node_ids"`
		RequireCode *bool    `json:"require_code"`
		ExpiresAt   *string  `json:"expires_at"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if id := chi.URLParam(r, "nodeID"); id != "" {
		in.NodeIDs = []string{id}
	}
	if len(in.NodeIDs) == 0 || len(in.NodeIDs) > 100 {
		writeError(w, r, 422, "SHARE_TARGETS_INVALID", "Select between 1 and 100 nodes.")
		return
	}
	var node repository.Node
	ids := make([]string, 0, len(in.NodeIDs))
	seen := map[string]bool{}
	for _, id := range in.NodeIDs {
		if seen[id] {
			continue
		}
		current, err := s.store.NodeByID(r.Context(), id)
		if err != nil {
			writeRepoError(w, r, err)
			return
		}
		if err := s.authorization.CanShareNode(r.Context(), u, current); err != nil {
			writeRepoError(w, r, err)
			return
		}
		if node.ID != "" && current.ProjectID != node.ProjectID {
			writeError(w, r, 422, "SHARE_TARGETS_INVALID", "All nodes must belong to the same project.")
			return
		}
		node = current
		seen[id] = true
		ids = append(ids, id)
	}
	requireCode := true
	if in.RequireCode != nil {
		requireCode = *in.RequireCode
	}
	defaultExpiration := s.durationSetting(r.Context(), "share.default_expiration", 7*24*time.Hour, time.Minute, 3650*24*time.Hour)
	expires := sql.NullInt64{Int64: time.Now().Add(defaultExpiration).UnixMilli(), Valid: true}
	if in.ExpiresAt != nil {
		if *in.ExpiresAt == "" {
			expires = sql.NullInt64{}
		} else {
			parsed, e := time.Parse(time.RFC3339, *in.ExpiresAt)
			if e != nil || parsed.Before(time.Now()) {
				writeError(w, r, 422, "EXPIRATION_INVALID", "Expiration must be a future RFC3339 time.")
				return
			}
			expires = sql.NullInt64{Int64: parsed.UTC().UnixMilli(), Valid: true}
		}
	}
	token, err := security.GenerateShareToken()
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	var code string
	var codeHash sql.NullString
	if requireCode {
		code, err = sharing.GenerateCode()
		if err == nil {
			codeHash, err = sharing.HashCode(code)
		}
		if err != nil {
			writeRepoError(w, r, err)
			return
		}
	}
	now := repository.NowMS()
	sh := repository.Share{ID: security.NewID(), ProjectID: node.ProjectID, TargetNodeID: ids[0], TargetNodeIDs: ids, CreatedBy: u.ID, PublicTokenHash: security.TokenHash(token), CodeHash: codeHash, RequireCode: requireCode, CodeLength: sharing.CodeLength, ExpiresAt: expires, CreatedAt: now}
	credentials, err := json.Marshal(map[string]string{"Token": token, "Code": code})
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	encrypted, err := s.box.Encrypt(credentials, "share:"+sh.ID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	sh.CredentialsCiphertext = sql.NullString{String: encrypted, Valid: true}
	if err := s.store.CreateShare(r.Context(), sh); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "share.created", node.ProjectID, sh.ID, map[string]any{"target_node_ids": ids, "require_code": requireCode})
	response := s.presentOwnedShare(sh)
	response["url"] = strings.TrimRight(s.webURL, "/") + "/s/" + security.FormatShareToken(token)
	if requireCode {
		response["code"] = code
	}
	writeJSON(w, 201, response)
}
func (s *Server) getShare(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	sh, err := s.store.ShareByID(r.Context(), chi.URLParam(r, "shareID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if u.Role != "admin" && sh.CreatedBy != u.ID {
		writeRepoError(w, r, repository.ErrForbidden)
		return
	}
	writeJSON(w, 200, s.presentOwnedShare(sh))
}
func (s *Server) updateShare(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "shareID")
	sh, err := s.store.ShareByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if u.Role != "admin" && sh.CreatedBy != u.ID {
		writeRepoError(w, r, repository.ErrForbidden)
		return
	}
	var in struct {
		ExpiresAt *string `json:"expires_at"`
		Disabled  *bool   `json:"disabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	expires := sh.ExpiresAt
	if in.ExpiresAt != nil {
		if *in.ExpiresAt == "" {
			expires = sql.NullInt64{}
		} else {
			parsed, e := time.Parse(time.RFC3339, *in.ExpiresAt)
			if e != nil || parsed.Before(time.Now()) {
				writeError(w, r, 422, "EXPIRATION_INVALID", "Expiration must be a future RFC3339 time.")
				return
			}
			expires = sql.NullInt64{Int64: parsed.UTC().UnixMilli(), Valid: true}
		}
	}
	disabled := sh.DisabledAt.Valid
	if in.Disabled != nil {
		disabled = disabled || *in.Disabled
	}
	if err := s.store.UpdateShare(r.Context(), id, expires, disabled); err != nil {
		writeRepoError(w, r, err)
		return
	}
	sh, _ = s.store.ShareByID(r.Context(), id)
	writeJSON(w, 200, s.presentOwnedShare(sh))
}
func (s *Server) deleteShare(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "shareID")
	sh, err := s.store.ShareByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if u.Role != "admin" && sh.CreatedBy != u.ID {
		writeRepoError(w, r, repository.ErrForbidden)
		return
	}
	if err := s.store.DisableShare(r.Context(), id); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "share.revoked", sh.ProjectID, id, nil)
	w.WriteHeader(204)
}

func (s *Server) loadPublicShare(w http.ResponseWriter, r *http.Request) (repository.Share, bool) {
	token := security.NormalizeShareToken(chi.URLParam(r, "token"))
	sh, err := s.store.ShareByToken(r.Context(), token)
	if err != nil {
		writeError(w, r, 404, "SHARE_NOT_FOUND", "The share is unavailable.")
		return repository.Share{}, false
	}
	if sh.DisabledAt.Valid {
		writeError(w, r, 404, "SHARE_NOT_FOUND", "The share is unavailable.")
		return repository.Share{}, false
	}
	if sh.ExpiresAt.Valid && sh.ExpiresAt.Int64 <= repository.NowMS() {
		writeError(w, r, 410, "SHARE_EXPIRED", "The share has expired.")
		return repository.Share{}, false
	}
	return sh, true
}
func (s *Server) publicShare(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.loadPublicShare(w, r)
	if !ok {
		return
	}
	node, err := s.store.NodeByID(r.Context(), sh.TargetNodeID)
	if len(sh.TargetNodeIDs) > 1 {
		node = repository.Node{Name: fmt.Sprintf("%d 个分享项目", len(sh.TargetNodeIDs)), NodeType: "directory"}
		err = nil
	}
	if err != nil {
		writeError(w, r, 404, "SHARE_NOT_FOUND", "The share is unavailable.")
		return
	}
	writeJSON(w, 200, map[string]any{"share": presentShare(sh), "target": presentNode(node), "grant_required": sh.RequireCode})
}
func (s *Server) verifyShare(w http.ResponseWriter, r *http.Request) {
	token := security.NormalizeShareToken(chi.URLParam(r, "token"))
	tokenHash := security.TokenHash(token)
	ipKey := clientIP(r) + "|" + tokenHash
	limit := s.shareRateLimit(r.Context())
	if retry, _ := s.store.BanRemaining(r.Context(), "share_ip", ipKey); retry > 0 {
		w.Header().Set("Retry-After", seconds(retry))
		writeError(w, r, 429, "SHARE_RATE_LIMITED", "Share verification is temporarily unavailable.")
		return
	}
	if retry, _ := s.store.BanRemaining(r.Context(), "share", tokenHash); retry > 0 {
		w.Header().Set("Retry-After", seconds(retry))
		writeError(w, r, 429, "SHARE_RATE_LIMITED", "Share verification is temporarily unavailable.")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sh, err := s.store.ShareByToken(r.Context(), token)
	valid := err == nil && repository.ActiveShare(sh, repository.NowMS()) == nil && sh.RequireCode && sh.CodeHash.Valid
	if valid {
		valid = sharing.ValidCodeFormat(in.Code, sh.CodeLength)
	}
	if valid {
		valid, _ = sharing.VerifyCode(sh.CodeHash.String, in.Code)
	}
	if !valid {
		retryIP, _ := s.store.RecordFailure(r.Context(), "share_ip", ipKey, limit.IPAttempts, time.Duration(limit.IPWindowSeconds)*time.Second, time.Duration(limit.IPBanSeconds)*time.Second, 3, time.Duration(limit.EscalatedBanSeconds)*time.Second)
		retryShare, _ := s.store.RecordFailure(r.Context(), "share", tokenHash, limit.ShareAttempts, time.Duration(limit.ShareWindowSeconds)*time.Second, time.Duration(limit.ShareBanSeconds)*time.Second, 0, 0)
		retry := retryIP
		if retryShare > retry {
			retry = retryShare
		}
		if retry > 0 {
			w.Header().Set("Retry-After", seconds(retry))
			writeError(w, r, 429, "SHARE_RATE_LIMITED", "Share verification is temporarily unavailable.")
			return
		}
		writeError(w, r, 401, "SHARE_CODE_INVALID", "The share code is invalid.")
		return
	}
	_ = s.store.ClearFailures(r.Context(), "share_ip", ipKey)
	grant, err := s.sharing.Grant(sh.ID, 30*time.Minute)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"grant": grant, "expires_at": time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)})
}
func (s *Server) authorizePublic(w http.ResponseWriter, r *http.Request, sh repository.Share) bool {
	if !sh.RequireCode {
		return true
	}
	grant := sharing.Bearer(r.Header.Get("Authorization"))
	if s.sharing.ValidateGrant(grant, sh.ID) != nil {
		writeError(w, r, 401, "SHARE_GRANT_INVALID", "A valid share grant is required.")
		return false
	}
	return true
}
func (s *Server) publicNodes(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.loadPublicShare(w, r)
	if !ok || !s.authorizePublic(w, r, sh) {
		return
	}
	parent := r.URL.Query().Get("parent_id")
	if parent == "" && len(sh.TargetNodeIDs) > 1 {
		out := make([]any, 0, len(sh.TargetNodeIDs))
		for _, id := range sh.TargetNodeIDs {
			n, err := s.store.NodeByID(r.Context(), id)
			if err == repository.ErrNotFound {
				continue
			}
			if err != nil {
				writeRepoError(w, r, err)
				return
			}
			out = append(out, presentNode(n))
		}
		writeJSON(w, 200, map[string]any{"items": out})
		return
	}
	if parent == "" {
		parent = sh.TargetNodeID
	}
	allowed, err := s.store.IsShareDescendant(r.Context(), sh, parent)
	if err != nil || !allowed {
		writeError(w, r, 403, "SHARE_NODE_FORBIDDEN", "The requested node is outside this share.")
		return
	}
	root, err := s.store.NodeByID(r.Context(), parent)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if root.NodeType == "file" {
		writeJSON(w, 200, map[string]any{"items": []any{presentNode(root)}})
		return
	}
	items, err := s.store.ListNodes(r.Context(), sh.ProjectID, parent)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	out := make([]any, len(items))
	for i, n := range items {
		out[i] = presentNode(n)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
func (s *Server) publicDownload(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.loadPublicShare(w, r)
	if !ok || !s.authorizePublic(w, r, sh) {
		return
	}
	nodeID := chi.URLParam(r, "nodeID")
	allowed, err := s.store.IsShareDescendant(r.Context(), sh, nodeID)
	if err != nil || !allowed {
		writeError(w, r, 403, "SHARE_NODE_FORBIDDEN", "The requested node is outside this share.")
		return
	}
	n, blob, err := s.store.BlobForNode(r.Context(), nodeID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	adapter, backend, err := s.factory.ByID(r.Context(), blob.StorageBackendID)
	if err != nil || !backend.Enabled {
		writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The storage backend is unavailable.")
		return
	}
	if adapter.Type() == "local" {
		writeJSON(w, 200, map[string]any{"delivery": "local", "url": strings.TrimRight(s.apiURL, "/") + "/api/v1/public/shares/" + chi.URLParam(r, "token") + "/nodes/" + n.ID + "/content"})
		return
	}
	configuredTTL := s.durationSetting(r.Context(), "share.download_presign_ttl", 120*time.Second, 30*time.Second, 10*time.Minute)
	ttl := sharing.MinTTL(configuredTTL, sh.ExpiresAt)
	if ttl <= 0 {
		writeError(w, r, 410, "SHARE_EXPIRED", "The share has expired.")
		return
	}
	target, err := adapter.PresignDownload(r.Context(), storageapi.DownloadRequest{Key: blob.ObjectKey, Filename: n.Name, Expires: ttl})
	if err != nil {
		writeError(w, r, 502, "STORAGE_PROVIDER_ERROR", "The download URL could not be generated.")
		return
	}
	writeJSON(w, 200, map[string]any{"delivery": "external", "url": target.URL, "expires_at": target.ExpiresAt.UTC().Format(time.RFC3339)})
}
func (s *Server) publicLocalContent(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.loadPublicShare(w, r)
	if !ok || !s.authorizePublic(w, r, sh) {
		return
	}
	nodeID := chi.URLParam(r, "nodeID")
	allowed, err := s.store.IsShareDescendant(r.Context(), sh, nodeID)
	if err != nil || !allowed {
		writeError(w, r, 403, "SHARE_NODE_FORBIDDEN", "The requested node is outside this share.")
		return
	}
	n, blob, err := s.store.BlobForNode(r.Context(), nodeID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	adapter, backend, err := s.factory.ByID(r.Context(), blob.StorageBackendID)
	if err != nil || !backend.Enabled || adapter.Type() != "local" {
		writeError(w, r, 404, "LOCAL_CONTENT_NOT_FOUND", "Local content was not found.")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": n.Name}))
	w.Header().Set("Content-Type", n.MIMEType)
	w.Header().Set("Accept-Ranges", "bytes")
	rng, err := parseRange(r.Header.Get("Range"), blob.Size)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", blob.Size))
		w.WriteHeader(416)
		return
	}
	reader, _, err := adapter.Open(r.Context(), blob.ObjectKey, rng)
	if err != nil {
		writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The local object could not be opened.")
		return
	}
	defer reader.Close()
	length := blob.Size
	if rng != nil {
		length = rng.End - rng.Start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.Start, rng.End, blob.Size))
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if rng != nil {
		w.WriteHeader(206)
	}
	_, _ = io.Copy(w, reader)
}
