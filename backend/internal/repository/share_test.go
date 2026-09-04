package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/stretchr/testify/require"
)

func TestShareLifecycleAndDescendantBoundary(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	root := directory(f, "Shared", "")
	child := directory(f, "Child", root.ID)
	outside := directory(f, "Outside", "")
	require.NoError(t, f.store.CreateDirectory(ctx, root))
	require.NoError(t, f.store.CreateDirectory(ctx, child))
	require.NoError(t, f.store.CreateDirectory(ctx, outside))
	ok, err := f.store.IsDescendant(ctx, root.ID, child.ID)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = f.store.IsDescendant(ctx, root.ID, outside.ID)
	require.NoError(t, err)
	require.False(t, ok)
	now := repository.NowMS()
	sh := repository.Share{ID: security.NewID(), ProjectID: f.project.ID, TargetNodeID: root.ID, CreatedBy: f.user.ID, PublicTokenHash: security.TokenHash("token"), RequireCode: false, ExpiresAt: sql.NullInt64{Int64: now - 1, Valid: true}, CreatedAt: now}
	require.NoError(t, f.store.CreateShare(ctx, sh))
	loaded, err := f.store.ShareByToken(ctx, "token")
	require.NoError(t, err)
	require.Error(t, repository.ActiveShare(loaded, now))
	loaded.ExpiresAt = sql.NullInt64{}
	loaded.DisabledAt = sql.NullInt64{Int64: now, Valid: true}
	require.Error(t, repository.ActiveShare(loaded, now))
}
