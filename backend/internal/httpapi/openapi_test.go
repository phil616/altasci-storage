package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Check the checked-in contract against the registered router without a database
// or network. Payload semantics still need handler/integration test coverage.
func TestOpenAPIContract(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/openapi.yaml")
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &spec))
	require.Equal(t, "3.1.0", spec["openapi"])
	paths := spec["paths"].(map[string]any)
	methods := map[string]bool{"get": true, "head": true, "post": true, "put": true, "patch": true, "delete": true, "options": true}
	documented := map[string]bool{}
	operationIDs := map[string]bool{}
	for path, value := range paths {
		for method, value := range value.(map[string]any) {
			if !methods[method] {
				continue
			}
			op := value.(map[string]any)
			id, _ := op["operationId"].(string)
			require.NotEmpty(t, id, "%s %s", method, path)
			require.False(t, operationIDs[id], "duplicate operationId %s", id)
			operationIDs[id] = true
			require.NotEmpty(t, op["responses"], "%s", id)
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}
	registered := map[string]bool{}
	router := New(Options{}).(chi.Routes)
	require.NoError(t, chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+path] = true
		return nil
	}))
	require.Equal(t, registered, documented, "OpenAPI methods/paths must match server.go")

	// All references are local and must resolve, including shared response models.
	var checkRefs func(any)
	checkRefs = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if ref, ok := value["$ref"].(string); ok {
				require.True(t, strings.HasPrefix(ref, "#/"), "unsupported reference %s", ref)
				var target any = spec
				for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
					object, ok := target.(map[string]any)
					require.True(t, ok, "invalid reference %s", ref)
					target, ok = object[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
					require.True(t, ok, "unresolved reference %s", ref)
				}
			}
			for _, child := range value {
				checkRefs(child)
			}
		case []any:
			for _, child := range value {
				checkRefs(child)
			}
		}
	}
	checkRefs(spec)

	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	responses := map[string]map[string]any{
		"User":           presentUser(repository.User{}),
		"Project":        presentProject(repository.Project{}),
		"Node":           presentNode(repository.Node{}),
		"Share":          presentShare(repository.Share{}),
		"APIKey":         presentAPIKey(repository.APIKey{}),
		"StorageBackend": presentStorage(repository.StorageBackend{}),
		"OIDCProvider":   presentOIDC(repository.OIDCProvider{}),
	}
	for name, response := range responses {
		t.Run(name+"Fields", func(t *testing.T) {
			schema := schemas[name].(map[string]any)
			properties := schema["properties"].(map[string]any)
			require.Len(t, properties, len(response), "response fields must match the schema")
			for field := range response {
				require.Contains(t, properties, field)
			}
			for _, field := range schema["required"].([]any) {
				require.Contains(t, response, field.(string))
			}
		})
	}
	settings := schemas["Settings"].(map[string]any)["properties"].(map[string]any)
	require.Len(t, settings, len(allowedSettings))
	for key := range allowedSettings {
		require.Contains(t, settings, key)
	}
	enum := schemas["APIKeyScope"].(map[string]any)["enum"].([]any)
	var scopes []string
	for _, scope := range enum {
		scopes = append(scopes, scope.(string))
	}
	require.ElementsMatch(t, apiScopes, scopes)

	// Compare per-route API scopes to apiAccess declarations in the actual source.
	file, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	require.NoError(t, err)
	declared := map[string]string{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !methods[strings.ToLower(method.Sel.Name)] {
			return true
		}
		with, ok := method.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range with.Args {
			access, ok := arg.(*ast.CallExpr)
			if !ok || len(access.Args) != 1 {
				continue
			}
			selector, ok := access.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "apiAccess" {
				continue
			}
			path, err := strconv.Unquote(call.Args[0].(*ast.BasicLit).Value)
			require.NoError(t, err)
			scope, err := strconv.Unquote(access.Args[0].(*ast.BasicLit).Value)
			require.NoError(t, err)
			declared[strings.ToLower(method.Sel.Name)+" /api/v1"+path] = scope
		}
		return true
	})
	documentedScopes := map[string]string{}
	for path, value := range paths {
		for method, value := range value.(map[string]any) {
			if !methods[method] {
				continue
			}
			op := value.(map[string]any)
			if scope, ok := op["x-api-scope"].(string); ok {
				documentedScopes[method+" "+path] = scope
			}
		}
	}
	require.Equal(t, declared, documentedScopes, "OpenAPI API key scopes must match apiAccess")
}
