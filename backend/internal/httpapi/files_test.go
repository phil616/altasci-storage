package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPresentUploadHeadersUsesJSONObjectForEmptyHeaders(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"headers": presentUploadHeaders(nil)})
	require.NoError(t, err)
	require.JSONEq(t, `{"headers":{}}`, string(raw))
}

func TestPresentUploadHeadersPreservesSignedHeaders(t *testing.T) {
	headers := map[string]string{"Content-Type": "application/octet-stream"}
	require.Equal(t, headers, presentUploadHeaders(headers))
}
