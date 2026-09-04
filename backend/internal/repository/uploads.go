package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/altasci/network-storage/backend/internal/security"
)

const uploadColumns = `id,project_id,node_id,blob_id,user_id,upload_type,provider_upload_id,expected_size,mime_type,original_name,parent_id,overwrite,status,created_at,expires_at,completed_at`

func scanUpload(scanner interface{ Scan(...any) error }) (Upload, error) {
	var u Upload
	err := scanner.Scan(&u.ID, &u.ProjectID, &u.NodeID, &u.BlobID, &u.UserID, &u.UploadType, &u.ProviderUploadID, &u.ExpectedSize, &u.MIMEType, &u.OriginalName, &u.ParentID, &u.Overwrite, &u.Status, &u.CreatedAt, &u.ExpiresAt, &u.CompletedAt)
	return u, mapError(err)
}

func (s *Store) CreateUpload(ctx context.Context, u Upload, b Blob) error {
	name, err := NormalizeName(u.OriginalName)
	if err != nil {
		return err
	}
	u.OriginalName = name
	if err := s.ensureDirectory(ctx, s.DB, u.ProjectID, u.ParentID.String); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existingID, nodeType string
	err = tx.QueryRowContext(ctx, `SELECT id,node_type FROM fs_nodes WHERE project_id=? AND COALESCE(parent_id,'')=? AND normalized_name=? AND deleted_at IS NULL`, u.ProjectID, u.ParentID.String, name).Scan(&existingID, &nodeType)
	if err == nil {
		if nodeType != "file" || !u.Overwrite {
			return ErrConflict
		}
		u.NodeID = sql.NullString{String: existingID, Valid: true}
	} else if err != sql.ErrNoRows {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO file_blobs(`+blobColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, b.ID, b.ProjectID, b.StorageBackendID, b.ObjectKey, b.Size, b.MIMEType, nil, nil, nil, "pending", b.CreatedAt, nil)
	if err != nil {
		return mapError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO upload_sessions(`+uploadColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, u.ID, u.ProjectID, nullableString(u.NodeID.String), u.BlobID, u.UserID, u.UploadType, nullableString(u.ProviderUploadID.String), u.ExpectedSize, u.MIMEType, u.OriginalName, nullableString(u.ParentID.String), u.Overwrite, u.Status, u.CreatedAt, u.ExpiresAt, nil)
	if err != nil {
		return mapError(err)
	}
	return tx.Commit()
}
func (s *Store) UploadByID(ctx context.Context, id string) (Upload, error) {
	return scanUpload(s.DB.QueryRowContext(ctx, `SELECT `+uploadColumns+` FROM upload_sessions WHERE id=?`, id))
}
func (s *Store) SetProviderUploadID(ctx context.Context, id, providerID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE upload_sessions SET provider_upload_id=?,status='uploading' WHERE id=? AND status='created'`, providerID, id)
	return err
}
func (s *Store) MarkUploadCompleting(ctx context.Context, id, userID string, now int64) (Upload, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE upload_sessions SET status='completing' WHERE id=? AND user_id=? AND status IN ('created','uploading') AND expires_at>?`, id, userID, now)
	if err != nil {
		return Upload{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Upload{}, ErrConflict
	}
	return s.UploadByID(ctx, id)
}
func (s *Store) FinalizeUpload(ctx context.Context, u Upload, infoSize int64, etag, checksumAlgorithm, checksumValue string) (Node, error) {
	if infoSize != u.ExpectedSize {
		return Node{}, fmt.Errorf("uploaded object size %d does not match expected size %d", infoSize, u.ExpectedSize)
	}
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Node{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE file_blobs SET status='active',etag=?,checksum_algorithm=?,checksum_value=? WHERE id=? AND status='pending'`, nullableString(etag), nullableString(checksumAlgorithm), nullableString(checksumValue), u.BlobID)
	if err != nil {
		return Node{}, err
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		return Node{}, ErrConflict
	}
	var n Node
	if u.NodeID.Valid {
		n, err = scanNode(tx.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM fs_nodes WHERE id=? AND deleted_at IS NULL`, u.NodeID.String))
		if err != nil {
			return Node{}, err
		}
		old := n.CurrentBlobID.String
		n.CurrentBlobID = sql.NullString{String: u.BlobID, Valid: true}
		n.Size = u.ExpectedSize
		n.MIMEType = u.MIMEType
		n.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, `UPDATE fs_nodes SET current_blob_id=?,size=?,mime_type=?,updated_at=? WHERE id=?`, u.BlobID, u.ExpectedSize, u.MIMEType, now, n.ID); err != nil {
			return Node{}, err
		}
		if old != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE file_blobs SET status='deleting',deleted_at=? WHERE id=? AND status='active'`, now, old); err != nil {
				return Node{}, err
			}
			if err := insertJob(ctx, tx, "delete_blob", map[string]string{"blob_id": old}, now); err != nil {
				return Node{}, err
			}
		}
	} else {
		n = Node{ID: securityID(), ProjectID: u.ProjectID, ParentID: u.ParentID, NodeType: "file", Name: u.OriginalName, NormalizedName: u.OriginalName, CurrentBlobID: sql.NullString{String: u.BlobID, Valid: true}, Size: u.ExpectedSize, MIMEType: u.MIMEType, CreatedBy: u.UserID, CreatedAt: now, UpdatedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO fs_nodes(`+nodeColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, n.ID, n.ProjectID, nullableString(n.ParentID.String), n.NodeType, n.Name, n.NormalizedName, u.BlobID, n.Size, n.MIMEType, n.CreatedBy, n.CreatedAt, n.UpdatedAt, nil)
		if err != nil {
			return Node{}, mapError(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET status='completed',node_id=?,completed_at=? WHERE id=? AND status='completing'`, n.ID, now, u.ID); err != nil {
		return Node{}, err
	}
	if err := tx.Commit(); err != nil {
		return Node{}, err
	}
	return n, nil
}

func securityID() string { return security.NewID() }

func (s *Store) AbortUpload(ctx context.Context, id, userID string) error {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var blobID string
	err = tx.QueryRowContext(ctx, `SELECT blob_id FROM upload_sessions WHERE id=? AND user_id=? AND status IN ('created','uploading','completing')`, id, userID).Scan(&blobID)
	if err != nil {
		return mapError(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET status='aborted' WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file_blobs SET status='deleting',deleted_at=? WHERE id=?`, now, blobID); err != nil {
		return err
	}
	if err := insertJob(ctx, tx, "delete_blob", map[string]string{"blob_id": blobID}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FailUpload(ctx context.Context, id string) error {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var blobID string
	if err := tx.QueryRowContext(ctx, `SELECT blob_id FROM upload_sessions WHERE id=?`, id).Scan(&blobID); err != nil {
		return mapError(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET status='failed' WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file_blobs SET status='deleting',deleted_at=? WHERE id=?`, now, blobID); err != nil {
		return err
	}
	if err := insertJob(ctx, tx, "delete_blob", map[string]string{"blob_id": blobID}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ExpiredUploads(ctx context.Context, now int64, limit int) ([]Upload, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+uploadColumns+` FROM upload_sessions WHERE status IN ('created','uploading','completing') AND expires_at<=? ORDER BY expires_at LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upload
	for rows.Next() {
		u, e := scanUpload(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// imports are kept explicit because upload finalization is a security boundary.
var _ = sql.ErrNoRows
