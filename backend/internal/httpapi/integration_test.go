package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/altasci/network-storage/backend/internal/authorization"
	"github.com/altasci/network-storage/backend/internal/oidc"
	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/sharing"
	"github.com/altasci/network-storage/backend/internal/storage/factory"
	"github.com/altasci/network-storage/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestAuthenticatedLocalFileFlow(t *testing.T) {
	ctx := context.Background()
	store := testutil.Store(t)
	key := bytes.Repeat([]byte{9}, 32)
	box, err := security.NewSecretBox(key)
	require.NoError(t, err)
	hash, err := security.HashPassword("correct horse battery staple")
	require.NoError(t, err)
	now := repository.NowMS()
	admin := repository.User{ID: security.NewID(), Email: "admin@example.com", PasswordHash: sql.NullString{String: hash, Valid: true}, Role: "admin", WriteEnabled: true, Status: "active", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateUser(ctx, admin))
	root := t.TempDir()
	backend := repository.StorageBackend{ID: security.NewID(), Name: "local", Type: "local", Enabled: true, ConfigJSON: `{"root_path":` + mustJSON(root) + `}`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateStorageBackend(ctx, backend))
	webOrigin := "https://web.example.test"
	require.NoError(t, store.SetSetting(ctx, "cors.allowed_origins", []string{webOrigin}, admin.ID))
	require.NoError(t, store.SetSetting(ctx, "security.trusted_proxy_cidrs", []string{}, admin.ID))
	f := factory.New(store, box)
	handler := New(Options{Store: store, Authorization: authorization.New(store), Factory: f, Box: box, Sharing: sharing.New(key), OIDC: oidc.New(store, box, "https://api.example.test"), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), WebURL: webOrigin, APIURL: "https://api.example.test"})
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := server.Client()
	client.Jar = jar

	login := doJSON(t, client, server.URL+"/api/v1/auth/login", http.MethodPost, webOrigin, "", map[string]any{"email": admin.Email, "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, login.StatusCode)
	var browserSession *http.Cookie
	for _, cookie := range login.Cookies() {
		if cookie.Name == sessionCookie {
			browserSession = cookie
			break
		}
	}
	require.NotNil(t, browserSession)
	require.Zero(t, browserSession.MaxAge, "remember=false must issue a browser-session cookie")
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	require.NoError(t, json.NewDecoder(login.Body).Decode(&auth))
	login.Body.Close()
	require.NotEmpty(t, auth.CSRF)

	created := doJSON(t, client, server.URL+"/api/v1/projects", http.MethodPost, webOrigin, auth.CSRF, map[string]any{"name": "Project", "storage_backend_id": backend.ID})
	require.Equal(t, http.StatusCreated, created.StatusCode)
	var project struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(created.Body).Decode(&project))
	created.Body.Close()

	uploadResponse := doJSON(t, client, server.URL+"/api/v1/projects/"+project.ID+"/uploads", http.MethodPost, webOrigin, auth.CSRF, map[string]any{"filename": "hello.txt", "size": 5, "mime_type": "text/plain", "overwrite": false})
	require.Equal(t, http.StatusCreated, uploadResponse.StatusCode)
	var upload struct {
		ID string `json:"upload_id"`
	}
	require.NoError(t, json.NewDecoder(uploadResponse.Body).Decode(&upload))
	uploadResponse.Body.Close()

	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/uploads/"+upload.ID+"/content", strings.NewReader("hello"))
	require.NoError(t, err)
	req.Header.Set("Origin", webOrigin)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-CSRF-Token", auth.CSRF)
	content, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, content.StatusCode)
	content.Body.Close()

	completed := doJSON(t, client, server.URL+"/api/v1/uploads/"+upload.ID+"/complete", http.MethodPost, webOrigin, auth.CSRF, map[string]any{"parts": []any{}})
	require.Equal(t, http.StatusOK, completed.StatusCode)
	var node struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(completed.Body).Decode(&node))
	completed.Body.Close()

	rangeRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/nodes/"+node.ID+"/content", nil)
	require.NoError(t, err)
	rangeRequest.Header.Set("Range", "bytes=1-3")
	rangeResponse, err := client.Do(rangeRequest)
	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, rangeResponse.StatusCode)
	raw, err := io.ReadAll(rangeResponse.Body)
	require.NoError(t, err)
	rangeResponse.Body.Close()
	require.Equal(t, "ell", string(raw))

	createdShareResponse := doJSON(t, client, server.URL+"/api/v1/nodes/"+node.ID+"/shares", http.MethodPost, webOrigin, auth.CSRF, map[string]any{"require_code": true})
	require.Equal(t, http.StatusCreated, createdShareResponse.StatusCode)
	var createdShare struct {
		URL        string `json:"url"`
		Code       string `json:"code"`
		CodeLength int    `json:"code_length"`
	}
	require.NoError(t, json.NewDecoder(createdShareResponse.Body).Decode(&createdShare))
	createdShareResponse.Body.Close()
	require.Regexp(t, `^\d{4}$`, createdShare.Code)
	require.Equal(t, 4, createdShare.CodeLength)
	shareToken := strings.TrimPrefix(createdShare.URL, webOrigin+"/s/")
	verifiedShare := doJSON(t, client, server.URL+"/api/v1/public/shares/"+shareToken+"/verify", http.MethodPost, webOrigin, "", map[string]any{"code": createdShare.Code})
	require.Equal(t, http.StatusOK, verifiedShare.StatusCode)
	verifiedShare.Body.Close()

	inUseStorageDelete := doJSON(t, client, server.URL+"/api/v1/admin/storage-backends/"+backend.ID, http.MethodDelete, webOrigin, auth.CSRF, map[string]any{})
	require.Equal(t, http.StatusConflict, inUseStorageDelete.StatusCode)
	var inUseStorageError apiError
	require.NoError(t, json.NewDecoder(inUseStorageDelete.Body).Decode(&inUseStorageError))
	inUseStorageDelete.Body.Close()
	require.Equal(t, "STORAGE_BACKEND_IN_USE", inUseStorageError.Error.Code)

	unusedStorageResponse := doJSON(t, client, server.URL+"/api/v1/admin/storage-backends", http.MethodPost, webOrigin, auth.CSRF, map[string]any{"name": "unused", "type": "local", "enabled": true, "config": map[string]any{"root_path": t.TempDir()}, "secret": nil})
	require.Equal(t, http.StatusCreated, unusedStorageResponse.StatusCode)
	var unusedStorage struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(unusedStorageResponse.Body).Decode(&unusedStorage))
	unusedStorageResponse.Body.Close()
	updatedStorageResponse := doJSON(t, client, server.URL+"/api/v1/admin/storage-backends/"+unusedStorage.ID, http.MethodPatch, webOrigin, auth.CSRF, map[string]any{"name": "unused renamed", "enabled": false, "config": map[string]any{"root_path": t.TempDir()}})
	require.Equal(t, http.StatusOK, updatedStorageResponse.StatusCode)
	updatedStorageResponse.Body.Close()
	unusedStorageDelete := doJSON(t, client, server.URL+"/api/v1/admin/storage-backends/"+unusedStorage.ID, http.MethodDelete, webOrigin, auth.CSRF, map[string]any{})
	require.Equal(t, http.StatusNoContent, unusedStorageDelete.StatusCode)
	unusedStorageDelete.Body.Close()

	oidcPayload := map[string]any{
		"name": "Integration IdP", "issuer": "https://idp.example.test", "client_id": "client", "client_secret": "secret",
		"scopes": "openid email", "enabled": true, "auto_create_user": false, "auto_link_verified_email": true, "allowed_email_domains": []string{},
	}
	createdOIDC := doJSON(t, client, server.URL+"/api/v1/admin/oidc-providers", http.MethodPost, webOrigin, auth.CSRF, oidcPayload)
	require.Equal(t, http.StatusCreated, createdOIDC.StatusCode)
	var oidcProvider struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(createdOIDC.Body).Decode(&oidcProvider))
	createdOIDC.Body.Close()
	storedProvider, err := store.OIDCProviderByID(ctx, oidcProvider.ID)
	require.NoError(t, err)
	_, err = store.ResolveOIDCIdentity(ctx, storedProvider, "integration-subject", admin.Email, true, security.NewID(), security.NewID())
	require.NoError(t, err)

	enabledDelete := doJSON(t, client, server.URL+"/api/v1/admin/oidc-providers/"+oidcProvider.ID, http.MethodDelete, webOrigin, auth.CSRF, map[string]any{})
	require.Equal(t, http.StatusConflict, enabledDelete.StatusCode)
	var enabledDeleteError apiError
	require.NoError(t, json.NewDecoder(enabledDelete.Body).Decode(&enabledDeleteError))
	enabledDelete.Body.Close()
	require.Equal(t, "OIDC_PROVIDER_ENABLED", enabledDeleteError.Error.Code)
	oidcPayload["enabled"] = false
	disabledOIDC := doJSON(t, client, server.URL+"/api/v1/admin/oidc-providers/"+oidcProvider.ID, http.MethodPatch, webOrigin, auth.CSRF, oidcPayload)
	require.Equal(t, http.StatusOK, disabledOIDC.StatusCode)
	disabledOIDC.Body.Close()
	deletedOIDC := doJSON(t, client, server.URL+"/api/v1/admin/oidc-providers/"+oidcProvider.ID, http.MethodDelete, webOrigin, auth.CSRF, map[string]any{})
	require.Equal(t, http.StatusNoContent, deletedOIDC.StatusCode)
	deletedOIDC.Body.Close()

	logout := doJSON(t, client, server.URL+"/api/v1/auth/logout", http.MethodPost, webOrigin, auth.CSRF, map[string]any{})
	require.Equal(t, http.StatusNoContent, logout.StatusCode)
	logout.Body.Close()
	var deletedSession *http.Cookie
	for _, cookie := range logout.Cookies() {
		if cookie.Name == sessionCookie {
			deletedSession = cookie
			break
		}
	}
	require.NotNil(t, deletedSession)
	require.Equal(t, -1, deletedSession.MaxAge, "logout must delete the session cookie")

	persistentClient := *server.Client()
	persistentClient.Jar = nil
	persistentLogin := doJSON(t, &persistentClient, server.URL+"/api/v1/auth/login", http.MethodPost, webOrigin, "", map[string]any{"email": admin.Email, "password": "correct horse battery staple", "remember": true})
	require.Equal(t, http.StatusOK, persistentLogin.StatusCode)
	defer persistentLogin.Body.Close()
	var persistentSession *http.Cookie
	for _, cookie := range persistentLogin.Cookies() {
		if cookie.Name == sessionCookie {
			persistentSession = cookie
			break
		}
	}
	require.NotNil(t, persistentSession)
	require.Equal(t, 7*24*60*60, persistentSession.MaxAge)
}

func doJSON(t *testing.T, client *http.Client, target, method, origin, csrf string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(method, target, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := client.Do(req)
	require.NoError(t, err)
	return response
}

func mustJSON(value string) string { raw, _ := json.Marshal(value); return string(raw) }
