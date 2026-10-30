package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type mcpKeyTransport struct {
	base  http.RoundTripper
	token string
}

func (t mcpKeyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}
func connectMCP(t *testing.T, server *httptest.Server, token, version string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "altasci-test", Version: "1.0.0"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpKeyTransport{server.Client().Transport, token}},
	}, &mcp.ClientSessionOptions{ProtocolVersion: version})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
func callMCP(ctx context.Context, cs *mcp.ClientSession, name string, args any) (map[string]any, error) {
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	if result.IsError {
		raw, _ := json.Marshal(result.Content)
		return nil, fmt.Errorf("tool error: %s", raw)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(raw, &out)
	return out, err
}
func mustCallMCP(t *testing.T, cs *mcp.ClientSession, name string, args any) map[string]any {
	t.Helper()
	out, err := callMCP(context.Background(), cs, name, args)
	require.NoError(t, err, "%s", name)
	return out
}
func requireMCPError(t *testing.T, cs *mcp.ClientSession, name string, args any, code string) {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	require.True(t, result.IsError, "%+v", result)
	raw, err := json.Marshal(result.Content)
	require.NoError(t, err)
	require.Contains(t, string(raw), code)
}

func TestMCPProtocolsAndToolBoundary(t *testing.T) {
	f := newAPIFixture(t)
	// Even an administrator key with every REST scope gets only this whitelist.
	_, err := f.store.DB.Exec(`UPDATE users SET role='admin' WHERE id=?`, f.user.ID)
	require.NoError(t, err)
	_, token := f.create(policy(nil, apiScopes...))
	server := httptest.NewServer(f.handler)
	defer server.Close()
	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			cs := connectMCP(t, server, token, version)
			require.Equal(t, version, cs.InitializeResult().ProtocolVersion)
			tools, err := cs.ListTools(context.Background(), nil)
			require.NoError(t, err)
			var names []string
			for _, tool := range tools.Tools {
				names = append(names, tool.Name)
				require.NotNil(t, tool.Annotations)
			}
			sort.Strings(names)
			require.Equal(t, []string{"abort_upload", "complete_upload", "create_directory", "create_upload", "delete_node", "get_download_url", "get_node", "get_project", "list_nodes", "list_projects", "presign_upload_parts", "read_file", "update_node", "upload_content"}, names)
			for _, name := range []string{"change_password", "update_account", "create_api_key", "create_user", "update_settings", "put_member", "update_project", "delete_project", "http_request"} {
				result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
				require.True(t, err != nil || result.IsError, "%s must not be callable", name)
			}
			mustCallMCP(t, cs, "get_project", map[string]any{"project_id": f.project.ID})
			// Additional properties are rejected by the SDK's inferred schema.
			result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_directory", Arguments: map[string]any{"project_id": f.project.ID, "name": "injected", "role": "admin"}})
			require.True(t, err != nil || result.IsError)
		})
	}
}

func TestMCPFileLifecycle(t *testing.T) {
	f := newAPIFixture(t)
	_, token := f.create(policy([]string{f.project.ID}, "projects:read", "files:read", "files:write", "files:delete"))
	server := httptest.NewServer(f.handler)
	defer server.Close()
	cs := connectMCP(t, server, token, "2026-07-28")
	projects := mustCallMCP(t, cs, "list_projects", map[string]any{})
	require.Len(t, projects["items"], 1)
	dir := mustCallMCP(t, cs, "create_directory", map[string]any{"project_id": f.project.ID, "name": "documents"})
	up := mustCallMCP(t, cs, "create_upload", map[string]any{"project_id": f.project.ID, "parent_id": dir["id"], "filename": "hello.txt", "size": 11, "mime_type": "text/plain"})
	requireMCPError(t, cs, "upload_content", map[string]any{"upload_id": up["upload_id"], "content": "short"}, "UPLOAD_SIZE_MISMATCH")
	mustCallMCP(t, cs, "upload_content", map[string]any{"upload_id": up["upload_id"], "content": "hello world"})
	node := mustCallMCP(t, cs, "complete_upload", map[string]any{"upload_id": up["upload_id"]})
	mustCallMCP(t, cs, "get_node", map[string]any{"node_id": node["id"]})
	listing := mustCallMCP(t, cs, "list_nodes", map[string]any{"project_id": f.project.ID, "parent_id": dir["id"]})
	require.Len(t, listing["items"], 1)
	read := mustCallMCP(t, cs, "read_file", map[string]any{"node_id": node["id"], "offset": 6, "limit": 5})
	require.Equal(t, "world", read["content"])
	require.Equal(t, true, read["eof"])
	read = mustCallMCP(t, cs, "read_file", map[string]any{"node_id": node["id"], "limit": 5, "encoding": "base64"})
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("hello")), read["content"])
	require.Equal(t, false, read["eof"])
	url := mustCallMCP(t, cs, "get_download_url", map[string]any{"node_id": node["id"]})
	require.Equal(t, "local", url["delivery"])
	require.NotContains(t, fmt.Sprint(url), token)
	updated := mustCallMCP(t, cs, "update_node", map[string]any{"node_id": node["id"], "name": "renamed.txt", "parent_id": ""})
	require.Nil(t, updated["parent_id"])
	require.Equal(t, "renamed.txt", updated["name"])
	requireMCPError(t, cs, "read_file", map[string]any{"node_id": node["id"], "offset": -1}, "READ_INVALID")
	requireMCPError(t, cs, "read_file", map[string]any{"node_id": node["id"], "limit": mcpInlineLimit + 1}, "READ_INVALID")
	requireMCPError(t, cs, "read_file", map[string]any{"node_id": node["id"], "offset": 12}, "READ_INVALID")
	// Overwrites still use files:write and the existing upload commit rules.
	up = mustCallMCP(t, cs, "create_upload", map[string]any{"project_id": f.project.ID, "filename": "renamed.txt", "size": 3, "overwrite": true})
	mustCallMCP(t, cs, "upload_content", map[string]any{"upload_id": up["upload_id"], "encoding": "base64", "content": "AP8B"})
	overwritten := mustCallMCP(t, cs, "complete_upload", map[string]any{"upload_id": up["upload_id"]})
	require.Equal(t, node["id"], overwritten["id"])
	requireMCPError(t, cs, "read_file", map[string]any{"node_id": node["id"]}, "ENCODING_INVALID")
	read = mustCallMCP(t, cs, "read_file", map[string]any{"node_id": node["id"], "encoding": "base64"})
	require.Equal(t, "AP8B", read["content"])
	// Exercise the exact inline limit, including base64 JSON expansion.
	up = mustCallMCP(t, cs, "create_upload", map[string]any{"project_id": f.project.ID, "filename": "limit.bin", "size": mcpInlineLimit})
	mustCallMCP(t, cs, "upload_content", map[string]any{"upload_id": up["upload_id"], "encoding": "base64", "content": base64.StdEncoding.EncodeToString(make([]byte, mcpInlineLimit))})
	large := mustCallMCP(t, cs, "complete_upload", map[string]any{"upload_id": up["upload_id"]})
	read = mustCallMCP(t, cs, "read_file", map[string]any{"node_id": large["id"], "limit": mcpInlineLimit, "encoding": "base64"})
	require.Equal(t, float64(mcpInlineLimit), read["bytes_read"])
	up = mustCallMCP(t, cs, "create_upload", map[string]any{"project_id": f.project.ID, "filename": "cancel.txt", "size": 0})
	mustCallMCP(t, cs, "abort_upload", map[string]any{"upload_id": up["upload_id"]})
	mustCallMCP(t, cs, "delete_node", map[string]any{"node_id": node["id"]})
	requireMCPError(t, cs, "get_node", map[string]any{"node_id": node["id"]}, "NOT_FOUND")
	mustCallMCP(t, cs, "delete_node", map[string]any{"node_id": dir["id"]})
	var metadata string
	require.NoError(t, f.store.DB.QueryRow(`SELECT metadata_json FROM audit_logs WHERE action='file.deleted' AND target_id=?`, node["id"]).Scan(&metadata))
	require.Contains(t, metadata, "api_key_id")
	require.NotContains(t, metadata, token)
}

func TestMCPAuthorizationRecheckedPerRequest(t *testing.T) {
	for _, change := range []string{"revoke", "expire", "disable", "remove_member", "remove_write", "reduce_scope", "change_project"} {
		t.Run(change, func(t *testing.T) {
			f := newAPIFixture(t)
			id, token := f.create(policy([]string{f.project.ID}, "files:read", "files:write"))
			server := httptest.NewServer(f.handler)
			defer server.Close()
			cs := connectMCP(t, server, token, "2026-07-28")
			mustCallMCP(t, cs, "list_nodes", map[string]any{"project_id": f.project.ID})
			var query string
			var args []any
			switch change {
			case "revoke":
				query, args = `UPDATE api_keys SET revoked_at=1 WHERE id=?`, []any{id}
			case "expire":
				query, args = `UPDATE api_keys SET expires_at=1 WHERE id=?`, []any{id}
			case "disable":
				query, args = `UPDATE users SET status='disabled' WHERE id=?`, []any{f.user.ID}
			case "remove_member":
				query, args = `DELETE FROM project_members WHERE project_id=? AND user_id=?`, []any{f.project.ID, f.user.ID}
			case "remove_write":
				query, args = `UPDATE users SET write_enabled=0 WHERE id=?`, []any{f.user.ID}
			case "reduce_scope":
				query, args = `UPDATE api_keys SET scopes_json='["files:read"]' WHERE id=?`, []any{id}
			case "change_project":
				query, args = `UPDATE api_keys SET project_ids_json=? WHERE id=?`, []any{`["` + f.other.ID + `"]`, id}
			}
			_, err := f.store.DB.Exec(query, args...)
			require.NoError(t, err)
			if change == "revoke" || change == "expire" || change == "disable" {
				_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_nodes", Arguments: map[string]any{"project_id": f.project.ID}})
				require.Error(t, err)
				require.Contains(t, err.Error(), "Unauthorized")
			} else {
				requireMCPError(t, cs, "create_directory", map[string]any{"project_id": f.project.ID, "name": "denied"}, "FORBIDDEN")
			}
		})
	}
}

func TestMCPHTTPBoundary(t *testing.T) {
	f := newAPIFixture(t)
	_, token := f.create(policy(nil, "files:read"))
	for _, test := range []struct {
		name, auth, origin, body, query string
		cookie                          bool
		status                          int
	}{
		{name: "missing key", status: 401},
		{name: "session only", cookie: true, status: 401},
		{name: "query key", query: "?api_key=" + token, status: 401},
		{name: "invalid explicit key", auth: "Bearer bad", cookie: true, status: 401},
		{name: "untrusted origin", auth: "Bearer " + token, origin: "https://evil.test", status: 403},
		{name: "malformed RPC", auth: "Bearer " + token, body: "{", status: 400},
		{name: "oversize body", auth: "Bearer " + token, body: strings.Repeat(" ", (2<<20)+1), status: 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://api.test/mcp"+test.query, strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			if test.auth != "" {
				r.Header.Set("Authorization", test.auth)
			}
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			if test.cookie {
				r.AddCookie(&http.Cookie{Name: sessionCookie, Value: f.session})
			}
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			require.Equal(t, test.status, w.Code, w.Body.String())
			if w.Code == 401 {
				require.Equal(t, `Bearer realm="altasci-mcp"`, w.Header().Get("WWW-Authenticate"))
			}
		})
	}
	r := httptest.NewRequest("OPTIONS", "http://api.test/mcp", nil)
	r.Header.Set("Origin", "https://web.test")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	require.Equal(t, 204, w.Code)
	require.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "MCP-Protocol-Version")
	r = httptest.NewRequest("POST", "http://api.test/mcp", strings.NewReader(`{}`))
	r.Header.Add("Authorization", "Bearer "+token)
	r.Header.Add("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	require.Equal(t, 401, w.Code)
}

func TestMCPScopesResourceIDsAndUploadOwnership(t *testing.T) {
	f := newAPIFixture(t)
	_, token := f.create(policy([]string{f.project.ID}, "files:read", "files:write", "files:delete"))
	_, readToken := f.create(policy([]string{f.project.ID}, "files:read"))
	_, writeToken := f.create(policy([]string{f.project.ID}, "files:write"))
	foreignDirResponse := f.request("POST", "/projects/"+f.other.ID+"/directories", "", map[string]any{"name": "foreign"}, true)
	require.Equal(t, 201, foreignDirResponse.Code)
	var foreignDir map[string]any
	require.NoError(t, json.Unmarshal(foreignDirResponse.Body.Bytes(), &foreignDir))
	foreignUploadResponse := f.request("POST", "/projects/"+f.other.ID+"/uploads", "", map[string]any{"filename": "foreign.txt", "size": 0}, true)
	require.Equal(t, 201, foreignUploadResponse.Code)
	var foreignUpload map[string]any
	require.NoError(t, json.Unmarshal(foreignUploadResponse.Body.Bytes(), &foreignUpload))
	server := httptest.NewServer(f.handler)
	defer server.Close()
	cs := connectMCP(t, server, token, "2026-07-28")
	readCS := connectMCP(t, server, readToken, "2026-07-28")
	writeCS := connectMCP(t, server, writeToken, "2026-07-28")
	requireMCPError(t, readCS, "create_directory", map[string]any{"project_id": f.project.ID, "name": "denied"}, "API_SCOPE_FORBIDDEN")
	requireMCPError(t, writeCS, "list_nodes", map[string]any{"project_id": f.project.ID}, "API_SCOPE_FORBIDDEN")
	for _, tool := range []string{"get_node", "delete_node", "read_file", "get_download_url"} {
		requireMCPError(t, cs, tool, map[string]any{"node_id": foreignDir["id"]}, "API_PROJECT_FORBIDDEN")
	}
	requireMCPError(t, cs, "update_node", map[string]any{"node_id": foreignDir["id"], "name": "denied"}, "API_PROJECT_FORBIDDEN")
	for _, tool := range []string{"complete_upload", "abort_upload", "presign_upload_parts", "upload_content"} {
		args := map[string]any{"upload_id": foreignUpload["upload_id"]}
		if tool == "presign_upload_parts" {
			args["part_numbers"] = []int{1}
		}
		if tool == "upload_content" {
			args["content"] = ""
		}
		requireMCPError(t, cs, tool, args, "API_PROJECT_FORBIDDEN")
	}
	dir := mustCallMCP(t, cs, "create_directory", map[string]any{"project_id": f.project.ID, "name": "own"})
	requireMCPError(t, writeCS, "delete_node", map[string]any{"node_id": dir["id"]}, "API_SCOPE_FORBIDDEN")
	// A destination outside the source project cannot bypass the key's range.
	requireMCPError(t, cs, "update_node", map[string]any{"node_id": dir["id"], "parent_id": foreignDir["id"]}, "NOT_FOUND")
	up := mustCallMCP(t, cs, "create_upload", map[string]any{"project_id": f.project.ID, "filename": "owner.txt", "size": 0})
	other := f.user
	other.ID, other.Email = security.NewID(), "upload-owner@example.test"
	require.NoError(t, f.store.CreateUser(context.Background(), other))
	_, err := f.store.DB.Exec(`UPDATE upload_sessions SET user_id=? WHERE id=?`, other.ID, up["upload_id"])
	require.NoError(t, err)
	_, err = f.store.DB.Exec(`UPDATE users SET role='admin' WHERE id=?`, f.user.ID)
	require.NoError(t, err)
	for _, tool := range []string{"complete_upload", "abort_upload", "presign_upload_parts", "upload_content"} {
		args := map[string]any{"upload_id": up["upload_id"]}
		if tool == "presign_upload_parts" {
			args["part_numbers"] = []int{1}
		}
		if tool == "upload_content" {
			args["content"] = ""
		}
		requireMCPError(t, cs, tool, args, "FORBIDDEN")
	}
}

func TestMCPConcurrentConflicts(t *testing.T) {
	f := newAPIFixture(t)
	_, token := f.create(policy([]string{f.project.ID}, "files:read", "files:write"))
	server := httptest.NewServer(f.handler)
	defer server.Close()
	cs := connectMCP(t, server, token, "2026-07-28")
	up := mustCallMCP(t, cs, "create_upload", map[string]any{"project_id": f.project.ID, "filename": "once.txt", "size": 4})
	mustCallMCP(t, cs, "upload_content", map[string]any{"upload_id": up["upload_id"], "content": "once"})
	for _, tool := range []string{"create_directory", "complete_upload"} {
		t.Run(tool, func(t *testing.T) {
			args := map[string]any{"project_id": f.project.ID, "name": "same-name"}
			if tool == "complete_upload" {
				args = map[string]any{"upload_id": up["upload_id"]}
			}
			const workers = 32
			results := make(chan *mcp.CallToolResult, workers)
			errors := make(chan error, workers)
			var wg sync.WaitGroup
			for range workers {
				wg.Go(func() {
					result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
					if err != nil {
						errors <- err
						return
					}
					results <- result
				})
			}
			wg.Wait()
			close(results)
			close(errors)
			for err := range errors {
				t.Error(err)
			}
			var success, conflicts int
			for result := range results {
				if !result.IsError {
					success++
					continue
				}
				raw, err := json.Marshal(result.Content)
				require.NoError(t, err)
				require.Contains(t, string(raw), "CONFLICT")
				conflicts++
			}
			require.Equal(t, 1, success)
			require.Equal(t, workers-1, conflicts)
		})
	}
	nodes := mustCallMCP(t, cs, "list_nodes", map[string]any{"project_id": f.project.ID})
	require.Len(t, nodes["items"], 2)
}

// These calls share SDK sessions and real HTTP connections. Distinct users/keys
// operate simultaneously; every denied call also tests for identity contamination.
func TestMCPConcurrentFileOperationsAndIsolation(t *testing.T) {
	f := newAPIFixture(t)
	_, tokenA := f.create(policy([]string{f.project.ID}, "files:read", "files:write", "files:delete"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	other := f.user
	other.ID, other.Email = security.NewID(), "mcp-other@example.test"
	require.NoError(t, f.store.CreateUser(ctx, other))
	now := repository.NowMS()
	require.NoError(t, f.store.PutMember(ctx, repository.ProjectMember{ProjectID: f.other.ID, UserID: other.ID, Permission: "write", GrantedBy: f.user.ID, CreatedAt: now, UpdatedAt: now}))
	_, err := f.store.DB.Exec(`UPDATE sessions SET user_id=? WHERE token_hash=?`, other.ID, security.TokenHash(f.session))
	require.NoError(t, err)
	_, tokenB := f.create(policy([]string{f.other.ID}, "files:read", "files:write", "files:delete"))
	server := httptest.NewServer(f.handler)
	defer server.Close()
	clients := []*mcp.ClientSession{connectMCP(t, server, tokenA, "2026-07-28"), connectMCP(t, server, tokenB, "2026-07-28")}
	projects := []string{f.project.ID, f.other.ID}
	const workers, rounds = 32, 2
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	started := time.Now()
	for worker := range workers {
		wg.Go(func() {
			cs, projectID := clients[worker%2], projects[worker%2]
			for round := range rounds {
				if _, err := callMCP(ctx, cs, "list_nodes", map[string]any{"project_id": projects[1-worker%2]}); err == nil {
					errors <- fmt.Errorf("worker %d escaped project scope", worker)
					return
				}
				up, err := callMCP(ctx, cs, "create_upload", map[string]any{"project_id": projectID, "filename": fmt.Sprintf("worker-%d-%d.txt", worker, round), "size": 5})
				if err != nil {
					errors <- fmt.Errorf("worker %d create: %w", worker, err)
					return
				}
				if _, err = callMCP(ctx, cs, "upload_content", map[string]any{"upload_id": up["upload_id"], "content": "hello"}); err != nil {
					errors <- err
					return
				}
				node, err := callMCP(ctx, cs, "complete_upload", map[string]any{"upload_id": up["upload_id"]})
				if err != nil {
					errors <- err
					return
				}
				if node["created_by"] != []string{f.user.ID, other.ID}[worker%2] {
					errors <- fmt.Errorf("worker %d identity crossed", worker)
					return
				}
				read, err := callMCP(ctx, cs, "read_file", map[string]any{"node_id": node["id"]})
				if err != nil || read["content"] != "hello" {
					errors <- fmt.Errorf("worker %d read failed: %v", worker, err)
					return
				}
				if _, err = callMCP(ctx, clients[1-worker%2], "get_node", map[string]any{"node_id": node["id"]}); err == nil {
					errors <- fmt.Errorf("worker %d leaked node", worker)
					return
				}
				if _, err = callMCP(ctx, cs, "delete_node", map[string]any{"node_id": node["id"]}); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	t.Logf("%d concurrent workers, %d complete file lifecycles, %d HTTP tool calls, elapsed %s", workers, workers*rounds, workers*rounds*7, time.Since(started))
}
