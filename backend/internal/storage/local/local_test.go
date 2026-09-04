package local

import (
	"context"
	"io"
	"strings"
	"testing"

	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/altasci/network-storage/backend/internal/storage/contracttest"
	"github.com/stretchr/testify/require"
)

func TestStorageContract(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	contracttest.Run(t, s)
	// Local-specific range behavior is also checked independently.
	ctx := context.Background()
	key := "v1/objects/project/blob"
	require.NoError(t, s.Put(ctx, key, strings.NewReader("hello"), 5, "text/plain"))
	info, err := s.Head(ctx, key)
	require.NoError(t, err)
	require.EqualValues(t, 5, info.Size)
	reader, _, err := s.Open(ctx, key, &storageapi.Range{Start: 1, End: 3})
	require.NoError(t, err)
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, "ell", string(raw))
	require.NoError(t, s.Delete(ctx, key))
	_, err = s.Head(ctx, key)
	require.Error(t, err)
}
func TestRejectsWrongSizeAndTraversal(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	require.Error(t, s.Put(context.Background(), "v1/objects/p/b", strings.NewReader("too long"), 3, "text/plain"))
	require.Error(t, s.Put(context.Background(), "../../etc/passwd", strings.NewReader("x"), 1, "text/plain"))
}
