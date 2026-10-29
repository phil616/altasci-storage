package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/altasci/network-storage/backend/internal/ascli"
	"github.com/altasci/network-storage/backend/internal/authorization"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/sharing"
	"github.com/altasci/network-storage/backend/internal/storage/factory"
	"github.com/stretchr/testify/require"
)

// Exercise the CLI against the real router, SQLite share/grant checks and local
// storage rather than only a hand-written simulation of the public protocol.
func TestASCLIRealPublicShareFlow(t *testing.T) {
	f := newAPIFixture(t)
	key := bytes.Repeat([]byte{1}, 32)
	box, err := security.NewSecretBox(key)
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(nil)
	apiURL := "http://" + server.Listener.Addr().String()
	f.handler = New(Options{Store: f.store, Authorization: authorization.New(f.store), Factory: factory.New(f.store, box), Box: box, Sharing: sharing.New(key), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), APIURL: apiURL, WebURL: "https://web.test"})
	server.Config.Handler = f.handler
	server.Start()
	defer server.Close()
	folderResponse := f.request("POST", "/projects/"+f.project.ID+"/directories", "", map[string]string{"name": "folder"}, true)
	require.Equal(t, 201, folderResponse.Code, folderResponse.Body.String())
	var folder struct{ ID string }
	require.NoError(t, json.Unmarshal(folderResponse.Body.Bytes(), &folder))
	var fileIDs []string
	for _, parent := range []string{"", folder.ID} {
		up := f.request("POST", "/projects/"+f.project.ID+"/uploads", "", map[string]any{"filename": "hello.txt", "parent_id": parent, "size": 5, "mime_type": "text/plain"}, true)
		require.Equal(t, 201, up.Code, up.Body.String())
		var upload struct {
			ID string `json:"upload_id"`
		}
		require.NoError(t, json.Unmarshal(up.Body.Bytes(), &upload))
		require.Equal(t, 204, f.request("PUT", "/uploads/"+upload.ID+"/content", "", "hello", true).Code)
		completed := f.request("POST", "/uploads/"+upload.ID+"/complete", "", map[string]any{"parts": []any{}}, true)
		require.Equal(t, 200, completed.Code, completed.Body.String())
		var file struct{ ID string }
		require.NoError(t, json.Unmarshal(completed.Body.Bytes(), &file))
		fileIDs = append(fileIDs, file.ID)
	}
	created := f.request("POST", "/shares", "", map[string]any{"node_ids": []string{fileIDs[0], folder.ID}, "require_code": true}, true)
	require.Equal(t, 201, created.Code, created.Body.String())
	var share struct{ ID, URL, Code string }
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &share))
	dir := t.TempDir()
	var out bytes.Buffer
	args := []string{share.URL, "--pwd", share.Code, "--api-url", apiURL, "--download-dir", dir}
	noEnv := func(string) string { return "" }
	require.Equal(t, 0, ascli.Run(context.Background(), args, &out, noEnv, "test"), out.String())
	for _, name := range []string{"hello.txt", filepath.Join("folder", "hello.txt")} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		require.Equal(t, "hello", string(data))
	}
	require.NotContains(t, out.String(), share.Code)
	// Re-running the same job must report a conflict instead of succeeding with
	// stale files. An explicit overwrite can replace them.
	require.Equal(t, 1, ascli.Run(context.Background(), args, io.Discard, noEnv, "test"))
	require.Equal(t, 0, ascli.Run(context.Background(), append(args, "--overwrite"), io.Discard, noEnv, "test"))
	require.Equal(t, 204, f.request("DELETE", "/shares/"+share.ID, "", nil, true).Code)
	require.Equal(t, 3, ascli.Run(context.Background(), append(args, "--overwrite"), io.Discard, noEnv, "test"))
}
