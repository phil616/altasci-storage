package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/altasci/network-storage/backend/internal/repository"
	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpInlineLimit = 1 << 20

// mcpHandler uses the official SDK's sessionless transport. Authentication belongs
// to each HTTP request, never a shared server or a client-supplied MCP session ID.
func (s *Server) mcpHandler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "altasci-storage", Version: "1.0.0"}, &mcp.ServerOptions{
		Instructions: "Manage files in authorized AltasCI projects. IDs refer to virtual storage nodes, not host filesystem paths. Account, password, API key, sharing, membership and administrator operations are unavailable. Uploads require create_upload, content transfer, then complete_upload. Never send your API key to external presigned URLs.",
		SetCacheable: func(_ context.Context, _ mcp.Request, c *mcp.Cacheable) {
			*c = mcp.Cacheable{TTLMs: 0, CacheScope: "private"}
		},
	})
	s.addMCPTools(server)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 2 << 20, PropagateRequestCancellation: true,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(r.Context(), origin) {
			writeError(w, r, http.StatusForbidden, "ORIGIN_FORBIDDEN", "The request origin is not allowed.")
			return
		}
		// No session-cookie fallback, including discovery and tool listing.
		w.Header().Set("WWW-Authenticate", `Bearer realm="altasci-mcp"`)
		s.authenticateAPIKey(w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Del("WWW-Authenticate")
			transport.ServeHTTP(w, r)
		}))
	})
}

type mcpProjectInput struct {
	ProjectID string `json:"project_id" jsonschema:"Authorized project ID"`
}
type mcpNodeInput struct {
	NodeID string `json:"node_id" jsonschema:"File or directory node ID"`
}
type mcpUploadInput struct {
	UploadID string `json:"upload_id" jsonschema:"Upload ID owned by the current user"`
}
type mcpListNodesInput struct {
	ProjectID string `json:"project_id"`
	ParentID  string `json:"parent_id,omitempty" jsonschema:"Directory ID; omit for the project root"`
}
type mcpDirectoryInput struct {
	ProjectID string `json:"project_id"`
	ParentID  string `json:"parent_id,omitempty" jsonschema:"Directory ID; omit for the project root"`
	Name      string `json:"name"`
}
type mcpUpdateNodeInput struct {
	NodeID   string  `json:"node_id"`
	Name     *string `json:"name,omitempty" jsonschema:"New name; omit to keep the name"`
	ParentID *string `json:"parent_id,omitempty" jsonschema:"Destination directory in the same project; empty string moves to root; omit to keep the parent"`
}
type mcpCreateUploadInput struct {
	ProjectID string `json:"project_id"`
	ParentID  string `json:"parent_id,omitempty"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size" jsonschema:"Exact file size in bytes, between zero and 5 TiB"`
	MIMEType  string `json:"mime_type,omitempty"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"Replace an existing file of the same name; default false"`
}
type mcpPresignPartsInput struct {
	UploadID    string  `json:"upload_id"`
	PartNumbers []int32 `json:"part_numbers" jsonschema:"One to 100 unique part numbers, each between 1 and 10000"`
}
type mcpCompleteUploadInput struct {
	UploadID string                     `json:"upload_id"`
	Parts    []storageapi.CompletedPart `json:"parts,omitempty" jsonschema:"Completed multipart ETags; omit for local and single uploads"`
}
type mcpUploadContentInput struct {
	UploadID string `json:"upload_id"`
	Content  string `json:"content" jsonschema:"UTF-8 text or standard base64 encoded file contents; at most 1 MiB decoded"`
	Encoding string `json:"encoding,omitempty" jsonschema:"utf8 (default) or base64"`
}
type mcpReadFileInput struct {
	NodeID   string `json:"node_id"`
	Offset   int64  `json:"offset,omitempty" jsonschema:"Byte offset, default zero"`
	Limit    int64  `json:"limit,omitempty" jsonschema:"Maximum bytes to read, default 65536, maximum 1048576"`
	Encoding string `json:"encoding,omitempty" jsonschema:"utf8 (default) or base64; use base64 for binary files or byte ranges splitting UTF-8 characters"`
}

// Every registration is an explicit capability. There is deliberately no tool
// taking an arbitrary HTTP method, route, URL, SQL query or host filesystem path.
func (s *Server) addMCPTools(server *mcp.Server) {
	addMCPTool(server, s, "list_projects", "List authorized projects. Requires projects:read.", "projects:read", s.listProjects, http.MethodGet, true, false,
		func(struct{}) mcpOperation { return mcpOperation{} })
	addMCPTool(server, s, "get_project", "Get an authorized project. Requires projects:read.", "projects:read", s.getProject, http.MethodGet, true, false,
		func(in mcpProjectInput) mcpOperation {
			return mcpOperation{params: map[string]string{"projectID": in.ProjectID}}
		})
	addMCPTool(server, s, "list_nodes", "List files and directories in a project directory. Requires files:read.", "files:read", s.listNodes, http.MethodGet, true, false,
		func(in mcpListNodesInput) mcpOperation {
			return mcpOperation{params: map[string]string{"projectID": in.ProjectID}, query: url.Values{"parent_id": {in.ParentID}}}
		})
	addMCPTool(server, s, "get_node", "Get file or directory metadata. Requires files:read.", "files:read", s.getNode, http.MethodGet, true, false,
		func(in mcpNodeInput) mcpOperation { return nodeOperation(in.NodeID, nil) })
	addMCPTool(server, s, "create_directory", "Create a directory. Requires files:write.", "files:write", s.createDirectory, http.MethodPost, false, false,
		func(in mcpDirectoryInput) mcpOperation {
			return mcpOperation{params: map[string]string{"projectID": in.ProjectID}, body: map[string]any{"parent_id": in.ParentID, "name": in.Name}}
		})
	addMCPTool(server, s, "update_node", "Rename or move a node within its project. Requires files:write.", "files:write", s.updateNode, http.MethodPatch, false, true,
		func(in mcpUpdateNodeInput) mcpOperation {
			body := map[string]any{}
			if in.Name != nil {
				body["name"] = *in.Name
			}
			if in.ParentID != nil {
				body["parent_id"] = *in.ParentID
			}
			return nodeOperation(in.NodeID, body)
		})
	addMCPTool(server, s, "delete_node", "Delete a file or a directory and all its descendants. Requires files:delete.", "files:delete", s.deleteNode, http.MethodDelete, false, true,
		func(in mcpNodeInput) mcpOperation { return nodeOperation(in.NodeID, nil) })
	addMCPTool(server, s, "get_download_url", "Get a download URL. Local URLs require the API key header; never send the API key to external signed URLs. Requires files:read.", "files:read", s.download, http.MethodPost, true, false,
		func(in mcpNodeInput) mcpOperation { return nodeOperation(in.NodeID, nil) })
	addMCPTool(server, s, "read_file", "Read a bounded byte range from a file on any storage backend. Requires files:read.", "files:read", s.mcpReadFile, http.MethodGet, true, false,
		func(in mcpReadFileInput) mcpOperation { return nodeOperation(in.NodeID, in) })
	addMCPTool(server, s, "create_upload", "Start an upload. Transfer content with upload_content (up to 1 MiB) or the returned HTTP upload target, then complete_upload. Large remote uploads use presign_upload_parts. Requires files:write.", "files:write", s.createUpload, http.MethodPost, false, true,
		func(in mcpCreateUploadInput) mcpOperation {
			return mcpOperation{params: map[string]string{"projectID": in.ProjectID}, body: map[string]any{"parent_id": in.ParentID, "filename": in.Filename, "size": in.Size, "mime_type": in.MIMEType, "overwrite": in.Overwrite}}
		})
	addMCPTool(server, s, "upload_content", "Write up to 1 MiB of content to your pending local or single upload on any backend. Call complete_upload afterwards. Requires files:write.", "files:write", s.mcpUploadContent, http.MethodPut, false, true,
		func(in mcpUploadContentInput) mcpOperation { return uploadOperation(in.UploadID, in) })
	addMCPTool(server, s, "presign_upload_parts", "Get signed URLs for multipart upload parts. Do not send the API key to these URLs. Requires files:write.", "files:write", s.presignParts, http.MethodPost, false, false,
		func(in mcpPresignPartsInput) mcpOperation {
			return uploadOperation(in.UploadID, map[string]any{"part_numbers": in.PartNumbers})
		})
	addMCPTool(server, s, "complete_upload", "Commit your upload and return file metadata. Requires files:write.", "files:write", s.completeUpload, http.MethodPost, false, true,
		func(in mcpCompleteUploadInput) mcpOperation {
			return uploadOperation(in.UploadID, map[string]any{"parts": in.Parts})
		})
	addMCPTool(server, s, "abort_upload", "Cancel your pending upload. Requires files:write.", "files:write", s.abortUpload, http.MethodDelete, false, true,
		func(in mcpUploadInput) mcpOperation { return uploadOperation(in.UploadID, nil) })
}

type mcpOperation struct {
	params map[string]string
	query  url.Values
	body   any
}

func nodeOperation(id string, body any) mcpOperation {
	return mcpOperation{params: map[string]string{"nodeID": id}, body: body}
}
func uploadOperation(id string, body any) mcpOperation {
	return mcpOperation{params: map[string]string{"uploadID": id}, body: body}
}

// Adapt existing handlers in-process to preserve their validation, authorization,
// upload lifecycle and audit behavior. Only server-selected handlers can run.
func addMCPTool[In any](server *mcp.Server, s *Server, name, description, scope string, handler http.HandlerFunc, method string, readOnly, destructive bool, operation func(In) mcpOperation) {
	closedWorld := false
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		ReadOnlyHint: readOnly, DestructiveHint: &destructive, OpenWorldHint: &closedWorld,
	}}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		op := operation(in)
		for _, id := range op.params {
			if strings.TrimSpace(id) == "" {
				return nil, nil, fmt.Errorf("resource ID must not be empty")
			}
		}
		body, err := json.Marshal(op.body)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid tool arguments")
		}
		route := chi.NewRouteContext()
		for key, value := range op.params {
			route.URLParams.Add(key, value)
		}
		r, err := http.NewRequestWithContext(context.WithValue(ctx, chi.RouteCtxKey, route), method, "/mcp", bytes.NewReader(body))
		if err != nil {
			return nil, nil, fmt.Errorf("could not prepare operation")
		}
		r.Header.Set("Content-Type", "application/json")
		r.URL.RawQuery = op.query.Encode()
		if _, ok := apiKeyFrom(r); !ok {
			return nil, nil, fmt.Errorf("API key authentication is required")
		}
		response := &mcpResponse{header: make(http.Header), status: http.StatusOK}
		s.apiAccess(scope)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// MCP never grants the administrator exception for another user's upload.
			if id := op.params["uploadID"]; id != "" {
				up, err := s.store.UploadByID(ctx, id)
				u, _ := userFrom(r)
				if err != nil {
					writeRepoError(w, r, err)
					return
				}
				if up.UserID != u.ID {
					writeRepoError(w, r, repository.ErrForbidden)
					return
				}
			}
			handler(w, r)
		})).ServeHTTP(response, r)
		if response.status >= 400 {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: response.body.String()}}}, nil, nil
		}
		if response.body.Len() == 0 {
			return nil, map[string]any{"ok": true}, nil
		}
		var out any
		if err := json.Unmarshal(response.body.Bytes(), &out); err != nil {
			return nil, nil, fmt.Errorf("could not decode operation result")
		}
		return nil, out, nil
	})
}

type mcpResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *mcpResponse) Header() http.Header         { return w.header }
func (w *mcpResponse) WriteHeader(status int)      { w.status = status }
func (w *mcpResponse) Write(p []byte) (int, error) { return w.body.Write(p) }

func (s *Server) mcpReadFile(w http.ResponseWriter, r *http.Request) {
	var in mcpReadFileInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Limit == 0 {
		in.Limit = 64 << 10
	}
	if in.Offset < 0 || in.Limit < 1 || in.Limit > mcpInlineLimit || (in.Encoding != "" && in.Encoding != "utf8" && in.Encoding != "base64") {
		writeError(w, r, 422, "READ_INVALID", "Use a nonnegative offset, a limit of 1 to 1048576 bytes and utf8 or base64 encoding.")
		return
	}
	n, blob, err := s.store.BlobForNode(r.Context(), chi.URLParam(r, "nodeID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if in.Offset > blob.Size {
		writeError(w, r, 422, "READ_INVALID", "Offset exceeds file size.")
		return
	}
	size := min(in.Limit, blob.Size-in.Offset)
	data := []byte{}
	if size > 0 {
		adapter, backend, err := s.factory.ByID(r.Context(), blob.StorageBackendID)
		if err != nil || !backend.Enabled {
			writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The storage backend is unavailable.")
			return
		}
		reader, _, err := adapter.Open(r.Context(), blob.ObjectKey, &storageapi.Range{Start: in.Offset, End: in.Offset + size - 1})
		if err != nil {
			writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The file could not be opened.")
			return
		}
		defer reader.Close()
		data, err = io.ReadAll(io.LimitReader(reader, size))
		if err != nil || int64(len(data)) != size {
			writeError(w, r, 503, "STORAGE_UNAVAILABLE", "The file range could not be read.")
			return
		}
	}
	encoding, content := "utf8", string(data)
	if in.Encoding == "base64" {
		encoding, content = "base64", base64.StdEncoding.EncodeToString(data)
	} else if !utf8.Valid(data) {
		writeError(w, r, 422, "ENCODING_INVALID", "The byte range is not valid UTF-8; request base64 encoding.")
		return
	}
	writeJSON(w, 200, map[string]any{"node_id": n.ID, "mime_type": n.MIMEType, "size": blob.Size, "offset": in.Offset, "bytes_read": len(data), "eof": in.Offset+size == blob.Size, "encoding": encoding, "content": content})
}

func (s *Server) mcpUploadContent(w http.ResponseWriter, r *http.Request) {
	var in mcpUploadContentInput
	if !decodeJSONLimit(w, r, &in, 2<<20) {
		return
	}
	data := []byte(in.Content)
	if in.Encoding == "base64" {
		var err error
		data, err = base64.StdEncoding.Strict().DecodeString(in.Content)
		if err != nil {
			writeError(w, r, 422, "ENCODING_INVALID", "Content must be standard base64.")
			return
		}
	} else if in.Encoding != "" && in.Encoding != "utf8" {
		writeError(w, r, 422, "ENCODING_INVALID", "Use utf8 or base64 encoding.")
		return
	}
	if len(data) > mcpInlineLimit {
		writeError(w, r, 413, "CONTENT_TOO_LARGE", "Inline uploads support at most 1 MiB; use the HTTP upload target for larger files.")
		return
	}
	up, err := s.store.UploadByID(r.Context(), chi.URLParam(r, "uploadID"))
	if err != nil {
		writeRepoError(w, r, err)
		return
	}
	if (up.UploadType != "local" && up.UploadType != "single") || up.Status != "created" || up.ExpiresAt <= repository.NowMS() {
		writeError(w, r, 409, "UPLOAD_STATE_INVALID", "The upload cannot accept inline content.")
		return
	}
	if int64(len(data)) != up.ExpectedSize {
		writeError(w, r, 422, "UPLOAD_SIZE_MISMATCH", "Content length must match the declared upload size.")
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
	if err := adapter.Put(r.Context(), blob.ObjectKey, bytes.NewReader(data), up.ExpectedSize, up.MIMEType); err != nil {
		writeError(w, r, 502, "STORAGE_PROVIDER_ERROR", "The file content could not be stored.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
