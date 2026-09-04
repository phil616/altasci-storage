package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	store   *repository.Store
	user    repository.User
	project repository.Project
	backend repository.StorageBackend
}

func setup(t *testing.T) fixture {
	ctx := context.Background()
	s := testutil.Store(t)
	now := repository.NowMS()
	u := repository.User{ID: security.NewID(), Email: "admin@example.com", PasswordHash: sql.NullString{String: "x", Valid: true}, Role: "admin", WriteEnabled: true, Status: "active", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, s.CreateUser(ctx, u))
	b := repository.StorageBackend{ID: security.NewID(), Name: "local", Type: "local", Enabled: true, ConfigJSON: `{"root_path":"/tmp/test"}`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, s.CreateStorageBackend(ctx, b))
	p := repository.Project{ID: security.NewID(), Name: "project", StorageBackendID: b.ID, CreatedBy: u.ID, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, s.CreateProject(ctx, p, true))
	return fixture{s, u, p, b}
}
func directory(f fixture, name, parent string) repository.Node {
	now := repository.NowMS()
	return repository.Node{ID: security.NewID(), ProjectID: f.project.ID, ParentID: sql.NullString{String: parent, Valid: parent != ""}, Name: name, CreatedBy: f.user.ID, CreatedAt: now, UpdatedAt: now}
}
func TestRenameMoveCycleDuplicateAndDelete(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	a := directory(f, "A", "")
	b := directory(f, "B", a.ID)
	require.NoError(t, f.store.CreateDirectory(ctx, a))
	require.NoError(t, f.store.CreateDirectory(ctx, b))
	duplicate := directory(f, "A", "")
	require.ErrorIs(t, f.store.CreateDirectory(ctx, duplicate), repository.ErrConflict)
	aTarget := b.ID
	_, err := f.store.UpdateNode(ctx, a.ID, nil, &aTarget)
	require.Error(t, err)
	name := "Renamed"
	moved := ""
	updated, err := f.store.UpdateNode(ctx, b.ID, &name, &moved)
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.NoError(t, f.store.DeleteNode(ctx, b.ID))
	_, err = f.store.NodeByID(ctx, b.ID)
	require.ErrorIs(t, err, repository.ErrNotFound)
}
func TestUploadSizeAndOverwriteTransaction(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	makeUpload := func(overwrite bool) (repository.Upload, repository.Blob) {
		now := repository.NowMS()
		blobID := security.NewID()
		blob := repository.Blob{ID: blobID, ProjectID: f.project.ID, StorageBackendID: f.backend.ID, ObjectKey: "v1/objects/" + f.project.ID + "/" + blobID, Size: 5, MIMEType: "text/plain", CreatedAt: now}
		up := repository.Upload{ID: security.NewID(), ProjectID: f.project.ID, BlobID: blobID, UserID: f.user.ID, UploadType: "local", ExpectedSize: 5, MIMEType: "text/plain", OriginalName: "file.txt", Overwrite: overwrite, Status: "created", CreatedAt: now, ExpiresAt: now + 100000}
		return up, blob
	}
	first, b1 := makeUpload(false)
	require.NoError(t, f.store.CreateUpload(ctx, first, b1))
	first, err := f.store.MarkUploadCompleting(ctx, first.ID, f.user.ID, repository.NowMS())
	require.NoError(t, err)
	_, err = f.store.FinalizeUpload(ctx, first, 4, "", "", "")
	require.Error(t, err)
	node, err := f.store.FinalizeUpload(ctx, first, 5, "etag", "", "")
	require.NoError(t, err)
	second, b2 := makeUpload(false)
	require.ErrorIs(t, f.store.CreateUpload(ctx, second, b2), repository.ErrConflict)
	second, b2 = makeUpload(true)
	require.NoError(t, f.store.CreateUpload(ctx, second, b2))
	second, err = f.store.MarkUploadCompleting(ctx, second.ID, f.user.ID, repository.NowMS())
	require.NoError(t, err)
	newNode, err := f.store.FinalizeUpload(ctx, second, 5, "new", "", "")
	require.NoError(t, err)
	require.Equal(t, node.ID, newNode.ID)
	old, err := f.store.BlobByID(ctx, b1.ID)
	require.NoError(t, err)
	require.Equal(t, "deleting", old.Status)
	var jobs int
	require.NoError(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM background_jobs WHERE job_type='delete_blob'`).Scan(&jobs))
	require.Equal(t, 1, jobs)
}
