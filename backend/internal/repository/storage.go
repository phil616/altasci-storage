package repository

import (
	"context"
	"database/sql"
)

const storageBaseColumns = `id,name,type,enabled,config_json,secret_ciphertext,created_at,updated_at,last_test_at,last_test_status,last_test_message`
const storageSelectColumns = storageBaseColumns + `,
	(SELECT COUNT(*) FROM projects p WHERE p.storage_backend_id=storage_backends.id),
	(SELECT COUNT(*) FROM file_blobs b WHERE b.storage_backend_id=storage_backends.id)`

func scanStorage(scanner interface{ Scan(...any) error }) (StorageBackend, error) {
	var b StorageBackend
	err := scanner.Scan(&b.ID, &b.Name, &b.Type, &b.Enabled, &b.ConfigJSON, &b.SecretCiphertext, &b.CreatedAt, &b.UpdatedAt, &b.LastTestAt, &b.LastTestStatus, &b.LastTestMessage, &b.ProjectCount, &b.BlobCount)
	return b, mapError(err)
}

func (s *Store) CreateStorageBackend(ctx context.Context, b StorageBackend) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO storage_backends(`+storageBaseColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, b.ID, b.Name, b.Type, b.Enabled, b.ConfigJSON, nullableString(b.SecretCiphertext.String), b.CreatedAt, b.UpdatedAt, nil, nil, nil)
	return mapError(err)
}
func (s *Store) StorageBackendByID(ctx context.Context, id string) (StorageBackend, error) {
	return scanStorage(s.DB.QueryRowContext(ctx, `SELECT `+storageSelectColumns+` FROM storage_backends WHERE id=?`, id))
}
func (s *Store) ListStorageBackends(ctx context.Context) ([]StorageBackend, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+storageSelectColumns+` FROM storage_backends ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StorageBackend
	for rows.Next() {
		v, e := scanStorage(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) UpdateStorageBackend(ctx context.Context, id, name, configJSON string, enabled bool, secret *string) (StorageBackend, error) {
	now := NowMS()
	query := `UPDATE storage_backends SET name=?,config_json=?,enabled=?,updated_at=?`
	args := []any{name, configJSON, enabled, now}
	if secret != nil {
		query += `,secret_ciphertext=?`
		args = append(args, *secret)
	}
	query += ` WHERE id=?`
	args = append(args, id)
	res, err := s.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return StorageBackend{}, mapError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return StorageBackend{}, ErrNotFound
	}
	return s.StorageBackendByID(ctx, id)
}
func (s *Store) DeleteStorageBackend(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM storage_backends
		WHERE id=?
		AND NOT EXISTS (SELECT 1 FROM projects WHERE storage_backend_id=storage_backends.id)
		AND NOT EXISTS (SELECT 1 FROM file_blobs WHERE storage_backend_id=storage_backends.id)`, id)
	if err != nil {
		return mapError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM storage_backends WHERE id=?)`, id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrConflict
		}
		return ErrNotFound
	}
	return nil
}
func (s *Store) SetStorageTest(ctx context.Context, id, status, message string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE storage_backends SET last_test_at=?,last_test_status=?,last_test_message=? WHERE id=?`, NowMS(), status, message, id)
	return err
}

var _ = sql.ErrNoRows
