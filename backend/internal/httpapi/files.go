package httpapi

import (
	"database/sql"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/go-chi/chi/v5"
)

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	projectID := chi.URLParam(r, "projectID")
	if err := s.authorization.CanReadProject(r.Context(), u, projectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	parent := r.URL.Query().Get("parent_id")
	if parent != "" {
		n, err := s.store.NodeByID(r.Context(), parent)
		if err != nil || n.ProjectID != projectID || n.NodeType != "directory" {
			writeError(w, r, 404, "DIRECTORY_NOT_FOUND", "The directory was not found.")
			return
		}
	}
	items, err := s.store.ListNodes(r.Context(), projectID, parent)
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
func (s *Server) createDirectory(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	projectID := chi.URLParam(r, "projectID")
	if err := s.authorization.CanWriteProject(r.Context(), u, projectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	var in struct {
		ParentID string `json:"parent_id"`
		Name     string `json:"name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	now := repository.NowMS()
	n := repository.Node{ID: security.NewID(), ProjectID: projectID, ParentID: sql.NullString{String: in.ParentID, Valid: in.ParentID != ""}, NodeType: "directory", Name: in.Name, CreatedBy: u.ID, CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateDirectory(r.Context(), n); err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, 201, presentNode(n))
}
func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	n, err := s.store.NodeByID(r.Context(), chi.URLParam(r, "nodeID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if err := s.authorization.CanReadProject(r.Context(), u, n.ProjectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, 200, presentNode(n))
}
func (s *Server) updateNode(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "nodeID")
	n, err := s.store.NodeByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if err := s.authorization.CanWriteProject(r.Context(), u, n.ProjectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	var in struct {
		Name     *string `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name == nil && in.ParentID == nil {
		writeError(w, r, 422, "NODE_INVALID", "At least one field must be changed.")
		return
	}
	n, err = s.store.UpdateNode(r.Context(), id, in.Name, in.ParentID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, 200, presentNode(n))
}
func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "nodeID")
	n, err := s.store.NodeByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if err := s.authorization.CanWriteProject(r.Context(), u, n.ProjectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	if err := s.store.DeleteNode(r.Context(), id); err != nil {
		writeRepoError(w, r, err)
		return
	}
	_ = s.audit(r, "file.deleted", n.ProjectID, id, map[string]string{"node_type": n.NodeType})
	w.WriteHeader(204)
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	projectID := chi.URLParam(r, "projectID")
	if err := s.authorization.CanWriteProject(r.Context(), u, projectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	var in struct {
		ParentID  string `json:"parent_id"`
		Filename  string `json:"filename"`
		Size      int64  `json:"size"`
		MIMEType  string `json:"mime_type"`
		Overwrite bool   `json:"overwrite"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Size < 0 || in.Size > 5<<40 {
		writeError(w, r, 422, "UPLOAD_INVALID", "File size must be between 0 and 5 TiB.")
		return
	}
	if in.MIMEType == "" {
		in.MIMEType = "application/octet-stream"
	}
	p, err := s.store.ProjectByID(r.Context(), projectID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	adapter, backend, err := s.factory.ByID(r.Context(), p.StorageBackendID)
	if err != nil || !backend.Enabled {
		writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The project storage backend is unavailable.")
		return
	}
	uploadType := "single"
	multipart := in.Size >= 100<<20
	if adapter.Type() == "local" {
		uploadType = "local"
		multipart = false
	} else if multipart {
		uploadType = "multipart"
	}
	now := repository.NowMS()
	blobID, uploadID := security.NewID(), security.NewID()
	blob := repository.Blob{ID: blobID, ProjectID: projectID, StorageBackendID: p.StorageBackendID, ObjectKey: "v1/objects/" + projectID + "/" + blobID, Size: in.Size, MIMEType: in.MIMEType, Status: "pending", CreatedAt: now}
	up := repository.Upload{ID: uploadID, ProjectID: projectID, BlobID: blobID, UserID: u.ID, UploadType: uploadType, ExpectedSize: in.Size, MIMEType: in.MIMEType, OriginalName: in.Filename, ParentID: sql.NullString{String: in.ParentID, Valid: in.ParentID != ""}, Overwrite: in.Overwrite, Status: "created", CreatedAt: now, ExpiresAt: now + 24*time.Hour.Milliseconds()}
	if err := s.store.CreateUpload(r.Context(), up, blob); err != nil {
		writeRepoError(w, r, err)
		return
	}
	uploadTTL := s.durationSetting(r.Context(), "storage.upload_presign_ttl", 15*time.Minute, time.Minute, time.Hour)
	target, err := adapter.CreateUpload(r.Context(), storageapi.CreateUploadRequest{Key: blob.ObjectKey, Size: blob.Size, ContentType: blob.MIMEType, Multipart: multipart, Expires: uploadTTL})
	if err != nil {
		_ = s.store.FailUpload(r.Context(), uploadID)
		writeError(w, r, 502, "STORAGE_PROVIDER_ERROR", "The upload could not be created.")
		return
	}
	if target.ProviderUploadID != "" {
		up.ProviderUploadID = sql.NullString{String: target.ProviderUploadID, Valid: true}
		_ = s.store.SetProviderUploadID(r.Context(), uploadID, target.ProviderUploadID)
	}
	response := map[string]any{"upload_id": uploadID, "upload_type": uploadType, "delivery": target.Delivery, "expires_at": rfc(up.ExpiresAt)}
	if uploadType == "local" {
		response["url"] = strings.TrimRight(s.apiURL, "/") + "/api/v1/uploads/" + uploadID + "/content"
		response["method"] = "PUT"
	} else if uploadType == "single" {
		response["url"] = target.URL
		response["method"] = target.Method
		response["headers"] = presentUploadHeaders(target.Headers)
		response["presign_expires_at"] = target.ExpiresAt.UTC().Format(time.RFC3339)
	} else {
		partSize := multipartPartSize(in.Size)
		response["part_size"] = partSize
		response["part_count"] = (in.Size + partSize - 1) / partSize
	}
	writeJSON(w, 201, response)
}
func multipartPartSize(size int64) int64 {
	part := int64(16 << 20)
	needed := (size + 9999) / 10000
	if needed > part {
		const mib = int64(1 << 20)
		part = ((needed + mib - 1) / mib) * mib
	}
	return part
}

func (s *Server) presignParts(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	up, err := s.store.UploadByID(r.Context(), chi.URLParam(r, "uploadID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if up.UserID != u.ID && u.Role != "admin" {
		writeRepoError(w, r, repository.ErrForbidden)
		return
	}
	if err := s.authorization.CanWriteProject(r.Context(), u, up.ProjectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	if up.UploadType != "multipart" || !up.ProviderUploadID.Valid || up.ExpiresAt <= repository.NowMS() {
		writeError(w, r, 409, "UPLOAD_STATE_INVALID", "The upload is not an active multipart upload.")
		return
	}
	var in struct {
		PartNumbers []int32 `json:"part_numbers"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.PartNumbers) < 1 || len(in.PartNumbers) > 100 {
		writeError(w, r, 422, "PARTS_INVALID", "One to 100 part numbers are required.")
		return
	}
	blob, err := s.store.BlobByID(r.Context(), up.BlobID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	adapter, backend, err := s.factory.ByID(r.Context(), blob.StorageBackendID)
	if err != nil || !backend.Enabled {
		writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The storage backend is unavailable.")
		return
	}
	seen := map[int32]bool{}
	parts := make([]any, 0, len(in.PartNumbers))
	uploadTTL := s.durationSetting(r.Context(), "storage.upload_presign_ttl", 15*time.Minute, time.Minute, time.Hour)
	for _, number := range in.PartNumbers {
		if number < 1 || number > 10000 || seen[number] {
			writeError(w, r, 422, "PARTS_INVALID", "Part numbers must be unique and between 1 and 10000.")
			return
		}
		seen[number] = true
		target, err := adapter.PresignUploadPart(r.Context(), storageapi.PresignPartRequest{Key: blob.ObjectKey, ProviderUploadID: up.ProviderUploadID.String, PartNumber: number, Expires: uploadTTL})
		if err != nil {
			writeError(w, r, 502, "STORAGE_PROVIDER_ERROR", "A part URL could not be generated.")
			return
		}
		parts = append(parts, map[string]any{"part_number": number, "url": target.URL, "method": target.Method, "headers": presentUploadHeaders(target.Headers), "expires_at": target.ExpiresAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, 200, map[string]any{"parts": parts})
}

// presentUploadHeaders keeps the HTTP contract stable when a provider does
// not require any signed headers. A nil Go map would otherwise be serialized
// as JSON null, which is not a valid RequestInit.headers value in browsers.
func presentUploadHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return map[string]string{}
	}
	return headers
}

func (s *Server) localUpload(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	up, err := s.store.UploadByID(r.Context(), chi.URLParam(r, "uploadID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if up.UserID != u.ID && u.Role != "admin" {
		writeRepoError(w, r, repository.ErrForbidden)
		return
	}
	if err := s.authorization.CanWriteProject(r.Context(), u, up.ProjectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	if up.UploadType != "local" || up.ExpiresAt <= repository.NowMS() || up.Status != "created" {
		writeError(w, r, 409, "UPLOAD_STATE_INVALID", "The upload cannot accept local content.")
		return
	}
	blob, err := s.store.BlobByID(r.Context(), up.BlobID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	adapter, backend, err := s.factory.ByID(r.Context(), blob.StorageBackendID)
	if err != nil || !backend.Enabled || adapter.Type() != "local" {
		writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The local storage backend is unavailable.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, up.ExpectedSize+1)
	if err := adapter.Put(r.Context(), blob.ObjectKey, r.Body, up.ExpectedSize, up.MIMEType); err != nil {
		writeError(w, r, 422, "UPLOAD_SIZE_MISMATCH", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "uploadID")
	up, err := s.store.UploadByID(r.Context(), id)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if up.UserID != u.ID && u.Role != "admin" {
		writeRepoError(w, r, repository.ErrForbidden)
		return
	}
	if err := s.authorization.CanWriteProject(r.Context(), u, up.ProjectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	var in struct {
		Parts []storageapi.CompletedPart `json:"parts"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ownerID := up.UserID
	up, err = s.store.MarkUploadCompleting(r.Context(), id, u.ID, repository.NowMS())
	if err != nil && u.Role == "admin" {
		up, err = s.store.MarkUploadCompleting(r.Context(), id, ownerID, repository.NowMS())
	}
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	blob, err := s.store.BlobByID(r.Context(), up.BlobID)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	adapter, backend, err := s.factory.ByID(r.Context(), blob.StorageBackendID)
	if err != nil || !backend.Enabled {
		writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The storage backend is unavailable.")
		return
	}
	if up.UploadType == "multipart" {
		if len(in.Parts) == 0 || !validCompletedParts(in.Parts) {
			_ = s.store.FailUpload(r.Context(), id)
			writeError(w, r, 422, "PARTS_INVALID", "Completed parts are invalid.")
			return
		}
		if err := adapter.CompleteMultipartUpload(r.Context(), blob.ObjectKey, up.ProviderUploadID.String, in.Parts); err != nil {
			_ = s.store.FailUpload(r.Context(), id)
			writeError(w, r, 502, "STORAGE_PROVIDER_ERROR", "The multipart upload could not be completed.")
			return
		}
	}
	info, err := adapter.Head(r.Context(), blob.ObjectKey)
	if err != nil || info.Size != up.ExpectedSize {
		_ = s.store.FailUpload(r.Context(), id)
		writeError(w, r, 422, "UPLOAD_SIZE_MISMATCH", "The stored object does not match the expected size.")
		return
	}
	node, err := s.store.FinalizeUpload(r.Context(), up, info.Size, info.ETag, info.ChecksumAlgorithm, info.ChecksumValue)
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	writeJSON(w, 200, presentNode(node))
}
func validCompletedParts(parts []storageapi.CompletedPart) bool {
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	last := int32(0)
	for _, p := range parts {
		if p.PartNumber <= last || p.PartNumber < 1 || p.PartNumber > 10000 || strings.TrimSpace(p.ETag) == "" {
			return false
		}
		last = p.PartNumber
	}
	return true
}
func (s *Server) abortUpload(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	up, err := s.store.UploadByID(r.Context(), chi.URLParam(r, "uploadID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if up.UserID != u.ID && u.Role != "admin" {
		writeRepoError(w, r, repository.ErrForbidden)
		return
	}
	blob, err := s.store.BlobByID(r.Context(), up.BlobID)
	if err == nil && up.UploadType == "multipart" && up.ProviderUploadID.Valid {
		if adapter, _, e := s.factory.ByID(r.Context(), blob.StorageBackendID); e == nil {
			_ = adapter.AbortMultipartUpload(r.Context(), blob.ObjectKey, up.ProviderUploadID.String)
		}
	}
	if err := s.store.AbortUpload(r.Context(), up.ID, up.UserID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	n, blob, err := s.store.BlobForNode(r.Context(), chi.URLParam(r, "nodeID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if err := s.authorization.CanReadProject(r.Context(), u, n.ProjectID); err != nil {
		writeRepoError(w, r, err)
		return
	}
	adapter, backend, err := s.factory.ByID(r.Context(), blob.StorageBackendID)
	if err != nil || !backend.Enabled {
		writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The storage backend is unavailable.")
		return
	}
	if adapter.Type() == "local" {
		writeJSON(w, 200, map[string]any{"delivery": "local", "url": strings.TrimRight(s.apiURL, "/") + "/api/v1/nodes/" + n.ID + "/content"})
		return
	}
	downloadTTL := s.durationSetting(r.Context(), "storage.download_presign_ttl", 5*time.Minute, time.Minute, time.Hour)
	target, err := adapter.PresignDownload(r.Context(), storageapi.DownloadRequest{Key: blob.ObjectKey, Filename: n.Name, Expires: downloadTTL})
	if err != nil {
		writeError(w, r, 502, "STORAGE_PROVIDER_ERROR", "The download URL could not be generated.")
		return
	}
	writeJSON(w, 200, map[string]any{"delivery": "external", "url": target.URL, "expires_at": target.ExpiresAt.UTC().Format(time.RFC3339)})
}
func (s *Server) localContent(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	n, blob, err := s.store.BlobForNode(r.Context(), chi.URLParam(r, "nodeID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if err := s.authorization.CanReadProject(r.Context(), u, n.ProjectID); err != nil {
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
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(blob.Size, 10))
		w.WriteHeader(200)
		return
	}
	rng, err := parseRange(r.Header.Get("Range"), blob.Size)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", blob.Size))
		writeError(w, r, http.StatusRequestedRangeNotSatisfiable, "RANGE_INVALID", "The requested byte range is invalid.")
		return
	}
	reader, _, err := adapter.Open(r.Context(), blob.ObjectKey, rng)
	if err != nil {
		writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The local object could not be opened.")
		return
	}
	defer reader.Close()
	if rng != nil {
		length := rng.End - rng.Start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.Start, rng.End, blob.Size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(blob.Size, 10))
	}
	_, _ = io.Copy(w, reader)
}
func parseRange(raw string, size int64) (*storageapi.Range, error) {
	if raw == "" {
		return nil, nil
	}
	if !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw, ",") {
		return nil, fmt.Errorf("invalid range")
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, "bytes="), "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range")
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return nil, fmt.Errorf("invalid range")
		}
		if suffix > size {
			suffix = size
		}
		return &storageapi.Range{Start: size - suffix, End: size - 1}, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return nil, fmt.Errorf("invalid range")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return nil, fmt.Errorf("invalid range")
		}
		if end >= size {
			end = size - 1
		}
	}
	return &storageapi.Range{Start: start, End: end}, nil
}
