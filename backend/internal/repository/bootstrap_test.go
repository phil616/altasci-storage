package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestInitializeIsAtomicAndCanOnlyRunOnce(t *testing.T) {
	store := testutil.Store(t)
	ctx := context.Background()
	admin := repository.User{
		ID:           "01991a62-c11a-7c99-8f51-b35e6ab6fe10",
		Email:        "Admin@Example.com",
		PasswordHash: sql.NullString{String: "hash", Valid: true},
		Role:         "admin",
		WriteEnabled: true,
		Status:       "active",
		CreatedAt:    repository.NowMS(),
		UpdatedAt:    repository.NowMS(),
	}
	settings := map[string]any{
		"site.public_web_url": "https://web.example.com",
		"site.public_api_url": "https://api.example.com",
	}

	require.NoError(t, store.Initialize(ctx, admin, settings))
	initialized, err := store.IsInitialized(ctx)
	require.NoError(t, err)
	require.True(t, initialized)
	created, err := store.UserByEmail(ctx, "admin@example.com")
	require.NoError(t, err)
	require.Equal(t, admin.ID, created.ID)

	err = store.Initialize(ctx, admin, settings)
	require.Error(t, err)
	require.True(t, errors.Is(err, repository.ErrConflict))
}

func TestInitializeRejectsPartialStateWithoutAddingAdministrator(t *testing.T) {
	store := testutil.Store(t)
	ctx := context.Background()
	require.NoError(t, store.SetSetting(ctx, "partial", true, ""))
	admin := repository.User{ID: "admin", Email: "admin@example.com", PasswordHash: sql.NullString{String: "hash", Valid: true}, Role: "admin", WriteEnabled: true, Status: "active", CreatedAt: repository.NowMS(), UpdatedAt: repository.NowMS()}

	err := store.Initialize(ctx, admin, map[string]any{"site.public_web_url": "https://web.example.com", "site.public_api_url": "https://api.example.com"})
	require.Error(t, err)
	_, err = store.UserByID(ctx, admin.ID)
	require.True(t, errors.Is(err, repository.ErrNotFound))
}

func TestInitializeRollsBackWhenASettingCannotBeEncoded(t *testing.T) {
	store := testutil.Store(t)
	ctx := context.Background()
	admin := repository.User{ID: "admin", Email: "admin@example.com", PasswordHash: sql.NullString{String: "hash", Valid: true}, Role: "admin", WriteEnabled: true, Status: "active", CreatedAt: repository.NowMS(), UpdatedAt: repository.NowMS()}

	err := store.Initialize(ctx, admin, map[string]any{"invalid": make(chan int)})
	require.Error(t, err)
	_, err = store.UserByID(ctx, admin.ID)
	require.True(t, errors.Is(err, repository.ErrNotFound))

	var settings int
	require.NoError(t, store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_settings").Scan(&settings))
	require.Zero(t, settings)
}
