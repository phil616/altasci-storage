package authorization

import (
	"context"
	"database/sql"
	"testing"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestProjectAuthorizationMatrix(t *testing.T) {
	ctx := context.Background()
	store := testutil.Store(t)
	now := repository.NowMS()
	admin := user("admin", true, "admin", now)
	reader := user("reader", false, "user", now)
	writer := user("writer", true, "user", now)
	disabled := user("disabled", false, "user", now)
	outsider := user("outsider", true, "user", now)
	for _, u := range []repository.User{admin, reader, writer, disabled, outsider} {
		require.NoError(t, store.CreateUser(ctx, u))
	}
	_, err := store.DB.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=?`, disabled.ID)
	require.NoError(t, err)
	backend := repository.StorageBackend{ID: security.NewID(), Name: "local", Type: "local", Enabled: true, ConfigJSON: `{"root_path":"/tmp/test"}`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateStorageBackend(ctx, backend))
	project := repository.Project{ID: security.NewID(), Name: "p", StorageBackendID: backend.ID, CreatedBy: admin.ID, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateProject(ctx, project, true))
	for _, m := range []repository.ProjectMember{{ProjectID: project.ID, UserID: reader.ID, Permission: "read", GrantedBy: admin.ID, CreatedAt: now, UpdatedAt: now}, {ProjectID: project.ID, UserID: writer.ID, Permission: "write", GrantedBy: admin.ID, CreatedAt: now, UpdatedAt: now}, {ProjectID: project.ID, UserID: disabled.ID, Permission: "write", GrantedBy: admin.ID, CreatedAt: now, UpdatedAt: now}} {
		require.NoError(t, store.PutMember(ctx, m))
	}
	svc := New(store)
	require.NoError(t, svc.CanReadProject(ctx, admin, project.ID))
	require.NoError(t, svc.CanReadProject(ctx, reader, project.ID))
	require.ErrorIs(t, svc.CanWriteProject(ctx, reader, project.ID), repository.ErrForbidden)
	require.NoError(t, svc.CanWriteProject(ctx, writer, project.ID))
	require.ErrorIs(t, svc.CanWriteProject(ctx, disabled, project.ID), repository.ErrForbidden)
	require.ErrorIs(t, svc.CanReadProject(ctx, outsider, project.ID), repository.ErrForbidden)
}
func user(email string, write bool, role string, now int64) repository.User {
	return repository.User{ID: security.NewID(), Email: email + "@example.com", PasswordHash: sql.NullString{String: "x", Valid: true}, Role: role, WriteEnabled: write, Status: "active", CreatedAt: now, UpdatedAt: now}
}
