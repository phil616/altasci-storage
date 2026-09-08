package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/altasci/network-storage/backend/internal/authorization"
	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/storage/factory"
	"github.com/altasci/network-storage/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

type apiFixture struct {
	t       *testing.T
	store   *repository.Store
	handler http.Handler
	user    repository.User
	project repository.Project
	other   repository.Project
	backend repository.StorageBackend
	session string
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	ctx := context.Background()
	store := testutil.Store(t)
	now := repository.NowMS()
	u := repository.User{ID: security.NewID(), Email: "api@example.test", Role: "user", WriteEnabled: true, Status: "active", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateUser(ctx, u))
	b := repository.StorageBackend{ID: security.NewID(), Name: "local", Type: "local", Enabled: true, ConfigJSON: `{"root_path":` + mustJSON(t.TempDir()) + `}`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateStorageBackend(ctx, b))
	p := repository.Project{ID: security.NewID(), Name: "allowed", StorageBackendID: b.ID, CreatedBy: u.ID, Status: "active", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateProject(ctx, p, false))
	other := p
	other.ID = security.NewID()
	other.Name = "other"
	require.NoError(t, store.CreateProject(ctx, other, false))
	require.NoError(t, store.SetSetting(ctx, "cors.allowed_origins", []string{"https://web.test"}, u.ID))
	token, _ := security.RandomToken(32)
	require.NoError(t, store.CreateSession(ctx, repository.Session{ID: security.NewID(), UserID: u.ID, TokenHash: security.TokenHash(token), CSRFTokenHash: security.TokenHash("csrf"), CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now + 3600000, AbsoluteExpiresAt: now + 3600000}))
	box, err := security.NewSecretBox(bytes.Repeat([]byte{1}, 32))
	require.NoError(t, err)
	h := New(Options{Store: store, Authorization: authorization.New(store), Factory: factory.New(store, box), Box: box, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), APIURL: "https://api.test"})
	return &apiFixture{t, store, h, u, p, other, b, token}
}
func (f *apiFixture) request(method, path, token string, body any, browser bool) *httptest.ResponseRecorder {
	f.t.Helper()
	var raw []byte
	if value, ok := body.(string); ok {
		raw = []byte(value)
	} else {
		var err error
		raw, err = json.Marshal(body)
		require.NoError(f.t, err)
	}
	r := httptest.NewRequest(method, "https://api.test/api/v1"+path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if browser {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: f.session})
		r.Header.Set("Origin", "https://web.test")
		r.Header.Set("X-CSRF-Token", "csrf")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func policy(projectIDs []string, scopes ...string) map[string]any {
	return map[string]any{"name": "automation", "scopes": scopes, "all_projects": len(projectIDs) == 0, "project_ids": projectIDs, "expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}
}
func (f *apiFixture) create(policy map[string]any) (string, string) {
	f.t.Helper()
	w := f.request("POST", "/api-keys", "", policy, true)
	require.Equal(f.t, 201, w.Code, w.Body.String())
	var result map[string]any
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &result))
	return result["id"].(string), result["token"].(string)
}
func TestAPIKeyLifecycleAndOwnership(t *testing.T) {
	f := newAPIFixture(t)
	ctx := context.Background()
	id, token := f.create(policy([]string{f.project.ID}, "projects:read", "files:read"))
	require.Len(t, token, 51)
	stored, err := f.store.APIKeyByID(ctx, id, f.user.ID)
	require.NoError(t, err)
	require.Equal(t, security.TokenHash(token), stored.TokenHash)
	require.NotEqual(t, token, stored.TokenHash)
	list := f.request("GET", "/api-keys", "", nil, true)
	require.Equal(t, 200, list.Code)
	require.NotContains(t, list.Body.String(), token)
	require.NotContains(t, list.Body.String(), stored.TokenHash)
	require.Equal(t, 200, f.request("GET", "/projects", token, nil, false).Code)
	stored, err = f.store.APIKeyByID(ctx, id, f.user.ID)
	require.NoError(t, err)
	require.True(t, stored.LastUsedAt.Valid)
	updated := f.request("PUT", "/api-keys/"+id, "", policy([]string{f.other.ID}, "files:read"), true)
	require.Equal(t, 200, updated.Code, updated.Body.String())
	require.NotContains(t, updated.Body.String(), token)
	require.Equal(t, 403, f.request("GET", "/projects", token, nil, false).Code)
	require.Equal(t, 403, f.request("GET", "/projects/"+f.project.ID+"/nodes", token, nil, false).Code)
	require.Equal(t, 200, f.request("GET", "/projects/"+f.other.ID+"/nodes", token, nil, false).Code)
	// A different account cannot discover, edit or revoke the key.
	other := f.user
	other.ID = security.NewID()
	other.Email = "other@example.test"
	require.NoError(t, f.store.CreateUser(ctx, other))
	_, err = f.store.DB.ExecContext(ctx, `UPDATE sessions SET user_id=? WHERE token_hash=?`, other.ID, security.TokenHash(f.session))
	require.NoError(t, err)
	require.JSONEq(t, `{"items":[]}`, f.request("GET", "/api-keys", "", nil, true).Body.String())
	require.Equal(t, 404, f.request("PUT", "/api-keys/"+id, "", policy(nil, "files:read"), true).Code)
	require.Equal(t, 404, f.request("DELETE", "/api-keys/"+id, "", nil, true).Code)
	_, err = f.store.DB.ExecContext(ctx, `UPDATE sessions SET user_id=? WHERE token_hash=?`, f.user.ID, security.TokenHash(f.session))
	require.NoError(t, err)
	require.Equal(t, 204, f.request("DELETE", "/api-keys/"+id, "", nil, true).Code)
	require.Equal(t, 204, f.request("DELETE", "/api-keys/"+id, "", nil, true).Code)
	require.Equal(t, 401, f.request("GET", "/projects", token, nil, false).Code)
	require.Equal(t, 409, f.request("PUT", "/api-keys/"+id, "", policy(nil, "files:read"), true).Code)
	var metadata string
	require.NoError(t, f.store.DB.QueryRowContext(ctx, `SELECT metadata_json FROM audit_logs WHERE action='api_key.created' AND target_id=?`, id).Scan(&metadata))
	require.NotContains(t, metadata, token)
}
func TestAPIKeyScopeAndProjectIsolation(t *testing.T) {
	f := newAPIFixture(t)
	_, token := f.create(policy([]string{f.project.ID}, "projects:read", "files:read"))
	list := f.request("GET", "/projects", token, nil, false)
	require.Equal(t, 200, list.Code)
	require.Contains(t, list.Body.String(), f.project.ID)
	require.NotContains(t, list.Body.String(), f.other.ID)
	require.Equal(t, 403, f.request("GET", "/projects/"+f.other.ID, token, nil, false).Code)
	require.Equal(t, 403, f.request("POST", "/projects/"+f.project.ID+"/directories", token, map[string]string{"name": "denied"}, false).Code)
	// Invalid explicit credentials never fall back to the valid browser cookie.
	require.Equal(t, 401, f.request("GET", "/projects", "bad", nil, true).Code)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api-keys"}, {"POST", "/api-keys"}, {"GET", "/auth/me"}, {"GET", "/auth/csrf"}, {"POST", "/auth/logout"}, {"POST", "/auth/change-password"}, {"GET", "/admin/users"}, {"GET", "/shares"}, {"GET", "/projects/" + f.project.ID + "/members"}} {
		t.Run(endpoint.method+endpoint.path, func(t *testing.T) {
			require.Equal(t, 403, f.request(endpoint.method, endpoint.path, token, map[string]string{}, true).Code)
		})
	}
	// Scoped keys cannot follow another project's node/upload ID.
	_, writeToken := f.create(policy([]string{f.project.ID}, "files:write", "files:delete", "files:read"))
	directory := f.request("POST", "/projects/"+f.other.ID+"/directories", "", map[string]string{"name": "other"}, true)
	require.Equal(t, 201, directory.Code)
	var node struct{ ID string }
	require.NoError(t, json.Unmarshal(directory.Body.Bytes(), &node))
	for _, endpoint := range []struct{ method, suffix string }{{"GET", ""}, {"PATCH", ""}, {"DELETE", ""}, {"POST", "/download"}, {"GET", "/content"}, {"HEAD", "/content"}} {
		require.Equal(t, 403, f.request(endpoint.method, "/nodes/"+node.ID+endpoint.suffix, writeToken, map[string]string{}, false).Code)
	}
	up := f.request("POST", "/projects/"+f.other.ID+"/uploads", "", map[string]any{"filename": "other.txt", "size": 1}, true)
	require.Equal(t, 201, up.Code, up.Body.String())
	var upload struct {
		ID string `json:"upload_id"`
	}
	require.NoError(t, json.Unmarshal(up.Body.Bytes(), &upload))
	for _, endpoint := range []struct{ method, suffix string }{{"PUT", "/content"}, {"POST", "/parts/presign"}, {"POST", "/complete"}, {"DELETE", ""}} {
		require.Equal(t, 403, f.request(endpoint.method, "/uploads/"+upload.ID+endpoint.suffix, writeToken, map[string]string{}, false).Code)
	}
}
func TestAPIKeyLiveUserPermissions(t *testing.T) {
	f := newAPIFixture(t)
	ctx := context.Background()
	id, token := f.create(policy(nil, "projects:read", "files:read", "files:write", "files:delete", "projects:create"))
	require.Equal(t, 403, f.request("PATCH", "/projects/"+f.project.ID, token, map[string]string{"name": "no"}, false).Code)
	// Changing live member permission cuts off writing with an already-issued key.
	_, err := f.store.DB.ExecContext(ctx, `UPDATE project_members SET permission='read' WHERE project_id=?`, f.project.ID)
	require.NoError(t, err)
	require.Equal(t, 403, f.request("POST", "/projects/"+f.project.ID+"/directories", token, map[string]string{"name": "no"}, false).Code)
	require.Equal(t, 200, f.request("GET", "/projects/"+f.project.ID, token, nil, false).Code)
	require.NoError(t, f.store.DeleteMember(ctx, f.project.ID, f.user.ID))
	require.Equal(t, 403, f.request("GET", "/projects/"+f.project.ID, token, nil, false).Code)
	list := f.request("GET", "/projects", token, nil, false)
	require.NotContains(t, list.Body.String(), f.project.ID)
	disabledWrite := false
	_, err = f.store.UpdateUser(ctx, f.user.ID, nil, &disabledWrite, nil)
	require.NoError(t, err)
	require.Equal(t, 403, f.request("POST", "/projects", token, map[string]string{"name": "no", "storage_backend_id": f.backend.ID}, false).Code)
	require.Equal(t, 403, f.request("GET", "/storage-backends", token, nil, false).Code)
	require.Equal(t, 403, f.request("POST", "/projects/"+f.other.ID+"/directories", token, map[string]string{"name": "no"}, false).Code)
	status := "disabled"
	_, err = f.store.UpdateUser(ctx, f.user.ID, nil, nil, &status)
	require.NoError(t, err)
	require.Equal(t, 401, f.request("GET", "/projects", token, nil, false).Code)
	status = "active"
	_, err = f.store.UpdateUser(ctx, f.user.ID, nil, nil, &status)
	require.NoError(t, err)
	_, err = f.store.DB.ExecContext(ctx, `UPDATE api_keys SET expires_at=? WHERE id=?`, repository.NowMS()-1, id)
	require.NoError(t, err)
	require.Equal(t, 401, f.request("GET", "/projects", token, nil, false).Code)
}
func TestAPIKeyLocalFileFlow(t *testing.T) {
	f := newAPIFixture(t)
	_, token := f.create(policy(nil, "projects:create", "projects:read", "files:read", "files:write", "files:delete"))
	created := f.request("POST", "/projects", token, map[string]string{"name": "API project", "storage_backend_id": f.backend.ID}, false)
	require.Equal(t, 201, created.Code, created.Body.String())
	var project struct{ ID string }
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &project))
	up := f.request("POST", "/projects/"+project.ID+"/uploads", token, map[string]any{"filename": "hello.txt", "size": 5, "mime_type": "text/plain"}, false)
	require.Equal(t, 201, up.Code, up.Body.String())
	var upload struct {
		ID string `json:"upload_id"`
	}
	require.NoError(t, json.Unmarshal(up.Body.Bytes(), &upload))
	require.Equal(t, 204, f.request("PUT", "/uploads/"+upload.ID+"/content", token, "hello", false).Code)
	complete := f.request("POST", "/uploads/"+upload.ID+"/complete", token, map[string]any{"parts": []any{}}, false)
	require.Equal(t, 200, complete.Code, complete.Body.String())
	var node struct{ ID string }
	require.NoError(t, json.Unmarshal(complete.Body.Bytes(), &node))
	download := f.request("GET", "/nodes/"+node.ID+"/content", token, nil, false)
	require.Equal(t, 200, download.Code)
	require.Equal(t, "hello", download.Body.String())
	require.Equal(t, 200, f.request("POST", "/nodes/"+node.ID+"/download", token, map[string]any{}, false).Code)
	require.Equal(t, 200, f.request("PATCH", "/nodes/"+node.ID, token, map[string]string{"name": "renamed.txt"}, false).Code)
	require.Equal(t, 204, f.request("DELETE", "/nodes/"+node.ID, token, nil, false).Code)
	require.Equal(t, 404, f.request("GET", "/nodes/"+node.ID, token, nil, false).Code)
}
func TestAPIKeyValidationAndCSRF(t *testing.T) {
	f := newAPIFixture(t)
	for _, modify := range []func(map[string]any){
		func(p map[string]any) { p["scopes"] = []string{"admin:*"} },
		func(p map[string]any) { p["scopes"] = []string{"files:read", "files:read"} },
		func(p map[string]any) { p["scopes"] = []string{} },
		func(p map[string]any) { p["all_projects"] = false; p["project_ids"] = []string{} },
		func(p map[string]any) { p["all_projects"] = true; p["project_ids"] = []string{f.project.ID} },
		func(p map[string]any) { p["name"] = strings.Repeat("长", 101) },
		func(p map[string]any) { p["expires_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339) },
		func(p map[string]any) { p["expires_at"] = time.Now().Add(367 * 24 * time.Hour).Format(time.RFC3339) },
		func(p map[string]any) {
			p["all_projects"] = false
			p["project_ids"] = []string{f.project.ID}
			p["scopes"] = []string{"projects:create"}
		},
	} {
		p := policy(nil, "files:read")
		modify(p)
		w := f.request("POST", "/api-keys", "", p, true)
		require.Equal(t, 422, w.Code, w.Body.String())
	}
	require.Equal(t, 403, f.request("POST", "/api-keys", "", policy(nil, "projects:write"), true).Code)
	require.Equal(t, 403, f.request("POST", "/api-keys", "", policy([]string{security.NewID()}, "files:read"), true).Code)
	req := httptest.NewRequest("POST", "https://api.test/api/v1/api-keys", strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: f.session})
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	require.Equal(t, 403, rec.Code)
}

func TestAPIKeyAdminDemotionAndAbortPermission(t *testing.T) {
	f := newAPIFixture(t)
	ctx := context.Background()
	_, err := f.store.DB.ExecContext(ctx, `UPDATE users SET role='admin' WHERE id=?`, f.user.ID)
	require.NoError(t, err)
	_, token := f.create(policy([]string{f.project.ID}, "projects:read", "projects:write", "projects:delete", "files:write"))
	require.Equal(t, 200, f.request("PATCH", "/projects/"+f.project.ID, token, map[string]string{"name": "changed"}, false).Code)
	require.Equal(t, 403, f.request("DELETE", "/projects/"+f.other.ID, token, nil, false).Code)
	require.Equal(t, 403, f.request("GET", "/admin/users", token, nil, false).Code)
	_, err = f.store.DB.ExecContext(ctx, `UPDATE users SET role='user' WHERE id=?`, f.user.ID)
	require.NoError(t, err)
	require.Equal(t, 403, f.request("PATCH", "/projects/"+f.project.ID, token, map[string]string{"name": "no"}, false).Code)
	require.Equal(t, 403, f.request("DELETE", "/projects/"+f.project.ID, token, nil, false).Code)
	up := f.request("POST", "/projects/"+f.project.ID+"/uploads", token, map[string]any{"filename": "pending", "size": 1}, false)
	require.Equal(t, 201, up.Code)
	var upload struct {
		ID string `json:"upload_id"`
	}
	require.NoError(t, json.Unmarshal(up.Body.Bytes(), &upload))
	require.NoError(t, f.store.DeleteMember(ctx, f.project.ID, f.user.ID))
	require.Equal(t, 403, f.request("DELETE", "/uploads/"+upload.ID, token, nil, false).Code)
	require.Equal(t, 403, f.request("DELETE", "/uploads/"+upload.ID, "", nil, true).Code)
}
