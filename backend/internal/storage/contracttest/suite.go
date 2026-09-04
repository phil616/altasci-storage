package contracttest

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Run exercises the provider-neutral storage semantics. Local has the explicit
// PLAN.md exception that it streams through Go and therefore has no presigned
// or provider multipart operations.
func Run(t *testing.T, storage storageapi.Storage) {
	t.Helper()
	ctx := context.Background()
	key := "v1/objects/contract/" + uuid.NewString()
	require.NoError(t, storage.Put(ctx, key, strings.NewReader("contract"), 8, "text/plain"))
	t.Cleanup(func() { _ = storage.Delete(context.Background(), key) })
	info, err := storage.Head(ctx, key)
	require.NoError(t, err)
	require.EqualValues(t, 8, info.Size)
	reader, _, err := storage.Open(ctx, key, &storageapi.Range{Start: 1, End: 3})
	require.NoError(t, err)
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, "ont", string(raw))
	if storage.Type() != "local" {
		download, err := storage.PresignDownload(ctx, storageapi.DownloadRequest{Key: key, Filename: "contract.txt", Expires: time.Minute})
		require.NoError(t, err)
		require.NotEmpty(t, download.URL)
		upload, err := storage.CreateUpload(ctx, storageapi.CreateUploadRequest{Key: key + "-put", Size: 8, ContentType: "text/plain", Expires: time.Minute})
		require.NoError(t, err)
		require.NotEmpty(t, upload.URL)
		multipartKey := key + "-multipart"
		multipart, err := storage.CreateUpload(ctx, storageapi.CreateUploadRequest{Key: multipartKey, Size: 16 << 20, ContentType: "application/octet-stream", Multipart: true, Expires: time.Minute})
		require.NoError(t, err)
		require.NotEmpty(t, multipart.ProviderUploadID)
		part, err := storage.PresignUploadPart(ctx, storageapi.PresignPartRequest{Key: multipartKey, ProviderUploadID: multipart.ProviderUploadID, PartNumber: 1, Expires: time.Minute})
		require.NoError(t, err)
		require.NotEmpty(t, part.URL)
		require.NoError(t, storage.AbortMultipartUpload(ctx, multipartKey, multipart.ProviderUploadID))
	}
	require.NoError(t, storage.Delete(ctx, key))
}
